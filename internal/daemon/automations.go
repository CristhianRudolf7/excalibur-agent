package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/automations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
)

func automationOp(ctx context.Context, s automations.Service, method string, raw json.RawMessage) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, &api.Error{Code: api.CodeInternal, Message: "automations not available"}
	}
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var p api.AutomationsParams
	if strings.TrimSpace(string(raw)) == "null" {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: "params must be an object"}
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: err.Error()}
	}
	if method != api.MethodAutomationsList && strings.TrimSpace(p.ID) == "" {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: "id is required"}
	}
	out, err := automationCall(ctx, s, method, p)
	if err != nil {
		var rpc *api.Error
		if errors.As(err, &rpc) {
			return nil, rpc
		}
		code := api.CodeInternal
		if errors.Is(err, automations.ErrNotFound) {
			code = api.CodeUnknownTarget
		}
		return nil, &api.Error{Code: code, Message: err.Error()}
	}
	return out, nil
}
func automationCall(ctx context.Context, s automations.Service, m string, p api.AutomationsParams) (any, error) {
	switch m {
	case api.MethodAutomationsList:
		a, e := s.List(ctx)
		if a == nil {
			a = []automations.Automation{}
		}
		sort.SliceStable(a, func(i, j int) bool { return a[i].Name < a[j].Name })
		return AutomationsListResult{Automations: a}, e
	case api.MethodAutomationsGet:
		a, v, e := s.Get(ctx, p.ID)
		if v == nil {
			v = []automations.Version{}
		}
		return AutomationsGetResult{Automation: a, Versions: v}, e
	case api.MethodAutomationsRuns:
		if p.Limit <= 0 {
			p.Limit = 20
		}
		if p.Limit > 100 {
			p.Limit = 100
		}
		r, e := s.Runs(ctx, p.ID, p.Limit)
		if r == nil {
			r = []automations.Run{}
		}
		return AutomationsRunsResult{Runs: r}, e
	case api.MethodAutomationsRun:
		r, e := s.RunNow(ctx, p.ID)
		return AutomationRunEvent{Run: r}, e
	case api.MethodAutomationsPause:
		return s.Pause(ctx, p.ID, "user")
	case api.MethodAutomationsResume:
		return s.Resume(ctx, p.ID)
	case api.MethodAutomationsDelete:
		e := s.Delete(ctx, p.ID)
		return map[string]any{"id": p.ID, "deleted": true}, e
	default:
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown automations method"}
	}
}

// WireAutomationEvents uses the normal event bus and websocket replay path.
func WireAutomationEvents(s automations.Service, bus *telemetry.Bus) func() {
	if s == nil || bus == nil {
		return func() {}
	}
	return s.Subscribe(func(e automations.Event) {
		switch e.Kind {
		case automations.EventUpdated:
			if e.Automation != nil {
				_, _ = bus.Publish(api.EventAutomationUpdated, nil, nil, AutomationEvent{Automation: *e.Automation})
			}
		case automations.EventDeleted:
			_, _ = bus.Publish(api.EventAutomationDeleted, nil, nil, api.AutomationDeletedEvent{ID: e.ID})
		case automations.EventRun:
			if e.Run != nil {
				_, _ = bus.Publish(api.EventAutomationRun, nil, nil, AutomationRunEvent{Run: *e.Run})
			}
		}
	})
}

// AutomationLifecycle is the manager boundary used by daemon startup/shutdown.
// The factory adapts manager construction and lifecycle signatures at merge.
type AutomationLifecycle interface {
	automations.Service
	Start(context.Context)
	Stop()
}

// Wire result and event payloads (kept here to avoid an api -> automations -> agenda -> api cycle).
type AutomationsListResult struct {
	Automations []automations.Automation `json:"automations"`
}
type AutomationsGetResult struct {
	Automation automations.Automation `json:"automation"`
	Versions   []automations.Version  `json:"versions"`
}
type AutomationsRunsResult struct {
	Runs []automations.Run `json:"runs"`
}
type AutomationEvent struct {
	Automation automations.Automation `json:"automation"`
}
type AutomationRunEvent struct {
	Run automations.Run `json:"run"`
}
