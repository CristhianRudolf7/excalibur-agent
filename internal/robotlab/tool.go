package robotlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Tool is the deferred model surface. Nil collaborators return install guidance.
type Tool struct {
	Sidecar   Sidecar
	Publisher Publisher
	Vision    *VisionLoop
}
type toolAction struct{ method, fields, required, view string }

var toolActions = map[string]toolAction{
	"scenarios":          {method: "scenarios.list"},
	"open":               {"session.open", "scenario params seed", "scenario", "scene"},
	"control":            {"session.control", "session mode inputs pose", "session mode", "scene"},
	"reset":              {"session.reset", "session seed", "session", "scene"},
	"set_params":         {"session.set_params", "session params", "session params", "scene"},
	"snapshot":           {"session.snapshot", "session camera width height", "session", "scene"},
	"create_experiment":  {"experiment.create", "name scenario params runs total_steps reward_weights code", "name scenario runs", "experiment"},
	"write_code":         {"experiment.write_code", "experiment code", "experiment code", "experiment"},
	"read_code":          {"experiment.get", "experiment", "experiment", "experiment"},
	"start":              {"experiment.start", "experiment", "experiment", "experiment"},
	"stop":               {"experiment.stop", "experiment", "experiment", "experiment"},
	"status":             {"experiment.get", "experiment", "", ""},
	"metrics":            {"experiment.metrics", "experiment run since", "experiment", ""},
	"load_policy":        {"session.load_policy", "session experiment run checkpoint pretrained", "session", "scene"},
	"vision_step":        {"", "session instruction", "session instruction", "vision"},
	"set_teleop_mapping": {"teleop.set_mapping", "session code", "session", "teleop"},
}

