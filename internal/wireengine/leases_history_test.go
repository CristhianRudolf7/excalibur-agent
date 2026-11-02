package wireengine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/codeintel"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

// TestSharedHistoryAndLeases covers Sections 13.6, 13.7 and 16.2: two
// edit tools share one history through the workspace bridge, leases
// serialize writers, and a parked run releases its leases.
func TestSharedHistoryAndLeases(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	leases := workspace.NewLeaseManager()
	emb, err := BuildEmbedded(EmbeddedOptions{Leases: leases, RunID: "run_hist_1"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	args, _ := json.Marshal(map[string]string{"path": target, "old_text": "hello", "new_text": "hello world"})
	res, err := emb.Exec.Execute(ctx, "edit_file", args, tools.ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != tools.StatusOK {
		t.Fatalf("edit_file: %+v", res)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "hello world" {
		t.Fatalf("file = %q", data)
	}
	// The tools history and the workspace history observe the same edit.
	if vers := emb.History.Versions(target); len(vers) != 1 {
		t.Fatalf("tools history versions = %d, want 1", len(vers))
	}
	if vers := emb.History.Work.Versions(target); len(vers) != 1 {
		t.Fatalf("workspace history versions = %d, want 1", len(vers))
	}
	// file_history (deferred workspace tool) lists it.
	fhArgs, _ := json.Marshal(map[string]string{"path": target})
	fh, err := emb.Exec.Execute(ctx, "file_history", fhArgs, tools.ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if fh.Status != tools.StatusOK {
		t.Fatalf("file_history: %+v", fh)
	}
	// revert_edit restores the preimage through the shared history.
	editID := emb.History.Work.Versions(target)[0].ID
	revArgs, _ := json.Marshal(map[string]string{"edit_id": editID})
	rev, err := emb.Exec.Execute(ctx, "revert_edit", revArgs, tools.ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rev.Status != tools.StatusOK {
		t.Fatalf("revert_edit: %+v", rev)
	}
	data, _ = os.ReadFile(target)
	if string(data) != "hello" {
		t.Fatalf("reverted file = %q, want hello", data)
	}
	// Leases: a second holder waits, and a parked run releases.
	rel, err := leases.Acquire(target, "run_hist_1", "editing")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := emb.Files.Leases.Holding(target); !ok {
		t.Fatal("adapter must observe the workspace lease")
	}
	NotifyRunState(leases, "run_hist_1", types.RunPaused, "")
	if _, ok := leases.Holding(target); ok {
		t.Fatal("paused runs must release their leases (16.2)")
	}
	rel()
}

// scriptedDiagnostics is a DiagnosticProvider used in tests.
type scriptedDiagnostics struct{ msg string }

func (s scriptedDiagnostics) Diagnostics(_ []string) string { return s.msg }

// TestDiagnosticsAfterEdit covers Sections 13.4 step 6 and 13.8: the
// edit outcome carries the LSP diagnostics of the touched file. The
// production DiagnosticProvider is the codeintel Manager, which
// satisfies the same interface (asserted below).
func TestDiagnosticsAfterEdit(t *testing.T) {
	var _ tools.DiagnosticProvider = (*codeintel.Manager)(nil)
	dir := t.TempDir()
	target := filepath.Join(dir, "a.go")
	if err := os.WriteFile(target, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	emb, err := BuildEmbedded(EmbeddedOptions{
		RunID:       "run_diag_1",
		Diagnostics: scriptedDiagnostics{msg: "a.go:2: type error"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	args, _ := json.Marshal(map[string]string{"path": target, "content": "package a\nvar X int = 1\n"})
	res, err := emb.Exec.Execute(ctx, "write_file", args, tools.ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != tools.StatusOK {
		t.Fatalf("write_file: %+v", res)
	}
	if !contains(res.Output, "diagnostics") || !contains(res.Output, "type error") {
		t.Fatalf("edit outcome must carry LSP diagnostics, got %q", res.Output)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestSharedHistoryUsesExecutingRun(t *testing.T) {
	emb, err := BuildEmbedded(EmbeddedOptions{RunID: "fallback"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "new.txt")
	invoke := func(run, name string, args any) {
		t.Helper()
		raw, _ := json.Marshal(args)
		res, err := emb.Exec.Execute(tools.WithSourceRun(context.Background(), run), name, raw, tools.ExecuteOptions{})
		if err != nil || res.Status != tools.StatusOK {
			t.Fatalf("%s: %+v %v", name, res, err)
		}
	}
	invoke("run_one", "write_file", map[string]string{"path": path, "content": "one"})
	invoke("run_two", "edit_file", map[string]string{"path": path, "old_text": "one", "new_text": "two"})
	invoke("run_two", "apply_patch", map[string]any{"ops": []map[string]string{{"op": "update", "path": path, "old_text": "two", "new_text": "three"}}})
	c := emb.History.Work.Changes([]string{"run_two"}, nil)
	if len(c) != 1 || c[0].Status != "modified" || *c[0].Content != "three" || c[0].Hunks[0].Lines[0] != "-one" {
		t.Fatalf("run attribution: %+v", c)
	}
	versions := emb.History.Work.Versions(path)
	invoke("run_three", "revert_edit", map[string]string{"edit_id": versions[2].ID})
	c = emb.History.Work.Changes([]string{"run_three"}, nil)
	if len(c) != 1 || *c[0].Content != "two" {
		t.Fatalf("revert attribution: %+v", c)
	}
	if len(emb.History.Work.Changes([]string{"fallback"}, nil)) != 0 {
		t.Fatal("used registry identity")
	}
}
