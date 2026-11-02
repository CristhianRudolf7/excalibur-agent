package edit

import (
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/create"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/opc"
	"path"
	"regexp"
	"strconv"
	"strings"
)

type sheetEntry struct {
	name, part string
	n          *node
}

func sheetList(p *opc.Package) ([]sheetEntry, error) {
	r, e := parse(string(p.Parts["xl/workbook.xml"]))
	if e != nil {
		return nil, e
	}
	rels, e := p.Relationships("xl/_rels/workbook.xml.rels")
	if e != nil {
		return nil, e
	}
	targets := map[string]string{}
	for _, v := range rels {
		targets[v.ID] = opc.Resolve("xl/_rels/workbook.xml.rels", v.Target)
	}
	out := []sheetEntry{}
	for _, n := range r.find("sheet") {
		part := targets[n.attr("id")]
		if part == "" {
			return nil, fmt.Errorf("sheet relationship missing")
		}
		out = append(out, sheetEntry{n.attr("name"), part, n})
	}
	return out, nil
}
func recalc(p *opc.Package) error {
	s := string(p.Parts["xl/workbook.xml"])
	r, e := parse(s)
	if e != nil {
		return e
	}
	cs := r.find("calcPr")
	if len(cs) > 0 {
		n := cs[0]
		tag := attr(attr(s[n.start:n.open], "fullCalcOnLoad", "1"), "forceFullCalc", "1")
		s = s[:n.start] + tag + s[n.open:]
	} else {
		s = insertEnd(s, r.children[0], `<calcPr xmlns="`+create.S+`" fullCalcOnLoad="1" forceFullCalc="1"/>`)
	}
	p.Parts["xl/workbook.xml"] = []byte(s)
	return nil
}
func excel(p *opc.Package, o Operation) error {
	sheets, e := sheetList(p)
	if e != nil {
		return e
	}
	if o.Action == "add_sheet" {
		sh := create.Sheet{Name: o.Name}
		if o.NewSheet != nil {
			sh = *o.NewSheet
		}
		for _, existing := range sheets {
			if strings.EqualFold(existing.name, sh.Name) {
				return fmt.Errorf("duplicate sheet name")
			}
		}
		xml, e := create.SheetXML(sh)
		if e != nil {
			return e
		}
		part := uniquePart(p, "xl/worksheets", "sheet")
		p.Parts[part] = []byte(xml)
		id, e := addRel(p, "xl/_rels/workbook.xml.rels", "worksheet", "worksheets/"+path.Base(part))
		if e != nil {
			return e
		}
		s := string(p.Parts["xl/workbook.xml"])
		r, e := parse(s)
		if e != nil {
			return e
		}
		ns := r.find("sheets")
		if len(ns) != 1 {
			return fmt.Errorf("missing sheets")
		}
		max := 0
		for _, sh := range sheets {
			i, _ := strconv.Atoi(sh.n.attr("sheetId"))
			if i > max {
				max = i
			}
		}
		p.Parts["xl/workbook.xml"] = []byte(insertEnd(s, ns[0], fmt.Sprintf(`<sheet xmlns="%s" xmlns:r="%s" name="%s" sheetId="%d" r:id="%s"/>`, create.S, create.R, opc.Escape(sh.Name), max+1, id)))
		if e = addType(p, part, "application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"); e != nil {
			return e
		}
		return recalc(p)
	}
	var sheet *sheetEntry
	for i := range sheets {
		if sheets[i].name == o.Sheet {
			sheet = &sheets[i]
			break
		}
	}
	if sheet == nil {
		return fmt.Errorf("sheet not found")
	}
	if o.Action == "rename_sheet" {
		if e = create.ValidSheetName(o.Name); e != nil {
			return e
		}
		for _, sh := range sheets {
			if strings.EqualFold(sh.name, o.Name) && sh.name != o.Sheet {
				return fmt.Errorf("duplicate sheet name")
			}
		}
		s := string(p.Parts["xl/workbook.xml"])
		n := sheet.n
		p.Parts["xl/workbook.xml"] = []byte(s[:n.start] + attr(s[n.start:n.open], "name", o.Name) + s[n.open:])
		renameFormulaSheets(p, o.Sheet, o.Name)
		return recalc(p)
	}
	s := string(p.Parts[sheet.part])
	r, e := parse(s)
	if e != nil {
		return e
	}
	switch o.Action {
	case "set_cell":
		row, col, e := create.CellPosition(o.Cell.Ref)
		if e != nil {
			return e
		}
		for _, f := range r.find("f") {
			kind := f.attr("t")
			if kind != "shared" && kind != "array" {
				continue
			}
			if f.parent.name.Local == "c" && f.parent.attr("r") == o.Cell.Ref {
				return fmt.Errorf("cannot overwrite a shared/array formula cell")
			}
			bounds := strings.Split(strings.ReplaceAll(f.attr("ref"), "$", ""), ":")
			if len(bounds) > 0 && bounds[0] != "" {
				lo, lc, err := create.CellPosition(bounds[0])
				if err != nil {
					return err
				}
				hi, hc := lo, lc
				if len(bounds) == 2 {
					hi, hc, err = create.CellPosition(bounds[1])
					if err != nil {
						return err
					}
				}
				if row >= lo && row <= hi && col >= lc && col <= hc {
					return fmt.Errorf("cannot overwrite a shared/array formula range")
				}
			}
		}
		fragment, e := create.CellXML(o.Cell)
		if e != nil {
			return e
		}
		fragment = strings.Replace(fragment, "<c ", `<c xmlns="`+create.S+`" `, 1)
		var found *node
		for _, n := range r.find("c") {
			if n.attr("r") == o.Cell.Ref {
				found = n
				break
			}
		}
		if found != nil { // preserve all existing cell attributes except the value type
			nr, _ := parse(fragment)
			newnode := nr.children[0]
			tag := attr(removeAttr(s[found.start:found.open], "t"), "xmlns", create.S)
			if v := newnode.attr("t"); v != "" {
				tag = attr(tag, "t", v)
			}
			if o.Cell.Header {
				tag = attr(tag, "s", "1")
			}
			if strings.HasSuffix(tag, "/>") {
				tag = strings.TrimSuffix(tag, "/>") + ">"
			}
			q := strings.Fields(strings.TrimPrefix(tag, "<"))[0]
			q = strings.TrimSuffix(q, ">")
			fragment = tag + fragment[newnode.open:newnode.close] + "</" + q + ">"
			s = s[:found.start] + fragment + s[found.end:]
		} else {
			var rn *node
			for _, n := range r.find("row") {
				if n.attr("r") == strconv.Itoa(row) {
					rn = n
					break
				}
			}
			if rn != nil {
				idx := rn.close
				for _, c := range rn.find("c") {
					_, cc, _ := create.CellPosition(c.attr("r"))
					if cc > col {
						idx = c.start
						break
					}
				}
				if rn.open == rn.close {
					s = insertEnd(s, rn, fragment)
				} else {
					s = s[:idx] + fragment + s[idx:]
				}
			} else {
				data := r.find("sheetData")
				if len(data) != 1 {
					return fmt.Errorf("missing sheetData")
				}
				rf := fmt.Sprintf(`<row xmlns="%s" r="%d">%s</row>`, create.S, row, fragment)
				idx := data[0].close
				for _, n := range data[0].find("row") {
					rr, _ := strconv.Atoi(n.attr("r"))
					if rr > row {
						idx = n.start
						break
					}
				}
				if data[0].open == data[0].close {
					s = insertEnd(s, data[0], rf)
				} else {
					s = s[:idx] + rf + s[idx:]
				}
			}
		}
	case "delete_rows":
		if o.Start < 1 || o.Count < 1 || o.Start > 1048576 || o.Count > 1048576-o.Start+1 {
			return fmt.Errorf("invalid row range")
		}
		for _, n := range r.find("f") {
			if n.attr("t") == "shared" || n.attr("t") == "array" {
				return fmt.Errorf("shared/array formulas prevent row deletion")
			}
		}
		if len(r.find("tablePart")) > 0 {
			return fmt.Errorf("table-bearing sheets are not supported for row deletion")
		}
		if len(r.find("mergeCell")) > 0 || len(r.find("dataValidation")) > 0 || len(r.find("conditionalFormatting")) > 0 || len(r.find("drawing")) > 0 {
			return fmt.Errorf("row deletion requires a simple sheet without merges, validations, conditional formatting or drawings")
		}
		ps := []patch{}
		for _, n := range r.find("row") {
			v, _ := strconv.Atoi(n.attr("r"))
			if v >= o.Start && v < o.Start+o.Count {
				ps = append(ps, patch{n.start, n.end, ""})
				continue
			}
			if v >= o.Start+o.Count {
				tag := attr(s[n.start:n.open], "r", strconv.Itoa(v-o.Count))
				ps = append(ps, patch{n.start, n.open, tag})
			}
			for _, c := range n.find("c") {
				rr, _, e := create.CellPosition(c.attr("r"))
				if e != nil {
					return e
				}
				if rr >= o.Start+o.Count {
					ref := regexp.MustCompile(`[0-9]+$`).ReplaceAllString(c.attr("r"), strconv.Itoa(rr-o.Count))
					ps = append(ps, patch{c.start, c.open, attr(s[c.start:c.open], "r", ref)})
				}
			}
		}
		s = splice(s, ps)
	default:
		return fmt.Errorf("unknown XLSX action %q", o.Action)
	}
	p.Parts[sheet.part] = []byte(s)
	if o.Action == "delete_rows" {
		if e = shiftFormulaRows(p, sheet.name, sheet.part, o.Start, o.Count); e != nil {
			return e
		} // remove the now stale optional used-range hint
		s = string(p.Parts[sheet.part])
		r, _ = parse(s)
		ps := []patch{}
		for _, n := range r.find("dimension") {
			ps = append(ps, patch{n.start, n.end, ""})
		}
		p.Parts[sheet.part] = []byte(splice(s, ps))
	}
	return recalc(p)
}
func renameFormulaSheets(p *opc.Package, old, new string) {
	for part := range spreadsheetFormulaParts(p) {
		data := p.Parts[part]
		r, e := parse(string(data))
		if e != nil {
			continue
		}
		ps := []patch{}
		for _, n := range append(r.find("f"), r.find("definedName")...) {
			if n.name.Space != create.S {
				continue
			}
			v := n.content()
			v = outsideStrings(v, func(v string) string {
				quoted := "'" + strings.ReplaceAll(old, "'", "''") + "'!"
				replacement := "'" + strings.ReplaceAll(new, "'", "''") + "'!"
				quotedRE := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(quoted))
				v = quotedRE.ReplaceAllStringFunc(v, func(string) string { return replacement })
				re := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_'.])` + regexp.QuoteMeta(old) + `!`)
				return re.ReplaceAllStringFunc(v, func(s string) string { return s[:len(s)-len(old)-1] + replacement })
			})
			if v != n.content() {
				ps = append(ps, patch{n.open, n.close, opc.Escape(v)})
			}
		}
		if len(ps) > 0 {
			p.Parts[part] = []byte(splice(string(data), ps))
		}
	}
}

