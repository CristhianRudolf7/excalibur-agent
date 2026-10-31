package cli

import (
	"context"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
)

// TestDaemonFirstTaskFlow spins a real daemon in-process and verifies
// the Section 3.6 routing: with the rendezvous present every task
// wrapper goes through the gate; without it they run embedded.
func TestDaemonFirstTaskFlow(t *testing.T) {
	if _, ok := useDaemon(); ok {
		t.Skip("a daemon is already running in this environment")
	}
	ctx := context.Background()
	bus := telemetry.NewBus(nil)
	orchStore := orchestrator.NewMemoryStore()
	orch := orchestrator.New(orchestrator.DefaultConfig(), orchStore, orchestrator.NoopJournal{})
	ctrls := &daemon.OrchControl{Orch: orch, Store: orchStore}
	m := master.New(master.Config{}, master.NewMemStore(), master.NewLexicon(), nil, ctrls, nil, nil)
	backend := daemon.NewDaemonBackend(m, ctrls, bus)
	tasks, err := daemon.NewTaskService(orch, orchStore)
	if err != nil {
		t.Fatal(err)
	}
	backend.AttachServices(daemon.BackendServices{Tasks: tasks})
	g := gateway.New(gateway.DefaultConfig(), backend, gateway.NewMemoryBlobs(), bus)
	rt := t.TempDir()
	d := daemon.New(daemon.Config{Listen: "127.0.0.1:0", RuntimeDir: rt}, daemon.Deps{Gate: g, Master: m, Orch: orch, Bus: bus, Backend: backend})
	addr, err := d.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()
	t.Setenv("EXCALIBUR_DAEMON_ADDR", addr)

	start, err := Start(StartOptions{Request: "write the tests", Profile: "delegate"})
	if err != nil {
		t.Fatalf("start through daemon: %v", err)
	}
	if start.TaskID == "" {
		t.Fatal("empty task id")
	}
	// The embedded store knows nothing about daemon tasks: the gate
	// owns them.
	if _, err := ResolveStore(""); err == nil {
		if store, serr := ResolveStore(""); serr == nil {
			if _, lerr := store.Load(string(start.TaskID)); lerr == nil {
				t.Fatal("daemon task leaked into the embedded store")
			}
		}
	}
	st, err := Status("", string(start.TaskID))
	if err != nil {
		t.Fatalf("status through daemon: %v", err)
	}
	if st.TaskID != start.TaskID {
		t.Fatalf("wrong task: %v", st.TaskID)
	}
	waited, err := Wait("", string(start.TaskID), 2)
	if err != nil {
		t.Fatalf("wait through daemon: %v", err)
	}
	if waited.TaskID != start.TaskID {
		t.Fatalf("wait returned wrong task: %v", waited.TaskID)
	}
	res, err := Result("", string(start.TaskID))
	if err != nil {
		t.Fatalf("result through daemon: %v", err)
	}
	if res.TaskID != start.TaskID {
		t.Fatalf("result for wrong task: %v", res.TaskID)
	}
	items, err := ListTasks("")
	if err != nil {
		t.Fatalf("list through daemon: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("daemon task list is empty")
	}
	logs, err := TaskLogs("", string(start.TaskID))
	if err != nil {
		t.Fatalf("logs through daemon: %v", err)
	}
	if len(logs) == 0 {
		t.Fatal("daemon task has no log lines")
	}
	replied, err := Reply("", string(start.TaskID), "use table tests")
	if err != nil {
		t.Fatalf("reply through daemon: %v", err)
	}
	if replied.TaskID != start.TaskID {
		t.Fatalf("reply for wrong task: %v", replied.TaskID)
	}
	canceled, err := Cancel("", string(start.TaskID))
	if err != nil {
		t.Fatalf("cancel through daemon: %v", err)
	}
	if canceled.TaskID != start.TaskID {
		t.Fatalf("cancel for wrong task: %v", canceled.TaskID)
	}
}
