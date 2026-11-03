package collections

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/create"
)

func blob(t *testing.T, b *gateway.MemoryBlobs, data []byte) string {
	t.Helper()
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	if err := b.Put(digest, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	return digest
}
func TestSpanishCSVImport(t *testing.T) {
	e := testEngine(t)
	b := gateway.NewMemoryBlobs()
	min, max := 0.0, 20.0
	c := createTest(t, e, Field{ID: "name", Name: "Curso", Type: "text", Required: true, Unique: true}, Field{ID: "grade", Name: "Calificación", Type: "number", Min: &min, Max: &max}, Field{ID: "date", Name: "Fecha", Type: "date"}, Field{ID: "status", Name: "Estado", Type: "select", Options: []string{"Approved", "Pending"}})
	digest := blob(t, b, []byte("Curso;Calificacion;Fecha;Estado;Unknown\n  Math  ;15,5;31/12/2026;approved;anything\nArt;quince;2026-12-31;Pending;x\nScience;14;03/04/2026;Pending;x\nMath;15,5;2026-12-31;Approved;x\n;12;2026-12-31;Pending;x\nMusic;25;2026-12-31;Unknown;x\n"))
	p := api.CollectionsImportParams{Collection: c.ID, BlobDigest: digest, DryRun: true, Fixes: "auto"}
	out, err := e.Import(ctx, b, p)
	if err != nil {
		t.Fatal(err)
	}
	if out.Report.TotalRows != 6 || out.Report.Imported != 1 || out.Report.Skipped != 5 || len(out.Report.UnknownColumns) != 1 {
		t.Fatalf("%+v", out.Report)
	}
	rules := map[string]bool{}
	for _, f := range out.Report.FixesApplied {
		rules[f.Rule] = true
		if f.Row != 2 && f.Row != 5 {
			t.Fatal(f)
		}
	}
	for _, rule := range []string{"trim", "decimal_comma", "date_iso", "select_case"} {
		if !rules[rule] {
			t.Fatal(out.Report.FixesApplied)
		}
	}
	problems := map[string]bool{}
	for _, c := range out.Report.Conflicts {
		problems[c.Problem] = true
	}
	for _, kind := range []string{"invalid_type", "ambiguous_date", "duplicate", "required_missing", "unknown_option", "out_of_range"} {
		if !problems[kind] {
			t.Fatal(out.Report.Conflicts)
		}
	}
	after, _ := e.Get(ctx, c.ID)
	if after.RecordCount != 0 {
		t.Fatal("dry run wrote records")
	}
	p.DryRun = false
	committed, err := e.Import(ctx, b, p)
	if err != nil || committed.Report.Imported != 1 {
		t.Fatal(committed, err)
	}
	after, _ = e.Get(ctx, c.ID)
	if after.RecordCount != 1 {
		t.Fatal(after)
	}
	if !out.Report.DryRun || committed.Report.DryRun {
		t.Fatal("dry run flags")
	}
}
func TestImportLatin1DelimiterMappingAndInference(t *testing.T) {
	e := testEngine(t)
	b := gateway.NewMemoryBlobs()
	data := []byte("T\xedtulo;Valor\nM\xfasica;15,5\nArte;20\n")
	digest := blob(t, b, data)
	out, err := e.Import(ctx, b, api.CollectionsImportParams{BlobDigest: digest, DryRun: true})
	if err != nil || len(out.ProposedFields) != 2 || out.ProposedFields[0].Name != "Título" || out.ProposedFields[1].Type != "number" || out.Report.Imported != 2 {
		t.Fatal(out, err)
	}
	all, _ := e.List(ctx)
	if len(all) != 0 {
		t.Fatal("schema inference created a collection")
	}
	out, err = e.Import(ctx, b, api.CollectionsImportParams{BlobDigest: digest, CreateSchema: true})
	if err != nil || out.Collection == nil || out.Collection.RecordCount != 2 {
		t.Fatal(out, err)
	}
	c := createTest(t, e, Field{ID: "title", Name: "Título", Type: "text"}, Field{ID: "amount", Name: "Amount", Type: "number"})
	mapped := "amount"
	out, err = e.Import(ctx, b, api.CollectionsImportParams{Collection: c.ID, BlobDigest: digest, Mapping: []api.ImportMapping{{Column: "Valor", Field: &mapped}}})
	if err != nil || out.Report.Imported != 2 || len(out.Report.UnknownColumns) != 0 {
		t.Fatal(out, err)
	}
	tab := blob(t, b, []byte("Name\tAmount\nAna\t2\n"))
	c2 := createTest(t, e, Field{ID: "name", Name: "Name", Type: "text"}, Field{ID: "amount", Name: "Amount", Type: "integer"})
	out, err = e.Import(ctx, b, api.CollectionsImportParams{Collection: c2.ID, BlobDigest: tab})
	if err != nil || out.Report.Imported != 1 {
		t.Fatal(out, err)
	}
	// A single semicolon column plus decimal commas must not select comma CSV.
	out, err = e.Import(ctx, b, api.CollectionsImportParams{BlobDigest: blob(t, b, []byte("Name;Amount\nAna;15,5\n")), DryRun: true})
	if err != nil || out.Report.Imported != 1 || len(out.ProposedFields) != 2 {
		t.Fatal(out, err)
	}
}
func TestImportDuplicatesUpsertNoneAndRelations(t *testing.T) {
	e := testEngine(t)
	b := gateway.NewMemoryBlobs()
	c := createTest(t, e, Field{ID: "name", Name: "Name", Type: "text"}, Field{ID: "amount", Name: "Amount", Type: "number"})
	p := api.CollectionsImportParams{Collection: c.ID, BlobDigest: blob(t, b, []byte("Name,Amount\nAna,15.5\nAna,15.5\n")), DryRun: true}
	out, err := e.Import(ctx, b, p)
	if err != nil || out.Report.Imported != 1 || out.Report.Skipped != 1 || out.Report.Conflicts[0].Row != 3 {
		t.Fatal(out, err)
	}
	p.DryRun = false
	if _, err = e.Import(ctx, b, p); err != nil {
		t.Fatal(err)
	}
	p.UpsertKey = "name"
	p.BlobDigest = blob(t, b, []byte("Name,Amount\nAna,20\n"))
	p.DryRun = true
	out, err = e.Import(ctx, b, p)
	if err != nil || out.Report.Updated != 1 {
		t.Fatal(out, err)
	}
	rows, _ := e.Query(ctx, api.RecordsListParams{Collection: c.ID})
	if rows.Records[0].Values["amount"] != 15.5 {
		t.Fatal("dry run mutated existing record")
	}
	p.DryRun = false
	out, err = e.Import(ctx, b, p)
	if err != nil || out.Report.Updated != 1 {
		t.Fatal(out, err)
	}
	p.Fixes = "none"
	p.BlobDigest = blob(t, b, []byte("Name;Amount\nAna;15,5\n"))
	out, err = e.Import(ctx, b, p)
	if err != nil || out.Report.Skipped != 1 || len(out.Report.FixesApplied) != 0 {
		t.Fatal(out, err)
	}
	child := createTest(t, e, Field{ID: "parent", Name: "Parent", Type: "relation", Target: c.ID})
	out, err = e.Import(ctx, b, api.CollectionsImportParams{Collection: child.ID, BlobDigest: blob(t, b, []byte("Parent\nmissing\n"))})
	if err != nil || out.Report.Conflicts[0].Problem != "relation_not_found" {
		t.Fatal(out, err)
	}
}
func TestHeaderDetectionFuzzyMappingAndSelectInference(t *testing.T) {
	input, err := readInput(ctx, []byte("Report\nNme,Amount\nAna,1\nBob,2\n"), "")
	if err != nil || input.headers[0] != "Nme" || input.lines[0] != 3 {
		t.Fatal(input, err)
	}
	c := Collection{Fields: []Field{{ID: "name", Name: "Name", Type: "text"}, {ID: "amount", Name: "Amount", Type: "number"}}}
	input.headers[0] = "Náme"
	mapping, err := mapColumns(input, c, nil)
	if err != nil || mapping[0].Field == nil || *mapping[0].Field != "name" {
		t.Fatal(mapping, err)
	}
	input.headers[0] = "Nmae"
	mapping, err = mapColumns(input, c, nil)
	if err != nil || mapping[0].Field == nil {
		t.Fatal(mapping, err)
	}
	input, err = readInput(ctx, []byte("1;2\n3;4\n"), "")
	if err != nil || len(input.rows) != 2 || input.headers[0] != "Column 1" {
		t.Fatal(input, err)
	}
	fields := inferFields(inputTable{headers: []string{"State"}, rows: [][]string{{"A"}, {"B"}, {"A"}, {"B"}}})
	if fields[0].Type != "select" || len(fields[0].Options) != 2 {
		t.Fatal(fields)
	}
}
func TestExporterRoundTripCSVAndXLSX(t *testing.T) {
	e := testEngine(t)
	b := gateway.NewMemoryBlobs()
	fields := []Field{{ID: "name", Name: "Name", Type: "text"}, {ID: "amount", Name: "Amount", Type: "money", Currency: "PEN"}, {ID: "date", Name: "Date", Type: "date"}, {ID: "active", Name: "Active", Type: "boolean"}, {ID: "tags", Name: "Tags", Type: "multi_select", Options: []string{"A", "B"}}}
	source := createTest(t, e, fields...)
	insertTest(t, e, source, map[string]any{"name": "=SUM(1,2)", "amount": 15.5, "date": "2026-12-31", "active": true, "tags": []string{"A", "B"}})
	for _, format := range []string{"csv", "xlsx"} {
		t.Run(format, func(t *testing.T) {
			out, err := e.Export(ctx, b, api.CollectionsExportParams{ID: source.ID, Format: format})
			if err != nil || !strings.HasSuffix(out.Name, "."+format) {
				t.Fatal(out, err)
			}
			target := createTest(t, e, fields...)
			result, err := e.Import(ctx, b, api.CollectionsImportParams{Collection: target.ID, BlobDigest: out.BlobDigest})
			if err != nil || result.Report.Imported != 1 || len(result.Report.Conflicts) > 0 {
				t.Fatal(result, err)
			}
			a, _ := e.Query(ctx, api.RecordsListParams{Collection: source.ID})
			z, _ := e.Query(ctx, api.RecordsListParams{Collection: target.ID})
			ra, _ := json.Marshal(a.Records[0].Values)
			rz, _ := json.Marshal(z.Records[0].Values)
			if string(ra) != string(rz) {
				t.Fatalf("%s != %s", ra, rz)
			}
		})
	}
}
func TestXLSXSheetSelectionAndCompleteExtraction(t *testing.T) {
	pkg, err := create.Build("xlsx", create.Spec{Sheets: []create.Sheet{{Name: "First", Cells: []create.Cell{{Ref: "A1", Value: "Name"}, {Ref: "A2", Value: "Ana"}}}, {Name: "Second", Cells: []create.Cell{{Ref: "A1", Value: "Name"}, {Ref: "A2", Value: "Bob"}, {Ref: "A201", Value: "Last"}}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := pkg.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	input, err := readInput(ctx, data, "Second")
	if err != nil || len(input.rows) != 2 || input.rows[1][0] != "Last" || input.lines[1] != 201 {
		t.Fatal(input, err)
	}
	if _, err = readInput(ctx, data, "Missing"); err == nil {
		t.Fatal("unknown sheet accepted")
	}
}

func TestExcelSerialDates(t *testing.T) {
	pkg, err := create.Build("xlsx", create.Spec{Sheets: []create.Sheet{{Name: "Dates", Cells: []create.Cell{{Ref: "A1", Value: "Date"}, {Ref: "A2", Type: "number", Value: 46022}}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pkg.Parts["xl/worksheets/sheet1.xml"] = bytes.Replace(pkg.Parts["xl/worksheets/sheet1.xml"], []byte(`<c r="A2"`), []byte(`<c r="A2" s="2"`), 1)
	pkg.Parts["xl/styles.xml"] = bytes.Replace(pkg.Parts["xl/styles.xml"], []byte(`</cellXfs>`), []byte(`<xf numFmtId="14"/></cellXfs>`), 1)
	data, err := pkg.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	input, err := readInput(ctx, data, "Dates")
	if err != nil || input.rows[0][0] != "2025-12-31" || len(input.excelFixes) != 1 {
		t.Fatal(input, err)
	}
	e := testEngine(t)
	b := gateway.NewMemoryBlobs()
	out, err := e.Import(ctx, b, api.CollectionsImportParams{BlobDigest: blob(t, b, data), DryRun: true})
	if err != nil || out.ProposedFields[0].Type != "date" || out.Report.FixesApplied[0].Rule != "excel_serial_date" {
		t.Fatal(out, err)
	}
}

func TestUpsertPreservesUnmappedRequiredFields(t *testing.T) {
	e := testEngine(t)
	b := gateway.NewMemoryBlobs()
	c := createTest(t, e, Field{ID: "key", Name: "Key", Type: "text", Unique: true}, Field{ID: "name", Name: "Name", Type: "text", Required: true}, Field{ID: "amount", Name: "Amount", Type: "number"})
	r := insertTest(t, e, c, map[string]any{"key": "one", "name": "Ana", "amount": 15})
	out, err := e.Import(ctx, b, api.CollectionsImportParams{Collection: c.ID, BlobDigest: blob(t, b, []byte("Key,Amount\none,20\ntwo,30\n")), UpsertKey: "key"})
	if err != nil || out.Report.Updated != 1 || out.Report.Skipped != 1 || out.Report.Conflicts[0].Problem != "required_missing" {
		t.Fatal(out, err)
	}
	got, err := e.Record(ctx, api.RecordsIDParams{Collection: c.ID, ID: r.ID})
	if err != nil || got.Record.Values["name"] != "Ana" || got.Record.Values["amount"] != float64(20) {
		t.Fatal(got, err)
	}
}

func TestSpreadsheetImportBeyondPreviewCellBudget(t *testing.T) {
	// Collection imports must read all rows even when a document preview would stop.
	const count = 10001
	var csv strings.Builder
	csv.WriteString("Name,Amount\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&csv, "Row %d,%d\n", i, i)
	}
	e := testEngine(t)
	b := gateway.NewMemoryBlobs()
	out, err := e.Import(ctx, b, api.CollectionsImportParams{BlobDigest: blob(t, b, []byte(csv.String())), CreateSchema: true})
	if err != nil || out.Report.Imported != count {
		t.Fatal(out.Report, err)
	}
	exported, err := e.Export(ctx, b, api.CollectionsExportParams{ID: out.Collection.ID, Format: "xlsx"})
	if err != nil {
		t.Fatal(err)
	}
	r, _, err := b.Get(exported.BlobDigest)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	table, err := readInput(ctx, data, "")
	if err != nil || len(table.rows) != count {
		t.Fatal(len(table.rows), err)
	}
}

func TestSpreadsheetLeadingBlankRowsAndCSVGridBudget(t *testing.T) {
	pkg, err := create.Build("xlsx", create.Spec{Sheets: []create.Sheet{{Name: "Leading", Cells: []create.Cell{{Ref: "A20", Value: "Name"}, {Ref: "A21", Value: "Ana"}}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := pkg.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	table, err := readInput(ctx, data, "")
	if err != nil || len(table.rows) != 1 || table.lines[0] != 21 || table.headers[0] != "Name" {
		t.Fatal(table, err)
	}
	headers := []string{}
	for i := 0; i < 128; i++ {
		headers = append(headers, fmt.Sprintf("Field%d", i))
	}
	large := strings.Join(headers, ",") + "\n" + strings.Repeat("value\n", 8000)
	if _, err = readInput(ctx, []byte(large), ""); err == nil {
		t.Fatal("sparse CSV exceeded grid budget")
	}
}
