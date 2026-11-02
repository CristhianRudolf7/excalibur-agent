package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/automations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

type automationFake struct {
	automations.Service
	id, reason string
	limit      int
	err        error
	listener   func(automations.Event)
}

func (f *automationFake) List(context.Context) ([]automations.Automation, error) {
	return []automations.Automation{{Name: "Z"}, {Name: "A"}}, f.err
}
func (f *automationFake) Get(_ context.Context, id string) (automations.Automation, []automations.Version, error) {
	f.id = id
	return automations.Automation{ID: id}, nil, f.err
}
func (f *automationFake) Runs(_ context.Context, id string, n int) ([]automations.Run, error) {
	f.id = id
	f.limit = n
	return nil, f.err
}
func (f *automationFake) RunNow(_ context.Context, id string) (automations.Run, error) {
	f.id = id
	return automations.Run{Status: automations.RunRunning}, f.err
}
func (f *automationFake) Pause(_ context.Context, id, r string) (automations.Automation, error) {
	f.id = id
	f.reason = r
	return automations.Automation{PausedReason: r}, f.err
}
func (f *automationFake) Resume(_ context.Context, id string) (automations.Automation, error) {
	f.id = id
	return automations.Automation{}, f.err
}
func (f *automationFake) Delete(_ context.Context, id string) error { f.id = id; return f.err }
func (f *automationFake) Subscribe(fn func(automations.Event)) func() {
	f.listener = fn
	return func() { f.listener = nil }
}
func TestAutomationRoutes(t *testing.T) {
	f := &automationFake{}
	b := &DaemonBackend{}
	b.AttachServices(BackendServices{Automations: f})
	for _, m := range []string{api.MethodAutomationsList, api.MethodAutomationsGet, api.MethodAutomationsRuns, api.MethodAutomationsRun, api.MethodAutomationsPause, api.MethodAutomationsResume, api.MethodAutomationsDelete} {
		out, e := b.Passthrough(context.Background(), m, json.RawMessage(`{"id":"a"}`))
		if e != nil {
			t.Fatal(m, e)
		}
		raw, _ := json.Marshal(out)
		if !json.Valid(raw) {
			t.Fatal(out)
		}
	}
	if f.id != "a" || f.reason != "user" || f.limit != 20 {
		t.Fatal(f)
	}
	out, _ := automationOp(context.Background(), f, api.MethodAutomationsList, nil)
	if out.(AutomationsListResult).Automations[0].Name != "A" {
		t.Fatal(out)
	}
	_, _ = automationOp(context.Background(), f, api.MethodAutomationsRuns, json.RawMessage(`{"id":"a","limit":999}`))
	if f.limit != 100 {
		t.Fatal(f.limit)
	}
	for _, err := range []error{automations.ErrNotFound, automations.ErrAlreadyRunning} {
		f.err = err
		_, e := automationOp(context.Background(), f, api.MethodAutomationsRun, json.RawMessage(`{"id":"a"}`))
		var rpc *api.Error
		if !errors.As(e, &rpc) || !strings.Contains(rpc.Message, err.Error()) {
			t.Fatal(e)
		}
	}
	for _, raw := range []string{`null`, `[]`, `{}`, `{"id":7}`} {
		if _, e := automationOp(context.Background(), f, api.MethodAutomationsGet, json.RawMessage(raw)); e == nil {
			t.Fatal(raw)
		}
	}
	if _, e := automationOp(context.Background(), nil, api.MethodAutomationsList, nil); e == nil || !strings.Contains(e.Error(), "not available") {
		t.Fatal(e)
	}
}
func TestAutomationEventBridge(t *testing.T) {
	f := &automationFake{}
	bus := telemetry.NewBus(nil)
	stop := WireAutomationEvents(f, bus)
	f.listener(automations.Event{Kind: automations.EventUpdated, Automation: &automations.Automation{ID: "a"}})
	f.listener(automations.Event{Kind: automations.EventDeleted, ID: "a"})
	f.listener(automations.Event{Kind: automations.EventRun, Run: &automations.Run{ID: "r"}})
	events := bus.Replay(types.Cursor{})
	if len(events) != 3 {
		t.Fatal(events)
	}
	for i, want := range []string{api.EventAutomationUpdated, api.EventAutomationDeleted, api.EventAutomationRun} {
		if string(events[i].Type) != want {
			t.Fatal(events[i])
		}
	}
	stop()
	if f.listener != nil {
		t.Fatal("subscription leaked")
	}
}

type automationLifecycleFake struct {
	automationFake
	started, stopped bool
}

func (f *automationLifecycleFake) Start(context.Context) { f.started = true }
func (f *automationLifecycleFake) Stop()                 { f.stopped = true }
func TestAutomationLifecycle(t *testing.T) {
	f := &automationLifecycleFake{}
	d := New(DefaultConfig(), Deps{Services: &Services{Automations: f}})
	if _, e := d.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	if !f.started {
		t.Fatal("not started")
	}
	if e := d.Stop(context.Background()); e != nil {
		t.Fatal(e)
	}
	if !f.stopped {
		t.Fatal("not stopped")
	}
}

func TestAutomationGatewayRPC(t *testing.T) {
	f := &automationFake{}
	b := NewDaemonBackend(nil, nil, nil)
	b.AttachServices(BackendServices{Automations: f})
	cfg := gateway.DefaultConfig()
	cfg.AllowRemote = true
	g := gateway.New(cfg, b, nil, telemetry.NewBus(nil))
	for _, method := range []string{api.MethodAutomationsList, api.MethodAutomationsGet, api.MethodAutomationsRuns, api.MethodAutomationsRun, api.MethodAutomationsPause, api.MethodAutomationsResume, api.MethodAutomationsDelete} {
		var result json.RawMessage
		if e := audioRPC(t, g, method, map[string]any{"id": "a"}, &result); e != nil {
			t.Fatal(method, e)
		}
	}
	f.err = automations.ErrAlreadyRunning
	var result json.RawMessage
	if e := audioRPC(t, g, api.MethodAutomationsRun, map[string]any{"id": "a"}, &result); e == nil || !strings.Contains(e.Error(), "already running") {
		t.Fatal(e)
	}
	f.err = automations.ErrNotFound
	if e := audioRPC(t, g, api.MethodAutomationsGet, map[string]any{"id": "a"}, &result); e == nil || !strings.Contains(e.Error(), "not found") {
		t.Fatal(e)
	}
}
