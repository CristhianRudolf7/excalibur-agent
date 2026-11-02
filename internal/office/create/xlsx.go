package create

import (
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/opc"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var cellRef = regexp.MustCompile(`^([A-Z]{1,3})([1-9][0-9]{0,6})$`)

func CellPosition(ref string) (int, int, error) {
	m := cellRef.FindStringSubmatch(ref)
	if m == nil {
		return 0, 0, fmt.Errorf("invalid cell reference %q", ref)
	}
	col := 0
	for _, c := range m[1] {
		col = col*26 + int(c-'A'+1)
	}
	row, _ := strconv.Atoi(m[2])
	if row > 1048576 || col > 16384 {
		return 0, 0, fmt.Errorf("cell out of range")
	}
	return row, col, nil
}
func ValidSheetName(name string) error {
	if len([]rune(name)) == 0 || len([]rune(name)) > 31 || strings.ContainsAny(name, `[]:*?/\`) || strings.HasPrefix(name, "'") || strings.HasSuffix(name, "'") {
		return fmt.Errorf("invalid sheet name %q", name)
	}
	return nil
}

// CellXML validates and serializes a typed cell. It never emits a cached formula value.
func CellXML(c Cell) (string, error) {
	if _, _, e := CellPosition(c.Ref); e != nil {
		return "", e
	}
	s := `<c r="` + c.Ref + `"`
	if c.Header {
		s += ` s="1"`
	}
	if c.Formula != "" {
		return s + `><f>` + esc(strings.TrimPrefix(c.Formula, "=")) + `</f></c>`, nil
	}
	kind := c.Type
	if kind == "" {
		kind = "string"
	}
	switch kind {
	case "string":
		v, ok := c.Value.(string)
		if !ok && c.Value != nil {
			return "", fmt.Errorf("string cell requires a string")
		}
		return s + ` t="inlineStr"><is><t xml:space="preserve">` + esc(v) + `</t></is></c>`, nil
	case "number":
		v, e := strconv.ParseFloat(fmt.Sprint(c.Value), 64)
		if e != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return "", fmt.Errorf("invalid numeric value")
		}
		return s + `><v>` + strconv.FormatFloat(v, 'g', -1, 64) + `</v></c>`, nil
	case "bool":
		v, ok := c.Value.(bool)
		if !ok {
			return "", fmt.Errorf("bool cell requires a boolean")
		}
		n := "0"
		if v {
			n = "1"
		}
		return s + ` t="b"><v>` + n + `</v></c>`, nil
	case "date":
		v, ok := c.Value.(string)
		if !ok {
			return "", fmt.Errorf("date requires ISO 8601 string")
		}
		if _, e := time.Parse("2006-01-02", v); e != nil {
			if _, e = time.Parse(time.RFC3339, v); e != nil {
				return "", fmt.Errorf("invalid ISO date")
			}
		}
		return s + ` t="d"><v>` + esc(v) + `</v></c>`, nil
	default:
		return "", fmt.Errorf("unknown cell type %q", kind)
	}
}
func SheetXML(s Sheet) (string, error) {
	if len(s.ColumnWidths) > 16384 {
		return "", fmt.Errorf("too many column widths")
	}
	if e := ValidSheetName(s.Name); e != nil {
		return "", e
	}
	var out strings.Builder
	out.WriteString(`<worksheet xmlns="` + S + `"><sheetViews><sheetView workbookViewId="0">`)
	if s.FreezeFirstRow {
		out.WriteString(`<pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/>`)
	}
	out.WriteString(`</sheetView></sheetViews>`)
	if len(s.ColumnWidths) > 0 {
		out.WriteString(`<cols>`)
		for i, w := range s.ColumnWidths {
			if w <= 0 || w > 255 || math.IsNaN(w) || math.IsInf(w, 0) {
				return "", fmt.Errorf("invalid column width")
			}
			out.WriteString(fmt.Sprintf(`<col min="%d" max="%d" width="%g" customWidth="1"/>`, i+1, i+1, w))
		}
		out.WriteString(`</cols>`)
	}
	out.WriteString(`<sheetData>`)
	cells := append([]Cell(nil), s.Cells...)
	sort.Slice(cells, func(i, j int) bool {
		ri, ci, _ := CellPosition(cells[i].Ref)
		rj, cj, _ := CellPosition(cells[j].Ref)
		return ri < rj || ri == rj && ci < cj
	})
	row := 0
	seen := map[string]bool{}
	for _, c := range cells {
		r, _, e := CellPosition(c.Ref)
		if e != nil {
			return "", e
		}
		if seen[c.Ref] {
			return "", fmt.Errorf("duplicate cell")
		}
		seen[c.Ref] = true
		if r != row {
			if row > 0 {
				out.WriteString(`</row>`)
			}
			out.WriteString(fmt.Sprintf(`<row r="%d">`, r))
			row = r
		}
		v, e := CellXML(c)
		if e != nil {
			return "", e
		}
		out.WriteString(v)
	}
	if row > 0 {
		out.WriteString(`</row>`)
	}
	out.WriteString(`</sheetData></worksheet>`)
	return out.String(), nil
}
func xlsx(s Spec) (*opc.Package, error) {
	if len(s.Sheets) == 0 {
		return nil, fmt.Errorf("at least one sheet required")
	}
	p := opc.New()
	root(p, "xl/workbook.xml")
	types := map[string]string{"xl/workbook.xml": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml", "xl/styles.xml": "application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"}
	rels := []opc.Relationship{relation("styles", "styles", "styles.xml")}
	book := `<workbook xmlns="` + S + `" xmlns:r="` + R + `"><sheets>`
	seen := map[string]bool{}
	for i, sheet := range s.Sheets {
		key := strings.ToLower(sheet.Name)
		if seen[key] {
			return nil, fmt.Errorf("duplicate sheet name")
		}
		seen[key] = true
		data, e := SheetXML(sheet)
		if e != nil {
			return nil, e
		}
		n := fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)
		p.Parts[n] = []byte(data)
		types[n] = "application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"
		id := fmt.Sprintf("sheet%d", i+1)
		book += fmt.Sprintf(`<sheet name="%s" sheetId="%d" r:id="%s"/>`, esc(sheet.Name), i+1, id)
		rels = append(rels, relation(id, "worksheet", fmt.Sprintf("worksheets/sheet%d.xml", i+1)))
	}
	p.Parts["xl/workbook.xml"] = []byte(book + `</sheets><calcPr fullCalcOnLoad="1" forceFullCalc="1"/></workbook>`)
	p.SetRelationships("xl/_rels/workbook.xml.rels", rels)
	p.Parts["xl/styles.xml"] = []byte(`<styleSheet xmlns="` + S + `"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border/></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/></cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`)
	p.Types(types)
	return p, nil
}
