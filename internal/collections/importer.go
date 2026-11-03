package collections

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"golang.org/x/text/unicode/norm"
)

// Blobs is deliberately independent of the harness store and accepts the
// daemon's existing content-addressed blob adapter.
type Blobs interface {
	Put(string, io.Reader) error
	Get(string) (io.ReadCloser, int64, error)
}

const MaxImportBytes = 64 << 20
const MaxImportRows = 100000

type inputTable struct {
	headers    []string
	rows       [][]string
	lines      []int
	name       string
	excelFixes []api.ImportFix
}

func folded(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(strings.TrimSpace(s))) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func readInput(ctx context.Context, data []byte, sheet string) (inputTable, error) {
	var rows [][]string
	var lines []int
	name := "Imported data"
	sourceFixes := []api.ImportFix{}
	if bytes.HasPrefix(data, []byte("PK")) {
		var err error
		rows, lines, name, sourceFixes, err = readSpreadsheet(ctx, data, sheet)
		if err != nil {
			return inputTable{}, err
		}
	} else {
		if !utf8.Valid(data) {
			var b strings.Builder
			for _, v := range data {
				b.WriteRune(rune(v))
			}
			data = []byte(b.String())
		}
		data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
		delimiter := ','
		best := -1
		for _, d := range []rune{',', ';', '\t'} {
			reader := csv.NewReader(bytes.NewReader(data))
			reader.Comma = d
			reader.FieldsPerRecord = -1
			widths := map[int]int{}
			sample := 0
			valid := true
			for i := 0; i < 10; i++ {
				row, err := reader.Read()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					valid = false
					break
				}
				widths[len(row)]++
				sample++
			}
			score := 0
			for width, count := range widths {
				if width > 1 {
					candidate := count*100/sample + min(width, 50)
					if candidate > score {
						score = candidate
					}
				}
			}
			if valid && score > best {
				best = score
				delimiter = d
			}
		}
		r := csv.NewReader(bytes.NewReader(data))
		r.Comma = delimiter
		r.FieldsPerRecord = -1
		for {
			if err := ctx.Err(); err != nil {
				return inputTable{}, err
			}
			row, err := r.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return inputTable{}, invalid("blob_digest", "invalid CSV: "+err.Error())
			}
			line, _ := r.FieldPos(0)
			rows = append(rows, row)
			lines = append(lines, line)
			if len(rows)*len(row) > MaxImportCells {
				return inputTable{}, invalid("blob_digest", "table exceeds 1,000,000 cells")
			}
			if len(rows) > MaxImportRows+20 {
				return inputTable{}, invalid("blob_digest", "too many import rows")
			}
		}
	}
	// Skip leading title/blank rows by selecting the first row with the dominant
	// width. Numeric/date first rows are treated as data with generated headers.
	start, maxWidth, firstNonempty := 0, 0, 0
	for i, row := range rows {
		nonempty := false
		for _, cell := range row {
			if strings.TrimSpace(cell) != "" {
				nonempty = true
				break
			}
		}
		if nonempty {
			firstNonempty = i
			break
		}
	}
	for i, row := range rows[firstNonempty:] {
		if i >= 10 {
			break
		}
		n := 0
		for _, s := range row {
			if strings.TrimSpace(s) != "" {
				n++
			}
		}
		if n > maxWidth {
			start = firstNonempty + i
			maxWidth = n
		}
	}
	if maxWidth == 0 {
		return inputTable{}, invalid("blob_digest", "empty table")
	}
	rows = rows[start:]
	lines = lines[start:]
	first := rows[0]
	hasHeader := true
	seen := map[string]bool{}
	for _, s := range first {
		if strings.TrimSpace(s) == "" {
			continue
		}
		key := folded(s)
		if seen[key] {
			return inputTable{}, invalid("mapping", "duplicate column header: "+s)
		}
		seen[key] = true
		if guessedType(s) != "text" {
			hasHeader = false
		}
	}
	width := len(first)
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}
	if len(rows)*width > MaxImportCells {
		return inputTable{}, invalid("blob_digest", "table grid exceeds 1,000,000 cells")
	}
	if width > 128 {
		return inputTable{}, invalid("blob_digest", "too many columns")
	}
	headers := make([]string, width)
	for i := range headers {
		headers[i] = fmt.Sprintf("Column %d", i+1)
		if hasHeader && i < len(first) && strings.TrimSpace(first[i]) != "" {
			headers[i] = strings.TrimSpace(first[i])
		}
	}
	if hasHeader {
		rows = rows[1:]
		lines = lines[1:]
	}
	out := inputTable{headers: headers, name: name}
	for _, fix := range sourceFixes {
		col, _ := strconv.Atoi(fix.Column)
		if col > 0 && col <= len(headers) {
			fix.Column = headers[col-1]
			out.excelFixes = append(out.excelFixes, fix)
		}
	}
	for i, row := range rows {
		nonempty := false
		for _, s := range row {
			if strings.TrimSpace(s) != "" {
				nonempty = true
			}
		}
		if nonempty {
			out.rows = append(out.rows, row)
			out.lines = append(out.lines, lines[i])
		}
	}
	return out, nil
}
func cellPosition(ref string) (int, int) {
	col, i := 0, 0
	for i < len(ref) && ref[i] >= 'A' && ref[i] <= 'Z' {
		col = col*26 + int(ref[i]-'A'+1)
		i++
	}
	r, _ := strconv.Atoi(ref[i:])
	return r, col
}
func guessedType(s string) api.FieldType {
	s = strings.TrimSpace(s)
	if s == "" {
		return "text"
	}
	if _, e := Normalize(Field{Type: "integer"}, s); e == nil {
		return "integer"
	}
	if _, e := Normalize(Field{Type: "number"}, s); e == nil {
		return "number"
	}
	// 1 and 0 remain numbers unless the column also has textual boolean values.
	if _, e := Normalize(Field{Type: "boolean"}, s); e == nil {
		return "boolean"
	}
	if _, e := Normalize(Field{Type: "date"}, s); e == nil {
		return "date"
	}
	if _, e := Normalize(Field{Type: "datetime"}, s); e == nil {
		return "datetime"
	}
	if _, e := Normalize(Field{Type: "email"}, s); e == nil {
		return "email"
	}
	if _, e := Normalize(Field{Type: "url"}, s); e == nil {
		return "url"
	}
	return "text"
}
func inferFields(t inputTable) []Field {
	out := []Field{}
	for col, name := range t.headers {
		seen := map[string]bool{}
		options := []string{}
		votes := map[api.FieldType]int{}
		count := 0
		for _, row := range t.rows {
			if col >= len(row) || strings.TrimSpace(row[col]) == "" {
				continue
			}
			s := strings.TrimSpace(row[col])
			count++
			guess := guessedType(s)
			if guess == "text" {
				if _, ok := numberWord(s); ok {
					guess = "integer"
				}
			}
			votes[guess]++
			if !seen[s] {
				seen[s] = true
				options = append(options, s)
			}
		}
		kind := majorityType(votes, count)
		f := Field{ID: fmt.Sprintf("f_%d", col+1), Name: name, Type: kind}
		if kind == "text" && count >= 4 && len(options) >= 2 && len(options) <= 12 && len(options)*2 <= count {
			f.Type = "select"
			f.Options = options
		}
		out = append(out, f)
	}
	return out
}

