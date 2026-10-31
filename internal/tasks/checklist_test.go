// Checklist tests (Section 8.8; verification row 8.11.8): the
// README regenerates only the marked section through lease and
// history, a user edit inside or outside the markers survives in
// the history, and eval writes outside the evaluated tree.
package tasks

import (
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func testTime() time.Time { return time.Date(2026, 10, 28, 14, 2, 0, 0, time.UTC) }

// fakeEditor records lease writes with their pre-images.
type fakeEditor struct {
	files   map[string]string
	writes  []string
	reasons []string
	history map[string][]string
}

func newFakeEditor() *fakeEditor {
	return &fakeEditor{files: map[string]string{}, history: map[string][]string{}}
}

func (f *fakeEditor) Read(path string) (string, error) {
	return f.files[path], nil
}

func (f *fakeEditor) WriteLeased(path, content, reason string) error {
	if old, ok := f.files[path]; ok {
		f.history[path] = append(f.history[path], old)
	}
	f.files[path] = content
	f.writes = append(f.writes, path)
	f.reasons = append(f.reasons, reason)
	return nil
}

func TestSplicePreservesOutsideMarkers(t *testing.T) {
	readme := "# Tests\n\nRun `make test`.\n\n" + ChecklistBegin + "\nold\n" + ChecklistEnd + "\n\n## License\nMIT\n"
	next := SpliceReadme(readme, "- [x] a — passed\n")
	if !strings.Contains(next, "Run `make test`.") || !strings.Contains(next, "## License\nMIT") {
		t.Fatalf("outside content changed:\n%s", next)
	}
	if strings.Contains(next, "old") {
		t.Fatalf("marked section not replaced:\n%s", next)
	}
	if strings.Count(next, ChecklistBegin) != 1 {
		t.Fatalf("markers duplicated:\n%s", next)
	}
}

func TestSpliceAppendsWithoutMarkers(t *testing.T) {
	next := SpliceReadme("# Tests\n", "- [ ] a — not run\n")
	if !strings.Contains(next, "# Tests") || !strings.Contains(next, ChecklistBegin) {
		t.Fatalf("section not appended:\n%s", next)
	}
}

func TestRegenerateChecklistKeepsUserEdits(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()
	_, chk := seedFreshCriterion(t, store, scope, "t.go")
	store.RecordFileEdit("t.go", "fp1")
	seedEvidence(store, chk, types.EvidencePass,
		CodeVersion{Commit: "abc"}, map[string]string{"t.go": "fp1"}, 0)
	names := map[types.CheckID]string{chk.ID: "test_login_ok"}

	ed := newFakeEditor()
	path := "tests/README.md"
	ed.files[path] = "# Tests\n\nHand-written notes.\n\n" + ChecklistBegin + "\nuser draft\n" + ChecklistEnd + "\n"
	if err := RegenerateChecklist(store, ed, path, scope, CodeVersion{Commit: "abc"}, names, testTime()); err != nil {
		t.Fatal(err)
	}
	got := ed.files[path]
	if !strings.Contains(got, "Hand-written notes.") {
		t.Fatalf("outside section touched:\n%s", got)
	}
	if !strings.Contains(got, "- [x] test_login_ok — passed") {
		t.Fatalf("line not rendered:\n%s", got)
	}
	if len(ed.history[path]) != 1 || !strings.Contains(ed.history[path][0], "user draft") {
		t.Fatal("user edit inside markers must survive in the history")
	}
	if len(ed.reasons) != 1 || ed.reasons[0] == "" {
		t.Fatal("write must go through the leased pipeline with a reason")
	}
}

func TestChecklistLineStates(t *testing.T) {
	line := ChecklistLineFor("bench_startup", nil, CodeVersion{Commit: "x"}, testTime())
	if line.Checked || !strings.Contains(line.Detail, "not run") {
		t.Fatalf("missing evidence line wrong: %+v", line)
	}
	rec := &EvidenceRecord{Evidence: types.Evidence{
		Result: types.EvidenceFail, CodeVersion: CodeVersion{Commit: "old"}.String(),
		ID: types.NewEvidenceID(),
	}}
	line = ChecklistLineFor("test_export_pdf", rec, CodeVersion{Commit: "new"}, testTime())
	if line.Checked || !strings.Contains(line.Detail, "fail") {
		t.Fatalf("failed line wrong: %+v", line)
	}
}

func TestEvalPathOutsideTree(t *testing.T) {
	normal := ChecklistPath("/proj", "tests", false)
	eval := ChecklistPath("/proj", "tests", true)
	if !strings.HasPrefix(normal, "/proj/") {
		t.Fatalf("normal path inside project expected: %q", normal)
	}
	if strings.HasPrefix(eval, "/proj/") {
		t.Fatalf("eval path must stay outside the evaluated tree: %q", eval)
	}
}
