package robotlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/whiteboard"
	"github.com/google/jsonschema-go/jsonschema"
)

// Tool exposes low-level training actions. Vision is retained as an inert
// compatibility field; observations travel in Results.
type Tool struct {
	Sidecar   Sidecar
	Publisher Publisher
	Vision    *VisionLoop
}

type toolAction struct {
	method string
	omit   []string
}

var toolActions = map[string]toolAction{
	"robots": {method: "robots.list"}, "environments": {method: "env.list"},
	"environment_get": {method: "env.get"}, "environment_create": {method: "env.save", omit: []string{"create"}}, "environment_update": {method: "env.save", omit: []string{"create"}},
	"environment_delete": {method: "env.delete"}, "environment_preview": {method: "env.preview"},
	"sessions": {method: "session.list"}, "session_resume": {method: "session.resume", omit: []string{"conversation"}},
	"session_start": {method: "session.start", omit: []string{"conversation"}}, "session_status": {method: "session.get"}, "session_stop": {method: "session.stop", omit: []string{"by"}},
	"attempt_reset": {method: "attempt.reset"}, "act": {method: "robot.act"}, "look": {method: "robot.look"},
	"skills": {method: "skills.list"}, "skill_read": {method: "skills.read"}, "skill_write": {method: "skills.write"}, "skill_use": {method: "skills.use"},
	"skills_copy": {method: "skills.copy"}, "note": {method: "log.note"},
}

func (*Tool) Definition() tools.Definition {
	return tools.Definition{Name: "robot_lab", Description: `Train simulated robots with low-level joint, end-point and keyframe tools. Robots: arm (UR10e + 2F85, table) and dog (Go2, room). Physics advances only during motion/wait; between calls motors hold their last targets. Angles are degrees, lengths meters, time simulation seconds.
Retry/repeat a previous simulation (including "vuelve a intentar"): call sessions with robot/environment/query filters, choose the newest matching session, then session_resume(session), even if succeeded; never session_start for a retry. Resume automatically stops another live session, reports stopped_session and focuses the requested session; if already live, returns it. Read skills with skill_use and continue.
Training loop: (1) session_start for new training, read the robot card (joint names, limits, link lengths, reference pose, sensors, cameras and notes), list skills (skills), and skill_use relevant markdown instructions. (2) note(kind=plan): plan small keyframes; check end-point reachability and joint-limit margins with act(tool=ik) before moving. (3) act(tool=sequence): prefer 4–12 keyframes per call over single moves (1–16 allowed, total duration at most 30 s). Each keyframe has its own duration; targets interpolate smoothly, then settle (default 0.2 s, range 0–2). Inspect the returned state, contacts, goal and attempt after every call; for the dog read support, margin, margin_dir_deg and terrain_ahead. Use look to observe; act(tool=look) is an alias. (4) After EVERY failed or abandoned attempt, reflect (note kind=reflection), then skill_write or improve a markdown skill with the lesson AND the exact keyframes that worked so far, failures:[N] (attempt number). (5) Only then attempt_reset(reflection); if no useful skill can be written, give no_skill_reason (max 2000 characters). Repeat until goal truth or the attempt limit; default max_attempts is 40. (6) Summarize created/improved skills and success rates (skills). Never claim success unless goal.met=true or the session succeeded.
Dog tools: move_joints, move_feet, sequence, ik, wait. Keep three feet planted and the COM inside the support triangle before lifting a foot. Move the trunk while unlisted feet retain their world targets. Arm tools: move_joints, move_gripper_to, gripper, sequence, ik, wait. Read the card for tool schemas and stop_if conditions. Rejected targets cause no motion. Grasp assist requires both finger pads touching the same dynamic object; opening beyond its width releases it. Skills are session-local SKILL.md instructions with versions and failure evidence; every write is accepted. Reuse via copy_skills_from on session_start or skills_copy(from,to); skill_use reads instructions and marks use.
Create/edit environments ONLY from chat text with environment_create/environment_update: version 1, unique slug id, name, kind room|table, objects (box/sphere/cylinder/capsule), source:{type:"text",notes?}. Sizes are full dimensions, positions centers, yaw degrees; omitted z rests on floor/table. Use realistic dimensions and masses, ask about ambiguous measurements, then environment_preview. Built-ins: escalera-estrecha (dog; robot_on target descanso, feet:4), estante-pequeno (arm; object_on caja target estante). object_on and robot_on take stable_s (default 1 s); inspect their met result. look and environment_preview return images. session_stop ends training; respect user stop. Do not blindly retry act or session mutations after uncertain results; inspect status first. Installation is explicit.`, InputSchema: toolSchema(), Effect: types.EffectExec, Idempotency: types.IdemNonIdempotent, Cancel: types.CancelCooperative, Timeout: 95 * time.Second, OutputBudget: 32000, Platforms: []string{"linux"}, Profiles: []string{"default", "delegate"}, Group: "science", Keywords: []string{"robot", "physics", "simulation", "training", "environment", "skills", "arm", "dog", "simulación", "simulacion", "entrenamiento", "laboratorio", "brazo", "perro"}}
}

