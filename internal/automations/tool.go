package automations

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

const toolDescription = `Manage classic scheduled automations; no event triggers or webhooks. Actions: create, update, list, get, history, run_now, pause, resume, delete, restore_version. Before create or update, show the user the name, schedule in plain words and all steps, then wait for an explicit yes in the conversation. Only after that approval set approved:true. Never infer approval. Updates create a new version; restore_version restores an earlier version. If the user attaches a block starting with [Automatización adjunta or [Attached automation containing id and version, call get, apply the requested change with update after approval, then confirm. Schedule kinds: once (at RFC3339), interval (every_minutes >=5), daily (time HH:MM), weekly (time and weekdays 0=Sunday..6=Saturday), monthly (time and day_of_month 1..31 clamped to last day), cron (five fields); always supply an IANA timezone. Step types: agent (instruction), script (language python/node/bash and code), tool (registry tool and args), condition (expression with yes/no edges), notify (message). Steps share prior outputs with {{steps.<id>.output}}; when a step prints JSON, use {{steps.<id>.output.<field>}} in messages and steps.<id>.output.<field> in conditions (e.g. steps.gen.output.sum > 150). Conditions that reference missing fields fail the run. Scripts must read prior outputs from the AUTOMATION_INPUT environment variable (JSON object step id -> output text) instead of pasting {{steps...}} into code, because outputs can contain quotes; print the result to stdout. Scripts may use pip/npm only inside this automation's own work directory. Write titles and messages in the user's language with real line breaks. If an automation has no notify step, each successful run posts its summary to the chat automatically; if it has notify steps, only those messages are posted (failures are always reported), so a branch without notify stays silent. Example: daily at 09:00 UTC, agent s1 researches public web news, python script s2 compares {{steps.s1.output}} with saved results, condition s3 tests steps.s2.output != "" (only notify if something new), notify s4 sends {{steps.s2.output}}; edges s1->s2, s2->s3, s3->s4 labeled yes and s3->s5 labeled no, where s5 is a script that exits without notification.`

// ToolDefinition is the default/assistant catalog entry; eval is unchanged.
func ToolDefinition() tools.Definition {
	return tools.Definition{Name: "automations", Description: toolDescription, InputSchema: json.RawMessage(toolSchema), Effect: types.EffectWriteLocal, Idempotency: types.IdemNonIdempotent, Cancel: types.CancelCooperative, Timeout: time.Minute, OutputBudget: tools.DefaultOutputBudget, Fast: true, Profiles: []string{"default", "assistant"}, Group: "agenda", Keywords: []string{"automation", "schedule"}}
}

const toolSchema = `{"type":"object","required":["action"],"additionalProperties":false,"properties":{"action":{"type":"string","enum":["create","update","list","get","history","run_now","pause","resume","delete","restore_version"]},"id":{"type":"string","minLength":1},"approved":{"type":"boolean","description":"Set true only after the user explicitly approved the displayed schedule and steps."},"version":{"type":"integer","minimum":1},"note":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":100},"spec":{"type":"object","required":["name","description","schedule","steps","edges"],"additionalProperties":false,"properties":{"name":{"type":"string","minLength":1},"description":{"type":"string"},"schedule":{"type":"object","required":["kind","timezone"],"additionalProperties":false,"properties":{"kind":{"enum":["once","interval","daily","weekly","monthly","cron"]},"timezone":{"type":"string"},"at":{"type":"string","format":"date-time"},"every_minutes":{"type":"integer","minimum":5},"time":{"type":"string","pattern":"^([01][0-9]|2[0-3]):[0-5][0-9]$"},"weekdays":{"type":"array","items":{"type":"integer","minimum":0,"maximum":6}},"day_of_month":{"type":"integer","minimum":1,"maximum":31},"cron":{"type":"string"}}},"steps":{"type":"array","minItems":1,"items":{"type":"object","required":["id","type","title"],"additionalProperties":false,"properties":{"id":{"type":"string","minLength":1},"type":{"enum":["agent","script","tool","condition","notify"]},"title":{"type":"string"},"instruction":{"type":"string"},"language":{"enum":["python","node","bash"]},"code":{"type":"string"},"tool":{"type":"string"},"args":{"type":"object"},"expression":{"type":"string"},"message":{"type":"string"}}}},"edges":{"type":"array","items":{"type":"object","required":["from","to"],"additionalProperties":false,"properties":{"from":{"type":"string"},"to":{"type":"string"},"label":{"enum":["yes","no"]}}}}}}}}`

