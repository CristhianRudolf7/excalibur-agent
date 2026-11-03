package collections

import (
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/opc"
)

const MaxImportCells = 1000000

type excelText struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func (s excelText) text() string {
	var b strings.Builder
	b.WriteString(s.Text)
	for _, r := range s.Runs {
		b.WriteString(r.Text)
	}
	return b.String()
}

// readSpreadsheet uses the existing bounded OOXML package reader. The document
// preview extractor has a 20,000-cell limit, so collection imports read the native
// cell grid directly instead of importing a truncated document preview.
func readSpreadsheet(ctx context.Context, data []byte, sheet string) ([][]string, []int, string, []api.ImportFix, error) {
	fail := func(err error) ([][]string, []int, string, []api.ImportFix, error) { return nil, nil, "", nil, err }
	pkg, err := opc.Open(data)
	if err != nil {
		return fail(err)
	}
	var book struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err = xml.Unmarshal(pkg.Parts["xl/workbook.xml"], &book); err != nil {
		return fail(err)
	}
	if len(book.Sheets) == 0 {
		return fail(invalid("sheet", "workbook has no sheets"))
	}
	chosen := book.Sheets[0]
	if sheet != "" {
		found := false
		for i, s := range book.Sheets {
			if s.Name == sheet || strconv.Itoa(i+1) == sheet {
				chosen = s
				found = true
				break
			}
		}
		if !found {
			return fail(invalid("sheet", "sheet not found"))
		}
	}
	relationships, err := pkg.Relationships("xl/_rels/workbook.xml.rels")
	if err != nil {
		return fail(err)
	}
	part := ""
	for _, r := range relationships {
		if r.ID == chosen.ID {
			part = opc.Resolve("xl/_rels/workbook.xml.rels", r.Target)
		}
	}
	if len(pkg.Parts[part]) == 0 {
		return fail(invalid("sheet", "sheet contents not found"))
	}
	var shared struct {
		Strings []excelText `xml:"si"`
	}
	if len(pkg.Parts["xl/sharedStrings.xml"]) > 0 {
		if err = xml.Unmarshal(pkg.Parts["xl/sharedStrings.xml"], &shared); err != nil {
			return fail(err)
		}
	}
	dateFixes, err := excelDateFixesPackage(pkg, chosen.Name)
	if err != nil {
		return fail(err)
	}
	var grid struct {
		Rows []struct {
			Number int `xml:"r,attr"`
			Cells  []struct {
				Ref    string    `xml:"r,attr"`
				Type   string    `xml:"t,attr"`
				Value  string    `xml:"v"`
				Inline excelText `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err = xml.Unmarshal(pkg.Parts[part], &grid); err != nil {
		return fail(err)
	}
	cells := map[[2]int]string{}
	maxRow, maxCol, lastRow, count := 0, 0, 0, 0
	fixes := []api.ImportFix{}
	for _, row := range grid.Rows {
		if err = ctx.Err(); err != nil {
			return fail(err)
		}
		rowNum := row.Number
		if rowNum == 0 {
			rowNum = lastRow + 1
		}
		lastRow = rowNum
		lastCol := 0
		for _, cell := range row.Cells {
			r, col := cellPosition(cell.Ref)
			if cell.Ref == "" {
				r = rowNum
				col = lastCol + 1
			}
			lastCol = col
			if r < 1 || r > MaxImportRows+1 || col < 1 || col > 128 || r != rowNum {
				return fail(invalid("blob_digest", "sheet dimensions exceed import limits or cell coordinates are invalid"))
			}
			count++
			if count > MaxImportCells {
				return fail(invalid("blob_digest", "sheet exceeds 1,000,000 cells"))
			}
			key := [2]int{r, col}
			if _, duplicate := cells[key]; duplicate {
				return fail(invalid("blob_digest", "duplicate spreadsheet cell"))
			}
			value := cell.Value
			switch cell.Type {
			case "s":
				index, e := strconv.Atoi(value)
				if e != nil || index < 0 || index >= len(shared.Strings) {
					return fail(invalid("blob_digest", "invalid shared string index"))
				}
				value = shared.Strings[index].text()
			case "inlineStr":
				value = cell.Inline.text()
			case "b":
				if value == "1" {
					value = "true"
				} else if value == "0" {
					value = "false"
				} else {
					return fail(invalid("blob_digest", "invalid boolean spreadsheet cell"))
				}
			}
			if fix, ok := dateFixes[cell.Ref]; ok {
				value = fix.To
				fixes = append(fixes, fix)
			}
			cells[key] = value
			maxRow = max(maxRow, r)
			maxCol = max(maxCol, col)
		}
	}
	if maxRow*maxCol > MaxImportCells {
		return fail(invalid("blob_digest", fmt.Sprintf("sheet grid exceeds %d cells", MaxImportCells)))
	}
	rows := make([][]string, maxRow)
	lines := make([]int, maxRow)
	for r := 1; r <= maxRow; r++ {
		rows[r-1] = make([]string, maxCol)
		lines[r-1] = r
		for col := 1; col <= maxCol; col++ {
			rows[r-1][col-1] = cells[[2]int{r, col}]
		}
	}
	return rows, lines, chosen.Name, fixes, nil
}