var toolSchemaOnce sync.Once
var toolSchemaRaw json.RawMessage
var toolValidator *jsonschema.Resolved
var toolSchemaErr error

func toolSchema() json.RawMessage {
	toolSchemaOnce.Do(func() {
		schemaOnce.Do(loadSchemas)
		if schemaErr != nil {
			toolSchemaErr = schemaErr
			return
		}
		actions := make([]string, 0, len(toolActions))
		for action := range toolActions {
			actions = append(actions, action)
		}
		sort.Strings(actions)
		properties := map[string]any{"action": map[string]any{"type": "string", "enum": actions}}
		branches := []any{}
		for _, action := range actions {
			spec := toolActions[action]
			params := contract.Methods["robotlab."+spec.method].Params
			fields := map[string]any{"action": map[string]any{"const": action}}
			omitted := map[string]bool{}
			for _, key := range spec.omit {
				omitted[key] = true
			}
			for key, value := range params["properties"].(map[string]any) {
				if !omitted[key] {
					fields[key] = value
					properties[key] = map[string]any{}
				}
			}
			required := []string{"action"}
			for _, value := range params["required"].([]any) {
				key := value.(string)
				if !omitted[key] {
					required = append(required, key)
				}
			}
			branches = append(branches, map[string]any{"type": "object", "properties": fields, "required": required, "additionalProperties": false})
		}
		schema := map[string]any{"type": "object", "properties": properties, "required": []string{"action"}, "additionalProperties": false, "oneOf": branches, "$defs": inputDefinitions(branches)}
		toolSchemaRaw, toolSchemaErr = json.Marshal(schema)
		if toolSchemaErr != nil {
			return
		}
		var parsed jsonschema.Schema
		toolSchemaErr = json.Unmarshal(toolSchemaRaw, &parsed)
		if toolSchemaErr == nil {
			toolValidator, toolSchemaErr = parsed.Resolve(nil)
		}
	})
	return toolSchemaRaw
}

// inputDefinitions avoids sending unrelated replay/result shapes in every turn.
func inputDefinitions(root any) map[string]any {
	defs := map[string]any{}
	var visit func(any)
	visit = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if reference, ok := node["$ref"].(string); ok {
				name := strings.TrimPrefix(reference, "#/$defs/")
				if _, seen := defs[name]; !seen {
					defs[name] = contract.Defs[name]
					visit(defs[name])
				}
			}
			for _, child := range node {
				visit(child)
			}
		case []any:
			for _, child := range node {
				visit(child)
			}
		}
	}
	visit(root)
	return defs
}