// majorityType picks a typed column when most values agree, so a few
// outliers ("quince", "3/11") become reported conflicts instead of turning
// the whole column into text. Integers and decimals share one family.
func majorityType(votes map[api.FieldType]int, count int) api.FieldType {
	if count == 0 {
		return "text"
	}
	numeric := votes["integer"] + votes["number"]
	best, bestVotes := api.FieldType("text"), 0
	for kind, n := range votes {
		if kind == "text" || kind == "integer" || kind == "number" {
			continue
		}
		if n > bestVotes {
			best, bestVotes = kind, n
		}
	}
	if numeric >= bestVotes && numeric > 0 {
		best, bestVotes = "integer", numeric
		if votes["number"] > 0 {
			best = "number"
		}
	}
	if bestVotes == count || (count >= 3 && bestVotes*10 >= count*7) {
		return best
	}
	return "text"
}

// numberWord reads whole numbers written as Spanish or English words (0-20).
func numberWord(s string) (int, bool) {
	words := map[string]int{
		"cero": 0, "uno": 1, "dos": 2, "tres": 3, "cuatro": 4, "cinco": 5, "seis": 6, "siete": 7, "ocho": 8, "nueve": 9, "diez": 10,
		"once": 11, "doce": 12, "trece": 13, "catorce": 14, "quince": 15, "dieciséis": 16, "dieciseis": 16, "diecisiete": 17, "dieciocho": 18, "diecinueve": 19, "veinte": 20,
		"zero": 0, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10,
		"eleven": 11, "twelve": 12, "thirteen": 13, "fourteen": 14, "fifteen": 15, "sixteen": 16, "seventeen": 17, "eighteen": 18, "nineteen": 19, "twenty": 20,
	}
	n, ok := words[strings.ToLower(strings.TrimSpace(s))]
	return n, ok
}

