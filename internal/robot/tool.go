package robot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Tool controls the daemon-owned bridge and carries camera images to the model.
type Tool struct{ Service *Service }

type arguments struct {
	Action   string    `json:"action"`
	Pixel    []float64 `json:"pixel,omitempty"`
	Approved bool      `json:"approved,omitempty"`
	Name     string    `json:"name,omitempty"`
	Pose     []float64 `json:"pose,omitempty"`
	Joints   []float64 `json:"joints,omitempty"`
	Delta    *delta    `json:"delta,omitempty"`
	Frame    string    `json:"frame,omitempty"`
	Speed    *float64  `json:"speed,omitempty"`
	Accel    *float64  `json:"accel,omitempty"`
	Gripper  string    `json:"gripper,omitempty"`
	On       *bool     `json:"on,omitempty"`
}
type delta struct {
	DX  float64 `json:"dx"`
	DY  float64 `json:"dy"`
	DZ  float64 `json:"dz"`
	DRX float64 `json:"drx"`
	DRY float64 `json:"dry"`
	DRZ float64 `json:"drz"`
}

// Definition keeps this physical-control tool out of the eval catalog.
func (*Tool) Definition() tools.Definition {
	return tools.Definition{
		Name: "robot", Description: "Control the supervised UR10e arm and pneumatic gripper. First call look, describe what you see, and plan the steps in meters/radians. Confirmation policy comes from status/look state.approval_required: when it is false the operator has disabled confirmations, so carry out the whole requested task end to end (every motion and gripper step) without asking for approval and report what you did; when it is true or missing, before EACH motion, gripper change or freedrive entry ask the user to approve that exact action with direction, distance and speed, and set approved:true ONLY after that explicit approval in this conversation. STOP is always available and needs no approval. Pick and place: look -> locate the object pixel -> open gripper, move_pose to the approach pose above, move_pose down to grasp height, close gripper, lift, move_named to the named destination (such as entrega or caja), open gripper, lift clear. Locate computes base coordinates without moving. Look sends the latest camera image with a labeled pixel grid, image size, calibration and robot state directly to you. Home is the named TCP pose home. Camera frames may be stale: check updated_at. Workspace, speed, step and mode limits are enforced by the bridge.",
		InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["action"],"properties":{"action":{"type":"string","enum":["status","look","locate","move_named","move_relative","move_pose","move_joints","gripper","home","stop","list_poses","save_pose","freedrive"]},"pixel":{"type":"array","items":{"type":"number"},"minItems":2,"maxItems":2},"approved":{"type":"boolean","default":false},"name":{"type":"string","pattern":"^[A-Za-z0-9_-]{1,64}$"},"pose":{"type":"array","items":{"type":"number"},"minItems":6,"maxItems":6},"joints":{"type":"array","items":{"type":"number"},"minItems":6,"maxItems":6},"delta":{"type":"object","additionalProperties":false,"minProperties":1,"properties":{"dx":{"type":"number"},"dy":{"type":"number"},"dz":{"type":"number"},"drx":{"type":"number"},"dry":{"type":"number"},"drz":{"type":"number"}}},"frame":{"type":"string","enum":["base","tool"]},"speed":{"type":"number","exclusiveMinimum":0},"accel":{"type":"number","exclusiveMinimum":0},"gripper":{"type":"string","enum":["open","close"]},"on":{"type":"boolean"}}}`),
		Effect:      types.EffectWriteExternal, Idempotency: types.IdemNonIdempotent, Cancel: types.CancelImmediate,
		Timeout: 40 * time.Second, OutputBudget: 16384, Platforms: []string{"linux"}, Profiles: []string{"default"}, Group: "robot", Tags: []string{"robot", "camera", "gripper", "UR10e"}, Keywords: []string{"arm", "move", "stop", "look", "home"},
	}
}

// ResourceKeys intentionally leaves STOP outside motion leases. The bridge
// rejects concurrent effects, so STOP never waits behind a long motion.
func (*Tool) ResourceKeys(json.RawMessage) ([]string, error) { return nil, nil }

// Invoke validates action-specific requirements even when called directly.
func (t *Tool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	fail := func(status tools.ResultStatus, err error) (tools.Result, error) {
		return tools.Result{Status: status, Output: err.Error()}, err
	}
	var p arguments
	if err := decode(inv.Arguments, &p); err != nil {
		return fail(tools.StatusInputError, err)
	}
	if t.Service == nil {
		return fail(tools.StatusToolError, fmt.Errorf("robot service unavailable"))
	}
	var value any
	var raw json.RawMessage
	var err error
	switch p.Action {
	case "status":
		value = t.Service.Status(ctx)
	case "look":
		status, images, lookErr := t.Service.Look(ctx)
		if lookErr != nil {
			return fail(tools.StatusToolError, lookErr)
		}
		output, _ := json.Marshal(status)
		return tools.Result{Status: tools.StatusOK, Output: string(output), Images: images}, nil
	case "locate":
		if len(p.Pixel) != 2 {
			return fail(tools.StatusInputError, fmt.Errorf("locate requires pixel:[u,v]"))
		}
		value, err = t.Service.Locate(ctx, p.Pixel)
	case "stop":
		raw, err = t.Service.Stop(ctx)
	case "list_poses":
		raw, err = t.Service.Call(ctx, http.MethodGet, "/poses", nil)
	case "save_pose":
		if p.Name == "" {
			return fail(tools.StatusInputError, fmt.Errorf("save_pose requires name"))
		}
		raw, err = t.Service.Call(ctx, http.MethodPost, "/poses", map[string]string{"name": p.Name})
	case "move_named", "move_pose", "move_joints", "move_relative", "home", "gripper", "freedrive":
		entering := p.Action != "freedrive" || p.On == nil || *p.On
		if entering && !p.Approved {
			if t.approvalRequired(ctx) {
				return fail(tools.StatusDenied, fmt.Errorf("explicit user approval required; approved:true only after approval of this exact action"))
			}
			// The operator disabled confirmations in robot.json; the bridge enforces the same setting.
			p.Approved = true
		}
		body := map[string]any{"approved": p.Approved}
		path := "/move"
		if p.Speed != nil {
			body["speed"] = *p.Speed
		}
		if p.Accel != nil {
			body["accel"] = *p.Accel
		}
		switch p.Action {
		case "home":
			body["kind"] = "named"
			body["name"] = "home"
		case "move_named":
			if p.Name == "" {
				return fail(tools.StatusInputError, fmt.Errorf("move_named requires name"))
			}
			body["kind"] = "named"
			body["name"] = p.Name
		case "move_pose":
			if len(p.Pose) != 6 {
				return fail(tools.StatusInputError, fmt.Errorf("move_pose requires six pose coordinates"))
			}
			body["kind"] = "pose"
			body["pose"] = p.Pose
		case "move_joints":
			if len(p.Joints) != 6 {
				return fail(tools.StatusInputError, fmt.Errorf("move_joints requires six joint angles"))
			}
			body["kind"] = "joints"
			body["joints"] = p.Joints
		case "move_relative":
			if p.Delta == nil {
				return fail(tools.StatusInputError, fmt.Errorf("move_relative requires delta"))
			}
			if p.Frame != "" && p.Frame != "base" && p.Frame != "tool" {
				return fail(tools.StatusInputError, fmt.Errorf("frame must be base or tool"))
			}
			body["kind"] = "relative"
			body["delta"] = p.Delta
			if p.Frame != "" {
				body["frame"] = p.Frame
			}
		case "gripper":
			if p.Gripper != "open" && p.Gripper != "close" {
				return fail(tools.StatusInputError, fmt.Errorf("gripper must be open or close"))
			}
			path = "/gripper"
			body["action"] = p.Gripper
		case "freedrive":
			if p.On == nil {
				return fail(tools.StatusInputError, fmt.Errorf("freedrive requires on"))
			}
			path = "/freedrive"
			body["on"] = *p.On
		}
		raw, err = t.Service.Call(ctx, http.MethodPost, path, body)
	default:
		return fail(tools.StatusInputError, fmt.Errorf("unknown robot action %q", p.Action))
	}
	if err != nil {
		status := tools.StatusToolError
		if ctx.Err() != nil {
			status = tools.StatusCanceled
		}
		return fail(status, err)
	}
	if raw != nil {
		return tools.Result{Status: tools.StatusOK, Output: string(raw)}, nil
	}
	output, err := json.Marshal(value)
	if err != nil {
		return fail(tools.StatusToolError, err)
	}
	return tools.Result{Status: tools.StatusOK, Output: string(output)}, nil
}

// approvalRequired fails closed: only an explicit approval_required:false from the bridge waives confirmation.
func (t *Tool) approvalRequired(ctx context.Context) bool {
	if t.Service == nil {
		return true
	}
	state := t.Service.Status(ctx).State
	return state == nil || state.ApprovalRequired == nil || *state.ApprovalRequired
}
