package robotlab

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/whiteboard"
)

type toolSidecarFake struct {
	calls  []string
	params []map[string]any
	call   func(context.Context, string, any) (json.RawMessage, error)
}

func (f *toolSidecarFake) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	f.calls = append(f.calls, method)
	raw, _ := json.Marshal(params)
	var copy map[string]any
	_ = json.Unmarshal(raw, &copy)
	f.params = append(f.params, copy)
	if f.call != nil {
		return f.call(ctx, method, params)
	}
	switch method {
	case "robot.look":
		return json.RawMessage(`{"png":"cG5n","visible":[{"id":"ball","distance":1}],"state":{"x":0}}`), nil
	case "env.preview":
		return json.RawMessage(`{"png":"cG5n"}`), nil
	case "media.get":
		return json.RawMessage(`{"media_id":"m","kind":"video","name":"room.mp4","spec":"Door is two meters","frames":["cG5n","cG5nMg=="]}`), nil
	case "session.start":
		return json.RawMessage(`{"session":{"id":"dog-room-abcd","status":"live","attempt":1,"max_attempts":20,"attempts":[],"skill_events":[]},"card":{"id":"dog"},"skills":[]}`), nil
	default:
		return json.RawMessage(`{"ok":true,"goal":{"met":false},"attempt":{"n":1,"status":"running"}}`), nil
	}
}
func (*toolSidecarFake) Status(context.Context) Status {
	return Status{Enabled: true, Installed: true, Running: true}
}
func (*toolSidecarFake) Subscribe(func(Event)) func() { return func() {} }

type fakePublisher struct {
	names    []string
	payloads []any
}

func (p *fakePublisher) Publish(name string, payload any) {
	p.names = append(p.names, name)
	p.payloads = append(p.payloads, payload)
}

func toolArguments(action string) json.RawMessage {
	method := toolActions[action].method
	for _, tc := range forwardingCases {
		if tc.method != method {
			continue
		}
		var args map[string]any
		_ = json.Unmarshal([]byte(tc.params), &args)
		for _, key := range toolActions[action].omit {
			delete(args, key)
		}
		args["action"] = action
		raw, _ := json.Marshal(args)
		return raw
	}
	panic("no fixture for " + action)
}
func TestToolAllActions(t *testing.T) {
	t.Setenv("EXCALIBUR_ROBOTLAB", "1")
	for action, spec := range toolActions {
		t.Run(action, func(t *testing.T) {
			sidecar := &toolSidecarFake{}
			publisher := &fakePublisher{}
			tool := NewTool(sidecar, publisher, nil, nil)
			result, err := tool.Invoke(whiteboard.WithConversation(context.Background(), "conversation"), tools.Invocation{Arguments: toolArguments(action)})
			if err != nil || result.Status != tools.StatusOK {
				t.Fatalf("%s: %+v %v", action, result, err)
			}
			if len(sidecar.calls) != 1 || sidecar.calls[0] != spec.method {
				t.Fatal(sidecar.calls)
			}
			params := sidecar.params[0]
			switch action {
			case "session_start":
				if params["conversation"] != "conversation" || len(publisher.names) != 1 || publisher.names[0] != "robotlab.focus" {
					t.Fatal(params, publisher)
				}
				focus := publisher.payloads[0].(map[string]any)
				if focus["session"] != "dog-room-abcd" || focus["robot"] != "dog" || focus["environment"] != "room" {
					t.Fatal(focus)
				}
			case "session_stop":
				if params["by"] != "agent" {
					t.Fatal(params)
				}
			case "environment_create":
				if params["create"] != true {
					t.Fatal(params)
				}
			case "environment_update":
				if params["create"] != false {
					t.Fatal(params)
				}
			}
			if action != "session_start" && len(publisher.names) != 0 {
				t.Fatal("unexpected focus")
			}
		})
	}
}
func TestToolImagesAndConciseText(t *testing.T) {
	for _, tc := range []struct {
		action   string
		images   int
		contains string
	}{{"look", 1, "visible"}, {"environment_preview", 1, ""}, {"media", 2, "Door is two meters"}} {
		t.Run(tc.action, func(t *testing.T) {
			result, err := (&Tool{Sidecar: &toolSidecarFake{}}).Invoke(context.Background(), tools.Invocation{Arguments: toolArguments(tc.action)})
			if err != nil || result.Status != tools.StatusOK || len(result.Images) != tc.images {
				t.Fatalf("%+v %v", result, err)
			}
			for _, image := range result.Images {
				if !strings.HasPrefix(image.URL, "data:image/png;base64,") {
					t.Fatal(image.URL)
				}
			}
			if strings.Contains(result.Output, "cG5n") || strings.Contains(result.Output, `"png"`) || strings.Contains(result.Output, `"frames"`) {
				t.Fatal("pixels in text")
			}
			if !strings.Contains(result.Output, tc.contains) {
				t.Fatal(result.Output)
			}
		})
	}
}

