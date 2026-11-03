package collections

import (
	"encoding/xml"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/opc"
)

// Native extraction preserves cached cell values. This pass handles Excel's
// numeric date storage, including the 1904 epoch, using the workbook styles.
func excelDateFixesPackage(p *opc.Package, sheet string) (map[string]api.ImportFix, error) {
	out := map[string]api.ImportFix{}
	var err error
	var book struct {
		Properties struct {
			Date1904 string `xml:"date1904,attr"`
		} `xml:"workbookPr"`
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err = xml.Unmarshal(p.Parts["xl/workbook.xml"], &book); err != nil {
		return nil, err
	}
	id := ""
	for _, s := range book.Sheets {
		if s.Name == sheet {
			id = s.ID
		}
	}
	rels, err := p.Relationships("xl/_rels/workbook.xml.rels")
	if err != nil {
		return nil, err
	}
	part := ""
	for _, r := range rels {
		if r.ID == id {
			part = opc.Resolve("xl/_rels/workbook.xml.rels", r.Target)
		}
	}
	var styles struct {
		Formats []struct {
			ID   int    `xml:"numFmtId,attr"`
			Code string `xml:"formatCode,attr"`
		} `xml:"numFmts>numFmt"`
		Xfs []struct {
			ID int `xml:"numFmtId,attr"`
		} `xml:"cellXfs>xf"`
	}
	if len(p.Parts["xl/styles.xml"]) == 0 {
		return out, nil
	}
	if err = xml.Unmarshal(p.Parts["xl/styles.xml"], &styles); err != nil {
		return nil, err
	}
	formats := map[int]string{}
	for _, f := range styles.Formats {
		formats[f.ID] = f.Code
	}
	dateStyles := map[int]bool{}
	for i, f := range styles.Xfs {
		if (f.ID >= 14 && f.ID <= 22) || (f.ID >= 27 && f.ID <= 36) || (f.ID >= 50 && f.ID <= 58) {
			dateStyles[i] = true
			continue
		}
		code := strings.ToLower(formats[f.ID])
		quoted, escaped, bracket := false, false, false
		clean := ""
		for _, r := range code {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				quoted = !quoted
				continue
			}
			if r == '[' {
				bracket = true
				continue
			}
			if r == ']' {
				bracket = false
				continue
			}
			if !quoted && !bracket {
				clean += string(r)
			}
		}
		if strings.ContainsAny(clean, "yd") {
			dateStyles[i] = true
		}
	}
	var worksheet struct {
		Rows []struct {
			Cells []struct {
				Ref   string `xml:"r,attr"`
				Style int    `xml:"s,attr"`
				Type  string `xml:"t,attr"`
				Value string `xml:"v"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err = xml.Unmarshal(p.Parts[part], &worksheet); err != nil {
		return nil, err
	}
	for _, r := range worksheet.Rows {
		for _, c := range r.Cells {
			if !dateStyles[c.Style] || (c.Type != "" && c.Type != "n") {
				continue
			}
			serial, e := strconv.ParseFloat(c.Value, 64)
			if e != nil || math.IsInf(serial, 0) || math.IsNaN(serial) || serial < 0 || serial > 2958465 {
				continue
			}
			days := int(serial)
			if days == 60 && book.Properties.Date1904 != "1" && book.Properties.Date1904 != "true" {
				continue
			}
			base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
			if days < 60 {
				base = base.AddDate(0, 0, 1)
			}
			if book.Properties.Date1904 == "1" || book.Properties.Date1904 == "true" {
				base = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
			}
			date := base.AddDate(0, 0, days).Add(time.Duration(math.Round((serial-float64(days))*86400)) * time.Second)
			normalized := date.Format("2006-01-02")
			if serial != float64(days) {
				normalized = date.Format(time.RFC3339)
			}
			row, col := cellPosition(c.Ref)
			out[c.Ref] = api.ImportFix{Row: row, Column: strconv.Itoa(col), From: c.Value, To: normalized, Rule: "excel_serial_date"}
		}
	}
	return out, nil
}
