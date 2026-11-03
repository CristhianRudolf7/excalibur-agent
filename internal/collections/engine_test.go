package collections

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

var ctx = context.Background()

func testEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}
func createTest(t *testing.T, e *Engine, fields ...Field) Collection {
	t.Helper()
	c, err := e.Create(ctx, api.CollectionsCreateParams{Name: "Grades", Fields: fields})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func insertTest(t *testing.T, e *Engine, c Collection, v map[string]any) Record {
	t.Helper()
	r, err := e.Insert(ctx, api.RecordsCreateParams{Collection: c.ID, Values: v})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestNormalizeTypes(t *testing.T) {
	cases := []struct {
		f       Field
		v, want any
		bad     bool
	}{
		{Field{Type: "text"}, "hello", "hello", false}, {Field{Type: "long_text"}, "line\nline", "line\nline", false},
		{Field{Type: "number"}, "15,5", 15.5, false}, {Field{Type: "number"}, "quince", nil, true},
		{Field{Type: "integer"}, "15", float64(15), false}, {Field{Type: "integer"}, 1.5, nil, true},
		{Field{Type: "money", Currency: "PEN"}, "500", float64(500), false},
		{Field{Type: "date"}, "31/12/2026", "2026-12-31", false}, {Field{Type: "date"}, "2026-02-30", nil, true},
		{Field{Type: "datetime"}, "03/11/2026 14:30", "2026-11-03T14:30:00Z", false},
		{Field{Type: "boolean"}, "sí", true, false}, {Field{Type: "boolean"}, "no", false, false}, {Field{Type: "boolean"}, "perhaps", nil, true},
		{Field{Type: "select", Options: []string{"A"}}, "A", "A", false}, {Field{Type: "select", Options: []string{"A"}}, "a", nil, true},
		{Field{Type: "multi_select", Options: []string{"A", "B"}}, []any{"A", "A"}, []string{"A"}, false},
		{Field{Type: "email"}, "a@example.test", "a@example.test", false}, {Field{Type: "email"}, "bad", nil, true},
		{Field{Type: "url"}, "https://example.test", "https://example.test", false}, {Field{Type: "url"}, "file:///etc/passwd", nil, true},
		{Field{Type: "file"}, strings.Repeat("a", 64), strings.Repeat("a", 64), false}, {Field{Type: "file"}, "no", nil, true},
		{Field{Type: "relation"}, "rec_1", "rec_1", false}, {Field{Type: "text", Required: true}, nil, nil, true},
	}
	for _, tc := range cases {
		t.Run(string(tc.f.Type)+"/"+stringify(tc.v), func(t *testing.T) {
			v, err := Normalize(tc.f, tc.v)
			if (err != nil) != tc.bad || (!tc.bad && !reflect.DeepEqual(v, tc.want)) {
				t.Fatalf("got %#v %v; want %#v", v, err, tc.want)
			}
		})
	}
	min, max := 0.0, 20.0
	f := Field{Type: "number", Min: &min, Max: &max}
	if _, err := Normalize(f, 21); err == nil {
		t.Fatal("range ignored")
	}
}
func stringify(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return "value"
}
func TestCRUDValidationAndPersistence(t *testing.T) {
	dir := t.TempDir()
	events := []api.CollectionsChangedEvent{}
	e, err := Open(dir, func(ev api.CollectionsChangedEvent) { events = append(events, ev) })
	if err != nil {
		t.Fatal(err)
	}
	c := createTest(t, e, Field{ID: "name", Name: "Course", Type: "text", Required: true, Unique: true}, Field{ID: "grade", Name: "Grade", Type: "number"})
	if _, err = e.Insert(ctx, api.RecordsCreateParams{Collection: c.ID, Values: map[string]any{"bad": true}}); err == nil {
		t.Fatal("validation ignored")
	}
	r := insertTest(t, e, c, map[string]any{"name": "Math", "grade": "15,5"})
	if _, err = e.Insert(ctx, api.RecordsCreateParams{Collection: c.ID, Values: r.Values}); err == nil {
		t.Fatal("unique ignored")
	}
	r, err = e.UpdateRecord(ctx, api.RecordsUpdateParams{Collection: c.ID, ID: r.ID, Values: map[string]any{"grade": 18}})
	if err != nil || r.Values["name"] != "Math" {
		t.Fatal(r, err)
	}
	if len(events) != 3 {
		t.Fatal(events)
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	e, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	got, err := e.Get(ctx, c.ID)
	if err != nil || got.RecordCount != 1 {
		t.Fatal(got, err)
	}
	if filepath.Base(dir) == "" {
		t.Fatal("invalid path")
	}
	if err = e.DeleteRecord(ctx, api.RecordsIDParams{Collection: c.ID, ID: r.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Record(ctx, api.RecordsIDParams{Collection: c.ID, ID: r.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
func TestFieldsDryRunBackupsAndTrash(t *testing.T) {
	e := testEngine(t)
	c := createTest(t, e, Field{ID: "name", Name: "Name", Type: "text"}, Field{ID: "grade", Name: "Grade", Type: "text"})
	r := insertTest(t, e, c, map[string]any{"name": "Math", "grade": "quince"})
	p := api.CollectionsFieldsParams{ID: c.ID, ChangeType: []api.FieldTypeChange{{Field: "grade", Type: "number"}}, DryRun: true}
	out, err := e.Fields(ctx, p)
	if err != nil || len(out.Report) != 1 || len(out.Report[0].Failed) != 1 || out.Report[0].Failed[0].RecordID != r.ID {
		t.Fatal(out, err)
	}
	p.DryRun = false
	if _, err = e.Fields(ctx, p); err == nil {
		t.Fatal("bad conversion committed")
	}
	if _, err = e.UpdateRecord(ctx, api.RecordsUpdateParams{Collection: c.ID, ID: r.ID, Values: map[string]any{"grade": "15,5"}}); err != nil {
		t.Fatal(err)
	}
	p.DryRun = true
	out, err = e.Fields(ctx, p)
	if err != nil || out.Report[0].Converted != 1 {
		t.Fatal(out, err)
	}
	got, _ := e.Get(ctx, c.ID)
	if got.Version != 1 || got.Fields[1].Type != "text" {
		t.Fatal(got)
	}
	p.DryRun = false
	if _, err = e.Fields(ctx, p); err != nil {
		t.Fatal(err)
	}
	out, err = e.Fields(ctx, api.CollectionsFieldsParams{ID: c.ID, Rename: []api.FieldRename{{Field: "name", Name: "Course"}}})
	if err != nil || out.Collection.Fields[0].ID != "name" {
		t.Fatal(out, err)
	}
	before, _ := e.Get(ctx, c.ID)
	if _, err = e.Fields(ctx, api.CollectionsFieldsParams{ID: c.ID, Add: []Field{{ID: "required", Name: "Required", Type: "text", Required: true}}}); err == nil {
		t.Fatal("required add to nonempty collection succeeded")
	}
	after, _ := e.Get(ctx, c.ID)
	if after.Version != before.Version {
		t.Fatal("failed migration advanced version")
	}
	if _, err = e.Fields(ctx, api.CollectionsFieldsParams{ID: c.ID, Remove: []string{"grade"}}); err != nil {
		t.Fatal(err)
	}
	restored, err := e.Restore(ctx, c.ID, 1)
	if err != nil || restored.Fields[1].Type != "text" {
		t.Fatal(restored, err)
	}
	detail, err := e.Record(ctx, api.RecordsIDParams{Collection: c.ID, ID: r.ID})
	if err != nil || detail.Record.Values["grade"] != "15,5" {
		t.Fatal(detail, err)
	}
	versions, err := e.Versions(ctx, c.ID)
	if err != nil || len(versions) == 0 {
		t.Fatal(versions, err)
	}
	if err = e.Delete(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Get(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	trash, _ := e.Trash(ctx)
	if len(trash) != 1 {
		t.Fatal(trash)
	}
	if _, err = e.Restore(ctx, c.ID, 0); err != nil {
		t.Fatal(err)
	}
	e.versions = 2
	for i := 0; i < 4; i++ {
		name := "Updated"
		if _, err = e.Update(ctx, api.CollectionsUpdateParams{ID: c.ID, Name: &name}); err != nil {
			t.Fatal(err)
		}
	}
	versions, _ = e.Versions(ctx, c.ID)
	if len(versions) != 2 {
		t.Fatal(versions)
	}
	if err = e.Delete(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	_, err = e.db.Exec(`UPDATE collection_meta SET deleted_at=? WHERE id=?`, time.Now().UTC().Add(-31*24*time.Hour).Format(time.RFC3339Nano), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.purge(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Restore(ctx, c.ID, 0); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
func TestRelations(t *testing.T) {
	e := testEngine(t)
	parent := createTest(t, e, Field{ID: "name", Name: "Name", Type: "text"})
	p := insertTest(t, e, parent, map[string]any{"name": "University"})
	child := createTest(t, e, Field{ID: "name", Name: "Name", Type: "text"}, Field{ID: "university", Name: "University", Type: "relation", Target: parent.ID})
	if _, err := e.Insert(ctx, api.RecordsCreateParams{Collection: child.ID, Values: map[string]any{"university": "missing"}}); err == nil {
		t.Fatal("missing relation accepted")
	}
	r := insertTest(t, e, child, map[string]any{"name": "Math", "university": p.ID})
	got, err := e.Record(ctx, api.RecordsIDParams{Collection: child.ID, ID: r.ID})
	if err != nil || len(got.Related) != 1 || got.Related[0].Records[0].ID != p.ID {
		t.Fatal(got, err)
	}
	got, err = e.Record(ctx, api.RecordsIDParams{Collection: parent.ID, ID: p.ID})
	if err != nil || len(got.Related) != 1 {
		t.Fatal(got, err)
	}
	err = e.DeleteRecord(ctx, api.RecordsIDParams{Collection: parent.ID, ID: p.ID})
	if err == nil || !strings.Contains(err.(*ValidationError).Errors[0].Message, r.ID) {
		t.Fatal(err)
	}
	if err = e.Delete(ctx, parent.ID); err == nil {
		t.Fatal("referenced collection deleted")
	}
	if err = e.DeleteRecord(ctx, api.RecordsIDParams{Collection: child.ID, ID: r.ID}); err != nil {
		t.Fatal(err)
	}
	if err = e.DeleteRecord(ctx, api.RecordsIDParams{Collection: parent.ID, ID: p.ID}); err != nil {
		t.Fatal(err)
	}
}
func TestQueryFiltersPaginationAndSummary(t *testing.T) {
	e := testEngine(t)
	c := createTest(t, e, Field{ID: "name", Name: "Name", Type: "text"}, Field{ID: "grade", Name: "Grade", Type: "number"}, Field{ID: "weight", Name: "Weight", Type: "integer"})
	for _, v := range []map[string]any{{"name": "Math", "grade": 10, "weight": 1}, {"name": "Math", "grade": 20, "weight": 3}, {"name": "Art", "grade": nil, "weight": 7}} {
		insertTest(t, e, c, v)
	}
	q := api.RecordsListParams{Collection: c.ID, Query: "Math", Sort: []api.RecordSort{{Field: "grade", Dir: "desc"}}, Limit: 1}
	first, err := e.Query(ctx, q)
	if err != nil || first.Total != 2 || len(first.Records) != 1 || first.Records[0].Values["grade"] != float64(20) || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	q.Cursor = first.NextCursor
	second, err := e.Query(ctx, q)
	if err != nil || second.Records[0].ID == first.Records[0].ID || second.NextCursor != "" {
		t.Fatal(second, err)
	}
	q.Query = "Art"
	if _, err = e.Query(ctx, q); err == nil {
		t.Fatal("cursor reused for other query")
	}
	for _, tc := range []struct {
		op    string
		value any
		want  int
	}{{"eq", 10, 1}, {"neq", 10, 1}, {"lt", 20, 1}, {"lte", 20, 2}, {"gt", 10, 1}, {"gte", 20, 1}, {"in", []any{10, 20}, 2}, {"is_empty", nil, 1}, {"not_empty", nil, 2}} {
		got, err := e.Query(ctx, api.RecordsListParams{Collection: c.ID, Filters: []api.CollectionFilter{{Field: "grade", Op: tc.op, Value: tc.value}}})
		if err != nil || got.Total != tc.want {
			t.Fatalf("%s: %+v %v", tc.op, got, err)
		}
	}
	got, err := e.Query(ctx, api.RecordsListParams{Collection: c.ID, Filters: []api.CollectionFilter{{Field: "name", Op: "contains", Value: "ath"}}})
	if err != nil || got.Total != 2 {
		t.Fatal(got, err)
	}
	cards := []api.SummaryCard{}
	for _, kind := range []string{"count", "sum", "avg", "min", "max", "weighted_avg"} {
		cards = append(cards, api.SummaryCard{ID: kind, Title: kind, Kind: kind, Field: "grade", WeightField: "weight", GroupBy: "name"})
	}
	if _, err = e.Update(ctx, api.CollectionsUpdateParams{ID: c.ID, Summary: &cards}); err != nil {
		t.Fatal(err)
	}
	summary, err := e.Summary(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{3, 30, 15, 10, 20, 17.5}
	for i, v := range summary {
		n, ok := v.Value.(float64)
		if !ok {
			n = float64(v.Value.(int64))
		}
		if n != want[i] || len(v.Series) != 2 {
			t.Fatal(summary)
		}
	}
}
func TestSchemaValidation(t *testing.T) {
	e := testEngine(t)
	for _, fields := range [][]Field{{{ID: `name";DROP`, Name: "Name", Type: "text"}}, {{ID: "name", Name: "Name", Type: "nonsense"}}, {{ID: "amount", Name: "Amount", Type: "money"}}, {{ID: "name", Name: "Name", Type: "text"}, {ID: "NAME", Name: "Other", Type: "text"}}} {
		if _, err := e.Create(ctx, api.CollectionsCreateParams{Name: "Invalid", Fields: fields}); err == nil {
			t.Fatal(fields)
		}
	}
	proposal, err := e.Propose(ctx, api.CollectionsCreateParams{Name: "Proposal", Fields: []Field{{Name: "Name", Type: "text"}}})
	if err != nil || proposal.Fields[0].ID == "" {
		t.Fatal(proposal, err)
	}
	list, _ := e.List(ctx)
	if len(list) != 0 {
		t.Fatal("proposal persisted")
	}
}
