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

// Tool exposes deterministic training actions. Vision is retained for callers
// compiled against the previous composition API; images now travel in Results.
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
	"environment_delete": {method: "env.delete"}, "environment_preview": {method: "env.preview"}, "media": {method: "media.get"},
	"session_start": {method: "session.start", omit: []string{"conversation"}}, "session_status": {method: "session.get"}, "session_stop": {method: "session.stop", omit: []string{"by"}},
	"attempt_reset": {method: "attempt.reset"}, "act": {method: "robot.act"}, "look": {method: "robot.look"},
	"skills": {method: "skills.list"}, "skill_read": {method: "skills.read"}, "skill_write": {method: "skills.write"}, "skill_use": {method: "skills.use"}, "skill_run": {method: "skills.run"},
	"skills_copy": {method: "skills.copy"}, "note": {method: "log.note"},
}

func (*Tool) Definition() tools.Definition {
	return tools.Definition{Name: "robot_lab", Description: `Train simulated robots by building reusable skills with deterministic tools. Robots: arm (table), dog (room, with arm), drone (room), humanoid (room). Physics advances only during act/skill_run; it is frozen between calls. Training loop: (1) session_start, read the returned body card, list skills (skills), and skill_use relevant ones. (2) note(kind=plan). (3) act/look in small steps; before navigating, read environment_get for furniture footprints (pos ± size/2) and plan waypoints with ≥ 0.35 m clearance from the robot base (walk_to moves in a straight line and stops when blocked); look again before fine manipulation because objects can be pushed by the robot; inspect state, goal and attempt. (4) After EVERY failed or abandoned attempt, reflect (note kind=reflection), then skill_write a concrete lesson in instructions with failures:[N] (attempt number); add Python code when a sequence of tool calls is reusable, or improve an existing skill. (5) Only then attempt_reset(reflection); if no skill can be written, pass an explicit no_skill_reason (max 2000 characters). Repeat until the goal or attempt limit. (6) At the end, summarize which skills were created/improved and their success rates (skills). Never claim success unless goal.met=true or the session succeeded. Skills belong to one session; reuse via copy_skills_from on session_start or skills_copy(from,to). skill_use reads instructions and marks use. skill_run executes optional Python def run(robot, **args), composing predefined tools, robot.look(), state(), objects(), log(text). Only math imports; no file/process access; 60 second simulation limit. Regression checks can reject a version. Grasp assist attaches only when the closing gripper is within 0.04 m; opening releases.
Environment actions accept version 1 JSON: {version:1,id:"room-slug",name:"Room",kind:"room"|"table",size:[x,y],objects:[{id,name,shape:"box"|"sphere"|"cylinder"|"capsule",size:[full dimensions in meters],pos:[x,y,z?],yaw:degrees,color:"#rrggbb",dynamic:true}],spawn:{x,y,yaw},source:{type:"text"|"image"|"video",media_id?,notes?}}. Use realistic sizes; small graspable things are 0.05-0.10 m and set mass (e.g. ball 0.07 m, 0.1 kg; default mass is volume*500). Sphere size [diameter], box [x,y,z], cylinder/capsule [diameter,height]; omitted z rests on floor or table. Room size is 1–30 m per side, at most 200 objects, unique ids, spawn outside fixed furniture. Tables default to 1.2×0.8 m, 0.75 m high. For environments from media, call media to see image/video frames and the user's spec; ask the user about ambiguous measurements. Use reference objects (doors about 0.9×2.0 m, tables about 0.75 m high) and the user's specification rather than inventing exact dimensions. environment_preview and look return images for inspection.
Goals: object_near(object,target,radius), robot_near(target,radius), object_held(object), object_on(object,target:{object}), all(goals:[...]); target is {point:[x,y]}, {object:id}, or {robot_start:true}. Distances are horizontal. session_stop ends training. act, skill_run and session tools must not be blindly retried. Installation is an explicit user action.`, InputSchema: toolSchema(), Effect: types.EffectExec, Idempotency: types.IdemNonIdempotent, Cancel: types.CancelCooperative, Timeout: 95 * time.Second, OutputBudget: 32000, Platforms: []string{"linux"}, Profiles: []string{"default", "delegate"}, Group: "science", Keywords: []string{"robot", "physics", "simulation", "training", "environment", "skills", "arm", "dog", "drone", "humanoid", "simulación", "simulacion", "entrenamiento", "laboratorio", "brazo", "perro", "dron", "humanoide"}}
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
	case "session_start":
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
	// Mutating operations are sent exactly once, even if runtime state is stale.
	result, err := t.Sidecar.Call(ctx, spec.method, params)
	if err != nil {
		return fail(toolErrorStatus(err), err)
	}
	var out map[string]any
	if err = json.Unmarshal(result, &out); err != nil {
		return fail(tools.StatusToolError, err)
	}
	if action == "session_start" && t.Publisher != nil {
		if session, ok := out["session"].(map[string]any); ok {
			if id, ok := session["id"].(string); ok && id != "" {
				t.Publisher.Publish("robotlab.focus", map[string]any{"session": id, "robot": params["robot"], "environment": params["environment"]})
			}
		}
	}
	images := []providers.ImageInput{}
	if action == "look" || action == "environment_preview" {
		if png, ok := out["png"].(string); ok && png != "" {
			images = append(images, providers.ImageInput{URL: "data:image/png;base64," + png})
		}
		delete(out, "png")
	}
	if action == "media" {
		if frames, ok := out["frames"].([]any); ok {
			for _, frame := range frames {
				if png, ok := frame.(string); ok && png != "" {
					images = append(images, providers.ImageInput{URL: "data:image/png;base64," + png})
				}
			}
		}
		out["frame_count"] = len(images)
		delete(out, "frames")
	}
	if session, ok := out["session"].(map[string]any); ok {
		// Full histories belong in replay/log RPCs; keep turn text focused on progress.
		delete(session, "attempts")
		delete(session, "skill_events")
	}
	raw, err = json.Marshal(out)
	return tools.Result{Status: tools.StatusOK, Output: string(raw), Images: images}, err
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
