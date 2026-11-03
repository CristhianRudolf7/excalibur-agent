package collections

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/create"
)

func cellRef(col, row int) string {
	letters := ""
	for col > 0 {
		col--
		letters = string(rune('A'+col%26)) + letters
		col /= 26
	}
	return fmt.Sprintf("%s%d", letters, row)
}
func exportText(v any) string {
	if v == nil {
		return ""
	}
	switch v.(type) {
	case []string, []any:
		raw, _ := json.Marshal(v)
		return string(raw)
	}
	return fmt.Sprint(v)
}
func (e *Engine) Export(ctx context.Context, blobs Blobs, p api.CollectionsExportParams) (api.CollectionsExportResult, error) {
	out := api.CollectionsExportResult{}
	if blobs == nil {
		return out, errors.New("blob store unavailable")
	}
	if p.Format != "csv" && p.Format != "xlsx" {
		return out, invalid("format", "format must be csv or xlsx")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := getCollection(ctx, e.db, p.ID, false)
	if err != nil {
		return out, err
	}
	w, args, err := where(c, "", p.Filters)
	if err != nil {
		return out, err
	}
	records, err := readRecords(ctx, e.db, c, w, args, `id`, 0, 0)
	if err != nil {
		return out, err
	}
	rows := [][]string{{}}
	for _, f := range c.Fields {
		rows[0] = append(rows[0], f.Name)
	}
	for _, r := range records {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		row := []string{}
		for _, f := range c.Fields {
			row = append(row, exportText(r.Values[f.ID]))
		}
		rows = append(rows, row)
	}
	var buf bytes.Buffer
	out.Name = "collection_" + strings.TrimPrefix(c.ID, "col_") + "." + p.Format
	if p.Format == "csv" {
		out.Mime = "text/csv; charset=utf-8"
		writer := csv.NewWriter(&buf)
		if err = writer.WriteAll(rows); err != nil {
			return out, err
		}
	} else {
		out.Mime = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		sheet := create.Sheet{Name: "Data", FreezeFirstRow: true}
		for i, row := range rows {
			for j, text := range row {
				cell := create.Cell{Ref: cellRef(j+1, i+1), Type: "string", Value: text, Header: i == 0}
				if i > 0 && text != "" {
					f := c.Fields[j]
					if numeric(f) {
						cell.Type = "number"
						cell.Value = records[i-1].Values[f.ID]
					} else if f.Type == "boolean" {
						cell.Type = "bool"
						cell.Value = records[i-1].Values[f.ID]
					} else if f.Type == "date" || f.Type == "datetime" {
						cell.Type = "date"
					}
				}
				sheet.Cells = append(sheet.Cells, cell)
			}
		}
		pkg, err := create.Build("xlsx", create.Spec{Sheets: []create.Sheet{sheet}}, nil)
		if err != nil {
			return out, err
		}
		encoded, err := pkg.Bytes()
		if err != nil {
			return out, err
		}
		buf.Write(encoded)
	}
	sum := sha256.Sum256(buf.Bytes())
	out.BlobDigest = hex.EncodeToString(sum[:])
	if err = blobs.Put(out.BlobDigest, &buf); err != nil {
		return out, err
	}
	return out, nil
}
