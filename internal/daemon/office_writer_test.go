package daemon

import (
	"context"
	"encoding/json"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"path/filepath"
	"testing"
)

func TestDaemonOfficeWriterInvocation(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	ctx := context.Background()
	db, e := store.Open(ctx, filepath.Join(root, "office.db"), store.DefaultOptions(), migrations.Main())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = db.Close() })
	if e = memory.EnsureSchema(ctx, db); e != nil {
		t.Fatal(e)
	}
	st := orchestrator.NewMemoryStore()
	orch := orchestrator.New(orchestrator.DefaultConfig(), st, orchestrator.NoopJournal{})
	svc, e := Assemble(AssembleOptions{Store: st, Orch: orch, Memory: memory.NewService(db)})
	if e != nil {
		t.Fatal(e)
	}
	result, e := svc.Exec.Execute(ctx, "call_tool", json.RawMessage(`{"name":"document_create","arguments":{"path":"daemon.xlsx","format":"xlsx","spec":{"sheets":[{"name":"Data","cells":[{"ref":"A1","value":"Daemon"}]}]}}}`), tools.ExecuteOptions{})
	if e != nil || result.Status != tools.StatusOK {
		t.Fatal(result, e)
	}
	var outcome struct {
		SHA string `json:"sha256"`
	}
	if e = json.Unmarshal([]byte(result.Output), &outcome); e != nil {
		t.Fatal(e)
	}
	args, _ := json.Marshal(map[string]any{"path": "daemon.xlsx", "expected_sha256": outcome.SHA, "ops": []any{map[string]any{"action": "set_cell", "sheet": "Data", "cell": map[string]any{"ref": "A2", "type": "number", "value": 42}}}})
	result, e = svc.Exec.Execute(ctx, "document_edit", args, tools.ExecuteOptions{})
	if e != nil || result.Status != tools.StatusOK {
		t.Fatal(result, e)
	}
}
