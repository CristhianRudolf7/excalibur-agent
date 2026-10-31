package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Section 13.10/13.7 row: reverting edits with later changes merges,
// or reports the conflict without overwriting.
func TestRevertClean(t *testing.T) {
	h := NewHistory()
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := h.Record(f, []byte("line1\nline2\n"), []byte("line1\nline2 changed\n"), "run-1", "edit_file")
	// The edit tool writes the postimage; the revert sees it on disk.
	if err := os.WriteFile(f, []byte("line1\nline2 changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	newID, err := h.Revert(id, "run-1", "revert_edit")
	if err != nil {
		t.Fatal(err)
	}
	if newID == id {
		t.Fatal("revert must record a new version")
	}
	data, _ := os.ReadFile(f)
	if string(data) != "line1\nline2\n" {
		t.Fatalf("bad restore: %q", data)
	}
	if got := len(h.Versions(f)); got != 2 {
		t.Fatalf("want 2 versions, got %d", got)
	}
}

// Disjoint later change merges: the agent fix and the user addition
// both survive.
func TestRevertThreeWayMerge(t *testing.T) {
	h := NewHistory()
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	base := "a\nb\nc\nd\n"
	mine := "a\nB\nc\nd\n"
	if err := os.WriteFile(f, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	id := h.Record(f, []byte(base), []byte(mine), "run-1", "edit_file")
	// User appends a line at the end afterwards.
	if err := os.WriteFile(f, []byte("a\nB\nc\nd\nuser\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Revert(id, "run-1", "revert_edit"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(f)
	if string(data) != "a\nb\nc\nd\nuser\n" {
		t.Fatalf("merge must keep the user line and drop the agent line: %q", data)
	}
}

// Overlapping later change conflicts: nothing is overwritten and the
// agent sees both diffs.
func TestRevertConflictKeepsFile(t *testing.T) {
	h := NewHistory()
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	base := "a\nb\nc\n"
	mine := "a\nB-agent\nc\n"
	if err := os.WriteFile(f, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	id := h.Record(f, []byte(base), []byte(mine), "run-1", "edit_file")
	later := "a\nB-user\nc\n"
	if err := os.WriteFile(f, []byte(later), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := h.Revert(id, "run-1", "revert_edit")
	if err == nil {
		t.Fatal("want conflict error")
	}
	c, ok := err.(*ConflictError)
	if !ok {
		t.Fatalf("want *ConflictError, got %T", err)
	}
	if !strings.Contains(c.Conflict, "agent change") || !strings.Contains(c.Conflict, "later change") {
		t.Fatalf("conflict must carry both diffs: %q", c.Conflict)
	}
	data, _ := os.ReadFile(f)
	if string(data) != later {
		t.Fatalf("conflict must not overwrite: %q", data)
	}
}

// Section 13.7: crash between intent and effect stays reversible:
// the preimage blob survives with no postimage applied.
func TestHistoryPreimageSurvives(t *testing.T) {
	h := NewHistory()
	f := filepath.Join(t.TempDir(), "a.txt")
	content := []byte("precious\n")
	ref := h.PutBlob(content)
	id := h.Record(f, content, content, "run-9", "write_file-intent")
	v, ok := h.Get(id)
	if !ok {
		t.Fatal("version must exist")
	}
	if v.Preimage != ref {
		t.Fatal("preimage ref must match the stored blob")
	}
	if _, ok := h.Blob(v.Preimage); !ok {
		t.Fatal("preimage blob must be in the store")
	}
}

// Unknown ids and missing blobs fail loudly.
func TestRevertErrors(t *testing.T) {
	h := NewHistory()
	if _, err := h.Revert("ed_nope", "", ""); err == nil {
		t.Fatal("unknown edit must fail")
	}
}

// Merge3 unit cases.
func TestMerge3Cases(t *testing.T) {
	merged, conflicted, _ := Merge3("a\nb\n", "a\nB\n", "a\nb\nc\n")
	if conflicted || merged != "a\nB\nc\n" {
		t.Fatalf("disjoint must merge: %q conflict=%v", merged, conflicted)
	}
	if _, conflicted, _ := Merge3("a\nb\n", "a\nB1\n", "a\nB2\n"); !conflicted {
		t.Fatal("same-line divergence must conflict")
	}
	merged, conflicted, _ = Merge3("a\n", "a\n", "a\nb\n")
	if conflicted || merged != "a\nb\n" {
		t.Fatalf("one-sided must take theirs: %q", merged)
	}
}