// dayFirstColumn reports whether a slash-date column is unambiguously
// day/month/year because at least one value has a day above 12.
func dayFirstColumn(t inputTable, col int) bool {
	for _, row := range t.rows {
		if col >= len(row) {
			continue
		}
		words := strings.Fields(row[col])
		if len(words) == 0 {
			continue
		}
		parts := strings.Split(words[0], "/")
		if len(parts) == 3 {
			if a, err := strconv.Atoi(parts[0]); err == nil && a > 12 && a <= 31 {
				return true
			}
		}
	}
	return false
}
func editDistance(a, b string) int {
	aa, bb := []rune(a), []rune(b)
	row := make([]int, len(bb)+1)
	for j := range row {
		row[j] = j
	}
	for i, x := range aa {
		next := make([]int, len(bb)+1)
		next[0] = i + 1
		for j, y := range bb {
			cost := 0
			if x != y {
				cost = 1
			}
			next[j+1] = min(row[j+1]+1, next[j]+1, row[j]+cost)
		}
		row = next
	}
	return row[len(bb)]
}
func mapColumns(t inputTable, c Collection, overrides []api.ImportMapping) ([]api.ImportMapping, error) {
	out := []api.ImportMapping{}
	used := map[string]bool{}
	override := map[string]*string{}
	for _, m := range overrides {
		if !slices.Contains(t.headers, m.Column) {
			return nil, invalid("mapping", "unknown source column "+m.Column)
		}
		override[m.Column] = m.Field
	}
	for _, column := range t.headers {
		var target *string
		if v, ok := override[column]; ok {
			target = v
			if target != nil {
				if _, err := fieldByID(c, *target); err != nil {
					return nil, err
				}
			}
		} else {
			matches := []string{}
			for _, f := range c.Fields {
				if column == f.ID || column == f.Name {
					matches = append(matches, f.ID)
				}
			}
			if len(matches) == 0 {
				for _, f := range c.Fields {
					if folded(column) == folded(f.Name) || folded(column) == folded(f.ID) {
						matches = append(matches, f.ID)
					}
				}
			}
			if len(matches) == 0 && len([]rune(column)) >= 4 {
				distance := 3
				for _, f := range c.Fields {
					d := editDistance(folded(column), folded(f.Name))
					if d < distance {
						distance = d
						matches = []string{f.ID}
					} else if d == distance {
						matches = append(matches, f.ID)
					}
				}
				if distance > 2 {
					matches = nil
				}
			}
			if len(matches) == 1 {
				id := matches[0]
				target = &id
			}
		}
		if target != nil {
			if used[*target] {
				return nil, invalid("mapping", "multiple columns map to "+*target)
			}
			used[*target] = true
		}
		out = append(out, api.ImportMapping{Column: column, Field: target})
	}
	return out, nil
}
func ambiguousDate(s string) bool {
	parts := strings.Split(strings.TrimSpace(s), "/")
	if len(parts) != 3 {
		return false
	}
	a, e1 := strconv.Atoi(parts[0])
	b, e2 := strconv.Atoi(parts[1])
	return e1 == nil && e2 == nil && a >= 1 && a <= 12 && b >= 1 && b <= 12 && a != b
}
func importValue(f Field, raw string, auto bool, row int, column string, dayFirst bool) (any, []api.ImportFix, *api.ImportConflict) {
	s := raw
	fixes := []api.ImportFix{}
	fix := func(to, rule string) {
		if s != to {
			fixes = append(fixes, api.ImportFix{Row: row, Column: column, From: s, To: to, Rule: rule})
			s = to
		}
	}
	conflict := func(problem, suggestion string) (any, []api.ImportFix, *api.ImportConflict) {
		return nil, fixes, &api.ImportConflict{Row: row, Column: column, Value: raw, Problem: problem, Suggestion: suggestion}
	}
	if auto {
		fix(strings.TrimSpace(s), "trim")
	}
	if (f.Type == "date" || f.Type == "datetime") && !dayFirst && ambiguousDate(strings.Split(s, " ")[0]) {
		return conflict("ambiguous_date", "Use an ISO date (YYYY-MM-DD) to specify day and month")
	}
	if auto && numeric(f) {
		if n, ok := numberWord(s); ok {
			fix(strconv.Itoa(n), "number_word")
		}
	}
	if auto && numeric(f) && strings.Count(s, ",") == 1 && !strings.Contains(s, ".") {
		fix(strings.ReplaceAll(s, ",", "."), "decimal_comma")
	}
	if auto && (f.Type == "select" || f.Type == "multi_select") {
		if f.Type == "select" {
			for _, option := range f.Options {
				if strings.EqualFold(s, option) {
					fix(option, "select_case")
					break
				}
			}
		} else {
			var selected []string
			if json.Unmarshal([]byte(s), &selected) != nil {
				selected = strings.Split(s, ",")
			}
			for i, x := range selected {
				for _, option := range f.Options {
					if strings.EqualFold(strings.TrimSpace(x), option) {
						selected[i] = option
						break
					}
				}
			}
			rawList, _ := json.Marshal(selected)
			fix(string(rawList), "select_case")
		}
	}
	if !auto && ((numeric(f) && strings.Contains(s, ",")) || s != strings.TrimSpace(s)) {
		return conflict("invalid_type", "Enable automatic fixes or normalize this value")
	}
	value, err := Normalize(f, s)
	if err != nil {
		problem := "invalid_type"
		message := err.(*ValidationError).Errors[0].Message
		switch {
		case strings.Contains(message, "required"):
			problem = "required_missing"
		case strings.Contains(message, "option"):
			problem = "unknown_option"
		case strings.Contains(message, "range"):
			problem = "out_of_range"
		}
		return conflict(problem, "Supply a valid "+string(f.Type)+" value")
	}
	if (f.Type == "date" || f.Type == "datetime") && value != nil && fmt.Sprint(value) != s {
		if !auto {
			return conflict("invalid_type", "Use ISO date format or enable automatic fixes")
		}
		fix(fmt.Sprint(value), "date_iso")
	}
	return value, fixes, nil
}
func emptyReport(dry bool) api.ImportReport {
	return api.ImportReport{DryRun: dry, Mapping: []api.ImportMapping{}, UnknownColumns: []string{}, Conflicts: []api.ImportConflict{}, FixesApplied: []api.ImportFix{}}
}
func (e *Engine) Import(ctx context.Context, blobs Blobs, p api.CollectionsImportParams) (api.CollectionsImportResult, error) {
	out := api.CollectionsImportResult{Report: emptyReport(p.DryRun)}
	if blobs == nil {
		return out, errors.New("blob store unavailable")
	}
	if p.Fixes != "" && p.Fixes != "auto" && p.Fixes != "none" {
		return out, invalid("fixes", "fixes must be auto or none")
	}
	r, size, err := blobs.Get(p.BlobDigest)
	if err != nil {
		return out, err
	}
	defer r.Close()
	if size > MaxImportBytes {
		return out, invalid("blob_digest", "import exceeds 64 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxImportBytes+1))
	if err != nil {
		return out, err
	}
	if len(data) > MaxImportBytes {
		return out, invalid("blob_digest", "import exceeds 64 MiB")
	}
	input, err := readInput(ctx, data, p.Sheet)
	if err != nil {
		return out, err
	}
	out.Report.TotalRows = len(input.rows)
	if p.Fixes == "none" {
		for _, fix := range input.excelFixes {
			for i, line := range input.lines {
				if line == fix.Row {
					for j, column := range input.headers {
						if column == fix.Column {
							input.rows[i][j] = fix.From
						}
					}
				}
			}
		}
	} else {
		out.Report.FixesApplied = append(out.Report.FixesApplied, input.excelFixes...)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var collectionID string
	err = e.transaction(ctx, func(tx *sql.Tx) error {
		var c Collection
		var err error
		if p.Collection == "" {
			out.ProposedFields = inferFields(input)
			c = prepare(api.CollectionsCreateParams{Name: input.name, Fields: out.ProposedFields})
			if err = e.create(ctx, tx, c); err != nil {
				return err
			}
		} else {
			c, err = getCollection(ctx, tx, p.Collection, false)
			if err != nil {
				return err
			}
		}
		collectionID = c.ID
		mapping, err := mapColumns(input, c, p.Mapping)
		if err != nil {
			return err
		}
		out.Report.Mapping = mapping
		for _, m := range mapping {
			if m.Field == nil {
				out.Report.UnknownColumns = append(out.Report.UnknownColumns, m.Column)
			}
		}
		dayFirst := map[int]bool{}
		for col := range input.headers {
			dayFirst[col] = dayFirstColumn(input, col)
		}
		key := p.UpsertKey
		if key != "" {
			if _, err = fieldByID(c, key); err != nil {
				return err
			}
		}
		for i, row := range input.rows {
			if err = ctx.Err(); err != nil {
				return err
			}
			line := input.lines[i]
			values := map[string]any{}
			columns := map[string]string{}
			rawValues := map[string]string{}
			bad := false
			for col, m := range mapping {
				if m.Field == nil {
					continue
				}
				f, _ := fieldByID(c, *m.Field)
				raw := ""
				if col < len(row) {
					raw = row[col]
				}
				value, fixes, conflict := importValue(f, raw, p.Fixes != "none", line, m.Column, dayFirst[col])
				out.Report.FixesApplied = append(out.Report.FixesApplied, fixes...)
				columns[f.ID] = m.Column
				rawValues[f.ID] = raw
				values[f.ID] = value
				if conflict != nil {
					out.Report.Conflicts = append(out.Report.Conflicts, *conflict)
					bad = true
				}
			}
			for _, f := range c.Fields {
				if _, ok := values[f.ID]; !ok {
					values[f.ID] = nil
				}
			}
			if bad {
				out.Report.Skipped++
				continue
			}
			var existing *Record
			if key != "" && values[key] != nil {
				matches, err := readRecords(ctx, tx, c, quote(key)+"=?", []any{sqlValue(values[key])}, "", 2, 0)
				if err != nil {
					return err
				}
				if len(matches) > 1 {
					out.Report.Conflicts = append(out.Report.Conflicts, api.ImportConflict{Row: line, Column: columns[key], Value: fmt.Sprint(values[key]), Problem: "duplicate", Suggestion: "Use a unique upsert key; multiple existing records match"})
					out.Report.Skipped++
					continue
				}
				if len(matches) == 1 {
					existing = &matches[0]
				}
			}
			if key == "" {
				var n int
				if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM `+table(c)+` WHERE row_hash=?`, hashValues(values)).Scan(&n); err != nil {
					return err
				}
				if n > 0 {
					out.Report.Conflicts = append(out.Report.Conflicts, api.ImportConflict{Row: line, Column: "", Value: "", Problem: "duplicate", Suggestion: "Identical row already exists; choose an upsert key to update"})
					out.Report.Skipped++
					continue
				}
			}
			id := ""
			if existing != nil {
				id = existing.ID
				merged := existing.Values
				for k, v := range values {
					if _, mapped := columns[k]; mapped {
						merged[k] = v
					}
				}
				values = merged
				existing.UpdatedAt = now()
			}
			_, err = writeRecord(ctx, tx, c, values, id, existing)
			if err != nil {
				var validation *ValidationError
				if !errors.As(err, &validation) {
					return err
				}
				for _, v := range validation.Errors {
					problem := "invalid_type"
					if strings.Contains(v.Message, "duplicate") {
						problem = "duplicate"
					}
					if strings.Contains(v.Message, "relation") {
						problem = "relation_not_found"
					}
					if strings.Contains(v.Message, "required") {
						problem = "required_missing"
					}
					if strings.Contains(v.Message, "option") {
						problem = "unknown_option"
					}
					if strings.Contains(v.Message, "range") {
						problem = "out_of_range"
					}
					column := columns[v.Field]
					if column == "" {
						field, _ := fieldByID(c, v.Field)
						column = field.Name
					}
					out.Report.Conflicts = append(out.Report.Conflicts, api.ImportConflict{Row: line, Column: column, Value: rawValues[v.Field], Problem: problem, Suggestion: v.Message})
				}
				out.Report.Skipped++
				continue
			}
			if existing != nil {
				out.Report.Updated++
			} else {
				out.Report.Imported++
			}
		}
		c.UpdatedAt = now()
		if err = saveCollection(ctx, tx, c); err != nil {
			return err
		}
		c, err = getCollection(ctx, tx, c.ID, false)
		if err != nil {
			return err
		}
		if p.Collection != "" || p.CreateSchema {
			out.Collection = &c
		}
		if p.DryRun || (p.Collection == "" && !p.CreateSchema) {
			out.Report.DryRun = true
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		err = nil
	}
	if err == nil && !out.Report.DryRun {
		e.emit(collectionID, "", "imported")
	}
	return out, err
}