func TestToolAttemptResetSkillReason(t *testing.T) {
	t.Setenv("EXCALIBUR_ROBOTLAB", "1")
	for _, tc := range []struct {
		name   string
		reason any
		valid  bool
	}{
		{name: "explanation", reason: "The attempt ended before any action.", valid: true},
		{name: "limit", reason: strings.Repeat("界", 2000), valid: true},
		{name: "too long", reason: strings.Repeat("界", 2001)},
		{name: "wrong type", reason: false},
		{name: "null", reason: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sidecar := &toolSidecarFake{}
			raw, _ := json.Marshal(map[string]any{
				"action": "attempt_reset", "session": "s", "reflection": "Reassess the approach.", "no_skill_reason": tc.reason,
			})
			result, err := (&Tool{Sidecar: sidecar}).Invoke(context.Background(), tools.Invocation{Arguments: raw})
			if !tc.valid {
				if err != nil || result.Status != tools.StatusInputError || len(sidecar.calls) != 0 {
					t.Fatalf("invalid reason reached runtime: %+v %v", result, err)
				}
				return
			}
			if err != nil || result.Status != tools.StatusOK || len(sidecar.calls) != 1 || sidecar.calls[0] != "attempt.reset" {
				t.Fatalf("reset failed: %+v %v", result, err)
			}
			if got := sidecar.params[0]; got["session"] != "s" || got["reflection"] != "Reassess the approach." || got["no_skill_reason"] != tc.reason {
				t.Fatal("reset parameters changed")
			}
		})
	}
}
func TestToolActionSchemas(t *testing.T) {
	bad := []string{
		`{"action":"act","session":"s","tool":"walk"}`,
		`{"action":"act","session":"s","tool":"walk","args":null}`,
		`{"action":"look","session":"s","height":0}`,
		`{"action":"session_start","robot":"dog","environment":"room","task":"Go","goal":{"type":"object_held","object":"ball"},"conversation":"forged"}`,
		`{"action":"session_stop","session":"s","by":"user"}`,
		`{"action":"attempt_reset","session":"s"}`,
		`{"action":"attempt_reset","session":"s","reflection":" "}`,
		`{"action":"skill_write","session":"s","name":"a","description":"d"}`,
		`{"action":"environment_create","environment":{},"create":false}`,
		`{"action":"robots","session":"s"}`,
		`{"action":"open","scenario":"arm"}`, `null`, `[]`, `{}`, `{} {}`,
	}
	for _, raw := range bad {
		sidecar := &toolSidecarFake{}
		result, err := (&Tool{Sidecar: sidecar}).Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(raw)})
		if err != nil || result.Status != tools.StatusInputError || len(sidecar.calls) != 0 {
			t.Fatalf("invalid input passed: %s %+v %v", raw, result, err)
		}
	}
	def := (&Tool{}).Definition()
	if def.Timeout != 95*time.Second || def.Idempotency != types.IdemNonIdempotent || def.Group != "science" {
		t.Fatal(def)
	}
	if chars := utf8.RuneCountInString(def.Description); chars > 3500 {
		t.Fatalf("tool description exceeds 3500 characters: %d", chars)
	}
	for _, phrase := range []string{"copy_skills_from", "skills_copy", "goal.met", "0.9×2.0", "ambiguous measurements", "no_skill_reason"} {
		if !strings.Contains(def.Description, phrase) {
			t.Fatal("missing training guidance: " + phrase)
		}
	}
	// The loop must teach skill use before action and skill writing before reset.
	remaining := def.Description
	for _, step := range []string{
		"(1) session_start", "body card", "list skills", "skill_use relevant",
		"(2) note(kind=plan)",
		"(3) act/look in small steps", "look again before fine manipulation", "objects can be pushed by the robot",
		"(4) After EVERY failed or abandoned attempt", "reflect (note kind=reflection)",
		"skill_write a concrete lesson in instructions with failures:[N]", "add Python code", "improve an existing skill",
		"(5) Only then attempt_reset(reflection)",
		"(6) At the end, summarize which skills were created/improved and their success rates",
	} {
		pos := strings.Index(remaining, step)
		if pos < 0 {
			t.Fatal("missing or out-of-order training guidance: " + step)
		}
		remaining = remaining[pos+len(step):]
	}
}
func TestToolNoBlindRetriesAndCancellation(t *testing.T) {
	for _, action := range []string{"act", "skill_run", "session_start", "session_stop", "attempt_reset"} {
		sidecar := &toolSidecarFake{call: func(ctx context.Context, _ string, _ any) (json.RawMessage, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 95*time.Second {
				t.Error("missing timeout")
			}
			return nil, errors.New("runtime failed")
		}}
		result, err := (&Tool{Sidecar: sidecar}).Invoke(context.Background(), tools.Invocation{Arguments: toolArguments(action)})
		if err != nil || result.Status != tools.StatusToolError || len(sidecar.calls) != 1 {
			t.Fatalf("retried %s: %v", action, sidecar.calls)
		}
	}
	for _, tc := range []struct {
		err    error
		status tools.ResultStatus
	}{{context.Canceled, tools.StatusCanceled}, {context.DeadlineExceeded, tools.StatusTimeout}} {
		sidecar := &toolSidecarFake{call: func(context.Context, string, any) (json.RawMessage, error) { return nil, tc.err }}
		result, _ := (&Tool{Sidecar: sidecar}).Invoke(context.Background(), tools.Invocation{Arguments: toolArguments("act")})
		if result.Status != tc.status {
			t.Fatal(result)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sidecar := &toolSidecarFake{}
	result, _ := (&Tool{Sidecar: sidecar}).Invoke(ctx, tools.Invocation{Arguments: toolArguments("act")})
	if result.Status != tools.StatusCanceled || len(sidecar.calls) != 0 {
		t.Fatal(result)
	}
}
func TestToolUnavailable(t *testing.T) {
	result, _ := (&Tool{}).Invoke(context.Background(), tools.Invocation{Arguments: toolArguments("robots")})
	if result.Status != tools.StatusToolError || !strings.Contains(result.Output, "unavailable") {
		t.Fatal(result)
	}
	t.Setenv("EXCALIBUR_ROBOTLAB", "off")
	sidecar := &toolSidecarFake{}
	result, _ = (&Tool{Sidecar: sidecar}).Invoke(context.Background(), tools.Invocation{Arguments: toolArguments("robots")})
	if result.Status != tools.StatusToolError || len(sidecar.calls) != 0 {
		t.Fatal(result)
	}
}

func TestToolActLookAlias(t *testing.T) {
	args := json.RawMessage(`{"action":"act","session":"s","tool":"look","args":{"camera":"robot/head"}}`)
	result, err := (&Tool{Sidecar: &toolSidecarFake{}}).Invoke(context.Background(), tools.Invocation{Arguments: args})
	if err != nil || result.Status != tools.StatusOK || len(result.Images) != 1 || !strings.Contains(result.Output, "visible") {
		t.Fatalf("%+v %v", result, err)
	}
}
