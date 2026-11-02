package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	orchfake "github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestActivityStepDescriptions(t *testing.T) {
	for _, tt := range []struct{ tool, args, en, es string }{
		{"write_file", `{"path":"app.py","content":"secret"}`, "Editing app.py", "Editando app.py"},
		{"edit_file", `{"path":"src/app.py"}`, "Editing src/app.py", "Editando src/app.py"},
		{"apply_patch", `{"ops":[{"path":"app.py"}]}`, "Editing app.py", "Editando app.py"},
		{"read_file", `{"path":"app.py"}`, "Reading app.py", "Leyendo app.py"},
		{"exec", `{"command":"python3 app.py"}`, "Running python3 app.py", "Ejecutando python3 app.py"},
		{"exec_io", `{}`, "Running…", "Ejecutando…"},
		{"source_search", `{}`, "Browsing the web", "Consultando la web"},
		{"grep", `{"pattern":"TODO"}`, "Searching TODO", "Buscando TODO"},
		{"search", `{"query":"functions"}`, "Searching functions", "Buscando functions"},
		{"web_search", `{}`, "Browsing the web", "Consultando la web"},
		{"", ``, "Thinking…", "Pensando…"},
		{"read_file", `invalid`, "Reading…", "Leyendo…"},
		{"call_tool", `{"name":"read_file","arguments":{"path":"app.py"}}`, "Reading app.py", "Leyendo app.py"},
	} {
		t.Run(tt.tool+tt.args, func(t *testing.T) {
			for _, spanish := range []bool{false, true} {
				want := tt.en
				if spanish {
					want = tt.es
				}
				if got := activityStep("running", "", tt.tool, tt.args, spanish); got != want {
					t.Fatalf("got %q, want %q", got, want)
				}
			}
		})
	}
	args, _ := json.Marshal(map[string]string{"command": strings.Repeat("ñ", 70)})
	if got := activityStep("running", "", "exec", string(args), true); got != "Ejecutando "+strings.Repeat("ñ", 60)+"…" {
		t.Fatal(got)
	}
	for _, tt := range []struct{ state, failure, en, es string }{
		{"completed", "result text", "Completed", "Completado"},
		{"failed", "disk full", "disk full", "disk full"},
		{"failed", "", "Failed", "Error"},
		{"cancelled", "", "Cancelled", "Cancelado"},
	} {
		if got := activityStep(tt.state, tt.failure, "exec", `{}`, false); got != tt.en {
			t.Fatal(got)
		}
		if got := activityStep(tt.state, tt.failure, "exec", `{}`, true); got != tt.es {
			t.Fatal(got)
		}
	}
}

func TestActivityUsesLiveArgumentsAndConversationLanguage(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	store := orchestrator.NewMemoryStore()
	if err := store.SaveAgent(types.Agent{ID: "worker"}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateRun(orchestrator.RunRecord{Run: types.Run{ID: "run", AgentID: "worker", State: types.RunRunning, CreatedAt: now, UpdatedAt: now}}); err != nil {
		t.Fatal(err)
	}
	orch := orchestrator.New(orchestrator.Config{}, store, nil)
	runner := orchfake.NewRunner("run", "worker")
	// The path follows content larger than status's truncated argument summary.
	runner.SetTool("write_file", `{"content":"`+strings.Repeat("x", 200)+`","path":"app.py"}`, types.CancelImmediate, nil, time.Minute, now)
	orch.AttachRunner(runner)
	b := NewDaemonBackend(master.New(master.Config{}, nil, nil, nil, nil, nil, nil), &OrchControl{Store: store, Orch: orch}, telemetry.NewBus(nil))
	if _, _, err := b.ConversationSend(ctx, "test", "first", "conv", "Edita app.py en el proyecto de prueba"); err != nil {
		t.Fatal(err)
	}
	b.trackActivity("conv", "run", "run")
	items, err := b.activityItems(ctx, "conv", now)
	if err != nil || len(items) != 1 || items[0].Step != "Editando app.py" {
		t.Fatalf("%+v %v", items, err)
	}
	runner.ClearTool()
	items, err = b.activityItems(ctx, "conv", now)
	if err != nil || items[0].Step != "Pensando…" {
		t.Fatalf("%+v %v", items, err)
	}
	rec, _ := store.GetRun("run")
	rec.Run.State = types.RunCompleted
	rec.Run.LastProgress = "An English summary"
	if err := store.UpdateRun(rec); err != nil {
		t.Fatal(err)
	}
	items, err = b.activityItems(ctx, "conv", now)
	if err != nil || items[0].Step != "Completado" {
		t.Fatalf("%+v %v", items, err)
	}
}