// Formula string literals are opaque. Unsupported complex references fail closed.
func outsideStrings(s string, fn func(string) string) string {
	var b strings.Builder
	for len(s) > 0 {
		i := strings.IndexByte(s, '"')
		if i < 0 {
			b.WriteString(fn(s))
			break
		}
		b.WriteString(fn(s[:i]))
		s = s[i:]
		j := 1
		for j < len(s) {
			if s[j] == '"' {
				if j+1 < len(s) && s[j+1] == '"' {
					j += 2
					continue
				}
				j++
				break
			}
			j++
		}
		b.WriteString(s[:j])
		s = s[j:]
	}
	return b.String()
}

var a1 = regexp.MustCompile(`(?i)((?:'(?:[^']|'')+'|[\p{L}_][\p{L}\p{N}_.]*)!)?(\$?[A-Z]{1,3})(\$?)([1-9][0-9]*)`)

func shiftFormulaRows(p *opc.Package, sheet, localPart string, start, count int) error {
	for part := range spreadsheetFormulaParts(p) {
		data := p.Parts[part]
		r, e := parse(string(data))
		if e != nil {
			continue
		}
		ps := []patch{}
		for _, n := range append(r.find("f"), r.find("definedName")...) {
			if n.name.Space != create.S {
				continue
			}
			v := n.content()
			if strings.ContainsAny(v, "[]") || strings.Contains(strings.ToUpper(v), "INDIRECT(") {
				return fmt.Errorf("complex formula prevents row deletion")
			}
			v = outsideStrings(v, func(v string) string { return shiftReferences(v, sheet, part == localPart, start, count) })
			if v != n.content() {
				ps = append(ps, patch{n.open, n.close, opc.Escape(v)})
			}
		}
		if len(ps) > 0 {
			p.Parts[part] = []byte(splice(string(data), ps))
		}
	}
	return nil
}