func (*Tool) Definition() tools.Definition {
	return tools.Definition{Name: "robot_lab", Description: `Open Robot Lab, a programmable physics and reinforcement-learning lab shared with the user. Six scenarios: cartpole (1–3 links), ballplate, arm (reach/pick_place/sort), quadruped, drone, rocket. First list scenarios for parameters, reward terms and recommended algorithms. Open a session for live controls, reset, policy playback or webcam teleoperation. Create experiments with named algorithm runs, total_steps and reward_weights; all runs execute the same editable Python script with their own hyperparameters. SDK: from robotlab import sdk; ctx=sdk.context(); env=sdk.make_env(); model=sdk.algorithm(ctx.algorithm,env,**ctx.hyperparams); model.learn(ctx.total_steps,callback=sdk.callback()); sdk.save(model). Use write_code/read_code for the script, start/stop/status for lifecycle, metrics for per-run last/best return, steps and trend. Experiments persist across conversations and restarts and are not orchestrator tasks: whenever the user asks about training, results, a comparison or whether something finished, call status without an experiment to list them before answering. Training usually outlives one reply: after start, report progress and offer to check again. Use vision_step on the arm when an instruction requires seeing and sorting objects: see numbered marks, decide, perform pick_place and verify with a second image. Snapshot returns marks without labels; images stay in the UI. set_teleop_mapping accepts Python map_pose. Training start and physical actions must not be blindly retried. Installation requires explicit user action.`, InputSchema: toolSchema(), Effect: types.EffectExec, Idempotency: types.IdemNonIdempotent, Cancel: types.CancelCooperative, Timeout: 95 * time.Second, OutputBudget: 32000, Platforms: []string{"linux"}, Profiles: []string{"default", "delegate"}, Group: "science", Keywords: []string{"robot", "physics", "simulation", "reinforcement learning", "experiment", "teleop", "vision"}}
}
func toolSchema() json.RawMessage {
	props := map[string]any{}
	for _, n := range strings.Fields("action session experiment run scenario name camera instruction checkpoint mode") {
		props[n] = map[string]any{"type": "string", "minLength": 1}
	}
	props["action"] = map[string]any{"type": "string", "enum": []string{"scenarios", "open", "control", "reset", "set_params", "snapshot", "create_experiment", "write_code", "read_code", "start", "stop", "status", "metrics", "load_policy", "vision_step", "set_teleop_mapping"}}
	props["scenario"] = map[string]any{"type": "string", "enum": []string{"cartpole", "ballplate", "arm", "quadruped", "drone", "rocket"}}
	props["mode"] = map[string]any{"type": "string", "enum": []string{"idle", "manual", "policy", "pose", "paused"}}
	props["checkpoint"] = map[string]any{"type": "string", "enum": []string{"best", "last"}}
	for _, n := range strings.Fields("params inputs pose reward_weights") {
		props[n] = map[string]any{"type": "object"}
	}
	props["code"] = map[string]any{"type": []string{"string", "null"}, "maxLength": 24000}
	props["pretrained"] = map[string]any{"type": "boolean"}
	for _, n := range strings.Fields("seed since total_steps width height") {
		props[n] = map[string]any{"type": "integer"}
	}
	for _, n := range []string{"width", "height"} {
		props[n] = map[string]any{"type": "integer", "minimum": 1, "maximum": 1920}
	}
	props["since"] = map[string]any{"type": "integer", "minimum": 0}
	props["total_steps"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 100000000}
	props["runs"] = map[string]any{"type": "array", "minItems": 1, "maxItems": 16, "items": map[string]any{"type": "object", "required": []string{"label", "algorithm"}, "properties": map[string]any{"label": map[string]any{"type": "string", "minLength": 1}, "algorithm": map[string]any{"type": "string", "minLength": 1}, "hyperparams": map[string]any{"type": "object"}}, "additionalProperties": false}}
	raw, _ := json.Marshal(map[string]any{"type": "object", "required": []string{"action"}, "properties": props, "additionalProperties": false})
	return raw
}
func (*Tool) ResourceKeys(raw json.RawMessage) ([]string, error) {
	var a map[string]json.RawMessage
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	return []string{"robotlab"}, nil
}
func (t *Tool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	fail := func(s tools.ResultStatus, e error) (tools.Result, error) {
		return tools.Result{Status: s, Output: e.Error()}, nil
	}
	if len(inv.Arguments) > 64000 {
		return fail(tools.StatusInputError, fmt.Errorf("robotlab: input exceeds 64 KB"))
	}
	if err := tools.ValidateAgainstSchema(inv.Arguments, toolSchema()); err != nil {
		return fail(tools.StatusInputError, err)
	}
	var a map[string]json.RawMessage
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return fail(tools.StatusInputError, err)
	}
	var action string
	_ = json.Unmarshal(a["action"], &action)
	if raw, exists := a["code"]; exists && string(raw) != "null" {
		var code string
		if err := json.Unmarshal(raw, &code); err != nil || len(code) > 24000 {
			return fail(tools.StatusInputError, fmt.Errorf("code must be a string of at most 24000 bytes or null"))
		}
	}
	if raw, exists := a["inputs"]; exists {
		var inputs map[string]float64
		if err := json.Unmarshal(raw, &inputs); err != nil {
			return fail(tools.StatusInputError, fmt.Errorf("inputs must map actuators to numbers"))
		}
		for _, value := range inputs {
			if value < -1 || value > 1 {
				return fail(tools.StatusInputError, fmt.Errorf("inputs must be in [-1,1]"))
			}
		}
	}
	spec, ok := toolActions[action]
	if !ok {
		return fail(tools.StatusInputError, fmt.Errorf("unknown action"))
	}
	for key := range a {
		if key != "action" && !containsField(spec.fields, key) {
			return fail(tools.StatusInputError, fmt.Errorf("%s does not accept %s", action, key))
		}
	}
	for _, key := range strings.Fields(spec.required) {
		if len(a[key]) == 0 || string(a[key]) == "null" {
			return fail(tools.StatusInputError, fmt.Errorf("%s requires %s", action, key))
		}
	}
	if action == "write_code" && string(a["code"]) == "null" {
		return fail(tools.StatusInputError, fmt.Errorf("write_code requires a string"))
	}
	if action == "load_policy" && string(a["pretrained"]) != "true" && (len(a["experiment"]) == 0 || len(a["run"]) == 0) {
		return fail(tools.StatusInputError, fmt.Errorf("load_policy requires pretrained:true or experiment and run"))
	}
	ctx, cancel := context.WithTimeout(ctx, 95*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		status := tools.StatusCanceled
		if errors.Is(err, context.DeadlineExceeded) {
			status = tools.StatusTimeout
		}
		return fail(status, err)
	}
	if !Enabled() || t.Sidecar == nil {
		return unavailableResult()
	}
	status := t.Sidecar.Status(ctx)
	if !status.Enabled || !status.Installed {
		return unavailableResult()
	}
	delete(a, "action")
	str := func(key string) string { var s string; _ = json.Unmarshal(a[key], &s); return s }
	var out any
	var err error
	if action == "status" && str("experiment") == "" {
		// Runtime state plus every stored experiment, so questions such as
		// "did it finish?" are answerable after restarts and in new replies.
		var listed struct {
			Experiments []json.RawMessage `json:"experiments"`
		}
		raw, listErr := t.Sidecar.Call(ctx, "experiment.list", map[string]any{})
		if listErr == nil {
			listErr = json.Unmarshal(raw, &listed)
		}
		summary := map[string]any{"runtime": status}
		if listErr != nil {
			summary["experiments_error"] = listErr.Error()
		} else {
			exps := make([]any, 0, len(listed.Experiments))
			for _, e := range listed.Experiments {
				var v any
				if json.Unmarshal(e, &v) == nil {
					exps = append(exps, compactToolOutput("status", v))
				}
			}
			summary["experiments"] = exps
		}
		out = summary
	} else if action == "vision_step" {
		loop := t.Vision
		if loop == nil {
			loop = &VisionLoop{Sidecar: t.Sidecar, Publisher: t.Publisher}
		}
		out, err = loop.VisionStep(ctx, str("session"), str("instruction"))
	} else {
		if action == "snapshot" {
			a["marks"] = json.RawMessage(`true`)
		}
		var raw json.RawMessage
		raw, err = t.Sidecar.Call(ctx, spec.method, a)
		// A stale installed status can precede a failed lazy start or crash.
		// Preserve cancellation/timeouts, but give stopped-runtime failures
		// the same installation guidance as an absent sidecar.
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			state := t.Sidecar.Status(ctx)
			if !state.Enabled || !state.Installed || !state.Running {
				return unavailableResult()
			}
		}
		if err == nil {
			err = json.Unmarshal(raw, &out)
		}
		if err == nil {
			out = compactToolOutput(action, out)
		}
	}
	if err != nil {
		s := tools.StatusToolError
		if errors.Is(err, context.DeadlineExceeded) {
			s = tools.StatusTimeout
		} else if errors.Is(err, context.Canceled) {
			s = tools.StatusCanceled
		}
		return fail(s, err)
	}
	if spec.view != "" && t.Publisher != nil {
		focus := map[string]any{"view": spec.view}
		for _, key := range []string{"session", "experiment"} {
			if s := str(key); s != "" {
				focus[key] = s
			}
		}
		if m, ok := out.(map[string]any); ok {
			for _, key := range []string{"session", "id"} {
				if v, ok := m[key]; ok {
					target := key
					if key == "id" {
						target = "experiment"
					}
					focus[target] = v
				}
			}
		}
		t.Publisher.Publish("robotlab.focus", focus)
	}
	raw, err := json.Marshal(out)
	return tools.Result{Status: tools.StatusOK, Output: string(raw)}, err
}
func containsField(fields, key string) bool {
	for _, f := range strings.Fields(fields) {
		if f == key {
			return true
		}
	}
	return false
}
func unavailableResult() (tools.Result, error) {
	return tools.Result{Status: tools.StatusToolError, Output: `{"status":"unavailable","detail":"Robot Lab is unavailable. Enable EXCALIBUR_ROBOTLAB and use the Robot Lab Install card to install Python dependencies and OSMesa; installation requires an explicit user action."}`}, nil
}
func compactToolOutput(action string, out any) any {
	m, ok := out.(map[string]any)
	if !ok {
		return out
	}
	if action == "metrics" {
		return summarizeMetrics(m)
	}
	if action == "read_code" {
		return map[string]any{"experiment": m["id"], "code": m["code"]}
	}
	if action == "snapshot" {
		raw, _ := json.Marshal(m)
		var s visionSnapshot
		_ = json.Unmarshal(raw, &s)
		return map[string]any{"width": m["width"], "height": m["height"], "marks": s.Marks}
	}
	delete(m, "scene")
	delete(m, "code")
	delete(m, "image_png_b64")
	delete(m, "frames")
	return m
}
func summarizeMetrics(m map[string]any) any {
	type summary struct {
		Run   string   `json:"run"`
		Steps float64  `json:"steps"`
		Last  *float64 `json:"last_return,omitempty"`
		Best  *float64 `json:"best_return,omitempty"`
		Trend string   `json:"trend"`
		first *float64
	}
	runs := map[string]*summary{}
	order := []string{}
	points, _ := m["points"].([]any)
	for _, p := range points {
		v, ok := p.(map[string]any)
		if !ok {
			continue
		}
		id, _ := v["run"].(string)
		r := runs[id]
		if r == nil {
			r = &summary{Run: id, Trend: "unknown"}
			runs[id] = r
			order = append(order, id)
		}
		step, _ := v["step"].(float64)
		if step > r.Steps {
			r.Steps = step
		}
		if val, ok := v["episode_return"].(float64); ok {
			v := val
			if r.first == nil {
				r.first = &v
			}
			r.Last = &v
			if r.Best == nil || v > *r.Best {
				r.Best = &v
			}
		}
	}
	result := []*summary{}
	for _, id := range order {
		r := runs[id]
		if r.Last != nil {
			r.Trend = "flat"
			if *r.Last > *r.first {
				r.Trend = "improving"
			} else if *r.Last < *r.first {
				r.Trend = "declining"
			}
		}
		result = append(result, r)
	}
	return map[string]any{"runs": result, "next": m["next"]}
}