func (*Tool) ResourceKeys(raw json.RawMessage) ([]string, error) {
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	return []string{"robotlab"}, nil
}
func (t *Tool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	fail := func(status tools.ResultStatus, err error) (tools.Result, error) {
		return tools.Result{Status: status, Output: err.Error()}, nil
	}
	if len(inv.Arguments) > 8<<20 {
		return fail(tools.StatusInputError, fmt.Errorf("robotlab: input exceeds 8 MiB"))
	}
	toolSchema()
	if toolSchemaErr != nil {
		return fail(tools.StatusToolError, toolSchemaErr)
	}
	var value any
	if err := json.Unmarshal(inv.Arguments, &value); err != nil {
		return fail(tools.StatusInputError, err)
	}
	if err := toolValidator.Validate(value); err != nil {
		return fail(tools.StatusInputError, err)
	}
	args := value.(map[string]any)
	action := args["action"].(string)
	delete(args, "action")
	// look is a core observation, not a robot tool; accept act(tool=look) as an alias.
	if action == "act" && args["tool"] == "look" {
		action = "look"
		lookArgs := map[string]any{"session": args["session"]}
		if extra, ok := args["args"].(map[string]any); ok {
			for _, key := range []string{"camera", "width", "height"} {
				if v, ok := extra[key]; ok {
					lookArgs[key] = v
				}
			}
		}
		args = lookArgs
	}
	spec := toolActions[action]
	switch action {
	case "environment_create":
		args["create"] = true
	case "environment_update":
		args["create"] = false
	case "session_start", "session_resume":
		if conversation := whiteboard.Conversation(ctx); conversation != "" {
			args["conversation"] = conversation
		}
	case "session_stop":
		args["by"] = "agent"
	}
	raw, _ := json.Marshal(args)
	params, err := decodeParams("robotlab."+spec.method, raw)
	if err != nil {
		return fail(tools.StatusInputError, err)
	}
	ctx, cancel := context.WithTimeout(ctx, 95*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return fail(toolErrorStatus(err), err)
	}
	if !Enabled() || t.Sidecar == nil {
		return unavailableResult()
	}
	status := t.Sidecar.Status(ctx)
	if !status.Enabled || !status.Installed {
		return unavailableResult()
	}
	// Only a confirmed live-session conflict permits one resume retry.
	result, err := t.Sidecar.Call(ctx, spec.method, params)
	if action == "session_resume" && err != nil {
		result, err = t.resumeConflict(ctx, params, err)
	}
	if err != nil {
		return fail(toolErrorStatus(err), err)
	}
	var out map[string]any
	if err = json.Unmarshal(result, &out); err != nil {
		return fail(tools.StatusToolError, err)
	}
	if (action == "session_start" || action == "session_resume") && t.Publisher != nil {
		if session, ok := out["session"].(map[string]any); ok {
			if id, ok := session["id"].(string); ok && id != "" {
				t.Publisher.Publish("robotlab.focus", map[string]any{"session": id, "robot": session["robot"], "environment": session["environment"]})
			}
		}
	}
	if action == "sessions" {
		// A retry request means a new attempt in the matching saved session, whatever its
		// outcome; say so next to the data the agent decides from.
		out["next"] = "To retry or repeat one of these sessions (for example after \"vuelve a intentar\"), call session_resume with its id: it works for succeeded, stopped and exhausted sessions, starts a new attempt with the saved skills and opens the lab. Do not answer that the session is already finished."
	}
	images := []providers.ImageInput{}
	if action == "look" || action == "environment_preview" {
		if png, ok := out["png"].(string); ok && png != "" {
			images = append(images, providers.ImageInput{URL: "data:image/png;base64," + png})
		}
		delete(out, "png")
	}
	if session, ok := out["session"].(map[string]any); ok {
		// Full histories belong in replay/log RPCs; keep turn text focused on progress.
		delete(session, "attempts")
		delete(session, "skill_events")
	}
	raw, err = json.Marshal(out)
	return tools.Result{Status: tools.StatusOK, Output: string(raw), Images: images}, err
}

func (t *Tool) resumeConflict(ctx context.Context, params map[string]any, conflict error) (json.RawMessage, error) {
	var runtimeErr *RuntimeError
	if !errors.As(conflict, &runtimeErr) || runtimeErr.Code != "conflict" {
		return nil, conflict
	}
	// Use runtime truth rather than parsing the human-readable conflict message.
	raw, err := t.Sidecar.Call(ctx, "status", map[string]any{})
	if err != nil {
		return nil, err
	}
	var status struct {
		LiveSession string `json:"live_session"`
	}
	if err = json.Unmarshal(raw, &status); err != nil {
		return nil, err
	}
	if status.LiveSession == "" {
		return nil, conflict
	}
	requested := params["session"].(string)
	// Check that the requested session exists before stopping any other session.
	raw, err = t.Sidecar.Call(ctx, "session.get", map[string]any{"session": requested})
	if err != nil {
		return nil, err
	}
	var saved struct {
		Session SessionState `json:"session"`
	}
	if err = json.Unmarshal(raw, &saved); err != nil {
		return nil, err
	}
	if saved.Session.ID != requested {
		return nil, conflict
	}
	if status.LiveSession == requested {
		if saved.Session.Status != "live" {
			return nil, conflict
		}
		return raw, nil
	}
	_, err = t.Sidecar.Call(ctx, "session.stop", map[string]any{
		"session": status.LiveSession, "by": "agent", "reason": "switched to " + requested + " on user request",
	})
	if err != nil {
		return nil, err
	}
	raw, err = t.Sidecar.Call(ctx, "session.resume", params)
	if err != nil {
		return nil, fmt.Errorf("stopped session %s; resume %s failed: %w", status.LiveSession, requested, err)
	}
	var out map[string]any
	if err = json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("stopped session %s; invalid resume result: %w", status.LiveSession, err)
	}
	out["stopped_session"] = status.LiveSession
	return json.Marshal(out)
}

func toolErrorStatus(err error) tools.ResultStatus {
	if errors.Is(err, context.Canceled) {
		return tools.StatusCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return tools.StatusTimeout
	}
	return tools.StatusToolError
}
func unavailableResult() (tools.Result, error) {
	return tools.Result{Status: tools.StatusToolError, Output: `{"status":"unavailable","detail":"Enable EXCALIBUR_ROBOTLAB and use the Robot Lab install action to install the runtime and robot assets."}`}, nil
}