func referenceSheet(m []string, sheet string, local bool) bool {
	if m[1] == "" {
		return local
	}
	q := strings.TrimSuffix(m[1], "!")
	q = strings.ReplaceAll(strings.Trim(q, "'"), "''", "'")
	return strings.EqualFold(q, sheet)
}
func ident(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '$'
}
func shiftReferences(s, sheet string, local bool, start, count int) string {
	matches := a1.FindAllStringIndex(s, -1)
	ps := []patch{}
	for i := 0; i < len(matches); i++ {
		a, b := matches[i][0], matches[i][1]
		if a > 0 && (ident(s[a-1]) || s[a-1] == '!' || s[a-1] == ']') || b < len(s) && (ident(s[b]) || s[b] == '(') {
			continue
		}
		m := a1.FindStringSubmatch(s[a:b])
		if _, _, err := create.CellPosition(strings.ToUpper(strings.ReplaceAll(m[2], "$", "")) + m[4]); err != nil {
			continue
		}
		targeted := referenceSheet(m, sheet, local)
		row, _ := strconv.Atoi(m[4])
		if i+1 < len(matches) && s[b:matches[i+1][0]] == ":" {
			next := matches[i+1]
			m2 := a1.FindStringSubmatch(s[next[0]:next[1]])
			secondLocal := local
			if m[1] != "" {
				secondLocal = targeted
			}
			if targeted && referenceSheet(m2, sheet, secondLocal) {
				high, _ := strconv.Atoi(m2[4])
				low := row
				if high >= low {
					if low >= start+count {
						low -= count
					} else if low >= start {
						low = start
					}
					if high >= start+count {
						high -= count
					} else if high >= start {
						high = start - 1
					}
					text := "#REF!"
					if high >= low {
						text = m[1] + m[2] + m[3] + strconv.Itoa(low) + ":" + m2[1] + m2[2] + m2[3] + strconv.Itoa(high)
					}
					ps = append(ps, patch{a, next[1], text})
					i++
					continue
				}
			}
		}
		if !targeted {
			continue
		}
		text := s[a:b]
		if row >= start && row < start+count {
			text = "#REF!"
		} else if row >= start+count {
			text = m[1] + m[2] + m[3] + strconv.Itoa(row-count)
		}
		if text != s[a:b] {
			ps = append(ps, patch{a, b, text})
		}
	}
	return splice(s, ps)
}

// Only workbook and worksheet parts are understood formula containers. Opaque
// custom XML, drawings and other unrecognized parts must remain byte-for-byte.
func spreadsheetFormulaParts(p *opc.Package) map[string]bool {
	parts := map[string]bool{"xl/workbook.xml": true}
	sheets, _ := sheetList(p)
	for _, s := range sheets {
		parts[s.part] = true
	}
	return parts
}