// WireTool registers the service-backed tool before intake begins.
func WireTool(reg *tools.MapRegistry, service Service) error {
	provider := &tools.StubTool{Def: ToolDefinition(), Provider: func(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
		out, err := toolAction(ctx, service, inv.Arguments)
		if err != nil {
			if ctx.Err() != nil {
				return tools.Result{Status: tools.StatusCanceled}, ctx.Err()
			}
			return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
		}
		raw, err := json.Marshal(out)
		return tools.Result{Status: tools.StatusOK, Output: string(raw)}, err
	}}
	if existing, err := reg.Get("automations"); err == nil {
		if stub, ok := existing.(*tools.StubTool); ok {
			stub.Provider = provider.Provider
			return nil
		}
		return fmt.Errorf("automations tool already registered")
	}
	return reg.Register(provider)
}

func toolAction(ctx context.Context, s Service, raw json.RawMessage) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var p struct {
		Action   string `json:"action"`
		ID       string `json:"id"`
		Approved bool   `json:"approved"`
		Version  int    `json:"version"`
		Note     string `json:"note"`
		Limit    int    `json:"limit"`
		Spec     *Spec  `json:"spec"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, fmt.Errorf("automations not available")
	}
	if p.Action != "list" && p.Action != "create" && p.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	switch p.Action {
	case "create", "update":
		if !p.Approved {
			return nil, fmt.Errorf("explicit conversation approval required: approved must be true")
		}
		if p.Spec == nil {
			return nil, fmt.Errorf("spec is required")
		}
		var a Automation
		var err error
		if p.Action == "create" {
			a, err = s.Create(ctx, *p.Spec)
		} else {
			a, err = s.Update(ctx, p.ID, *p.Spec, p.Note)
		}
		if err != nil {
			return nil, err
		}
		return savedResult(p.Action, a), nil
	case "list":
		a, e := s.List(ctx)
		if a == nil {
			a = []Automation{}
		}
		return map[string]any{"automations": a}, e
	case "get":
		a, v, e := s.Get(ctx, p.ID)
		if v == nil {
			v = []Version{}
		}
		return map[string]any{"automation": a, "versions": v}, e
	case "history":
		if p.Limit <= 0 {
			p.Limit = 20
		}
		if p.Limit > 100 {
			p.Limit = 100
		}
		r, e := s.Runs(ctx, p.ID, p.Limit)
		if r == nil {
			r = []Run{}
		}
		return map[string]any{"runs": r}, e
	case "run_now":
		r, e := s.RunNow(ctx, p.ID)
		return map[string]any{"run": r}, e
	case "pause":
		a, e := s.Pause(ctx, p.ID, "agent")
		if e != nil {
			return nil, e
		}
		return savedResult("pause", a), nil
	case "resume":
		a, e := s.Resume(ctx, p.ID)
		if e != nil {
			return nil, e
		}
		return savedResult("resume", a), nil
	case "delete":
		e := s.Delete(ctx, p.ID)
		return map[string]any{"id": p.ID, "deleted": true}, e
	case "restore_version":
		if p.Version < 1 {
			return nil, fmt.Errorf("version must be positive")
		}
		return s.RestoreVersion(ctx, p.ID, p.Version)
	default:
		return nil, fmt.Errorf("unknown automations action")
	}
}

// savedResult is the compact confirmation returned after a write. The full
// automation is deliberately omitted: models re-issued create calls when the
// answer was a large JSON document instead of an explicit confirmation.
func savedResult(action string, a Automation) map[string]any {
	done := map[string]string{"create": "created", "update": "updated", "pause": "paused", "resume": "resumed"}[action]
	out := map[string]any{
		"ok":      true,
		"result":  done,
		"id":      a.ID,
		"name":    a.Name,
		"version": a.Version,
		"status":  a.Status,
		"steps":   len(a.Steps),
		"note":    "Done. Do not repeat this call; tell the user it is " + done + ".",
	}
	if a.NextRunAt != nil {
		out["next_run_at"] = a.NextRunAt.Format(time.RFC3339)
	}
	return out
}
