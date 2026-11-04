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
	case "session.start", "session.resume":
		return json.RawMessage(`{"session":{"id":"dog-room-abcd","robot":"dog","environment":"room","status":"live","attempt":1,"max_attempts":40,"attempts":[],"skill_events":[]},"card":{"id":"dog"},"skills":[]}`), nil
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
			case "session_start", "session_resume":
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
			if action != "session_start" && action != "session_resume" && len(publisher.names) != 0 {
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
	}{{"look", 1, "visible"}, {"environment_preview", 1, ""}} {
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
		`{"action":"session_resume","session":"s","conversation":"forged"}`,
		`{"action":"session_resume"}`,
		`{"action":"media","media_id":"m"}`,
		`{"action":"skill_run","session":"s","name":"steady-pose"}`,
		`{"action":"skill_write","session":"s","name":"steady-pose","description":"Hold","instructions":"Use keyframes","code":""}`,
		`{"action":"act","session":"s","tool":"move_joints"}`,
		`{"action":"act","session":"s","tool":"move_joints","args":null}`,
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
}
func TestToolDescriptionTeachesLowLevelLearningLoop(t *testing.T) {
	description := (&Tool{}).Definition().Description
	remaining := description
	for _, step := range []string{
		"(1) session_start for new training", "robot card", "reference pose", "list skills", "skill_use relevant markdown",
		"(2) note(kind=plan)", "small keyframes", "act(tool=ik)",
		"(3) act(tool=sequence)", "prefer 4–12 keyframes", "returned state", "support, margin", "terrain_ahead", "act(tool=look)",
		"(4) After EVERY failed or abandoned attempt", "reflect (note kind=reflection)", "skill_write", "lesson AND the exact keyframes", "failures:[N]",
		"(5) Only then attempt_reset(reflection)", "no_skill_reason", "default max_attempts is 40",
		"(6) Summarize", "success rates", "Never claim success unless goal.met=true",
	} {
		pos := strings.Index(remaining, step)
		if pos < 0 {
			t.Fatalf("missing or out-of-order training guidance: %s", step)
		}
		remaining = remaining[pos+len(step):]
	}
	for _, phrase := range []string{"degrees", "simulation seconds", "30 s", "motors hold", "move_feet", "move_gripper_to", "stop_if", "copy_skills_from", "skills_copy", "ONLY from chat text", `source:{type:"text"`, "escalera-estrecha", "estante-pequeno", "robot_on", "stable_s", "respect user stop"} {
		if !strings.Contains(description, phrase) {
			t.Fatal("missing control guidance: " + phrase)
		}
	}
	for _, retired := range []string{"skill_run", "media_id", "skill.py", "walk_to", "Python", "regression"} {
		if strings.Contains(description, retired) {
			t.Fatal("retired guidance: " + retired)
		}
	}
}

func TestToolNoBlindRetriesAndCancellation(t *testing.T) {
	for _, action := range []string{"act", "session_start", "session_resume", "session_stop", "attempt_reset"} {
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

func TestToolSessionsFiltersAndCompactResults(t *testing.T) {
	t.Setenv("EXCALIBUR_ROBOTLAB", "1")
	sidecar := &toolSidecarFake{call: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		if method != "session.list" {
			t.Fatal(method)
		}
		return json.RawMessage(`{"sessions":[{"id":"stairs-2","name":"Stairs","robot":"dog","environment":"escalera-estrecha","task":"Climb","status":"stopped","attempts":2,"created_at":"2026-11-04","live":false}]}`), nil
	}}
	result, err := (&Tool{Sidecar: sidecar}).Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"sessions","robot":"dog","environment":"escalera","query":"stairs"}`)})
	if err != nil || result.Status != tools.StatusOK || !strings.Contains(result.Output, `"id":"stairs-2"`) || strings.Contains(result.Output, "skill_events") {
		t.Fatalf("%+v %v", result, err)
	}
	params := sidecar.params[0]
	if params["robot"] != "dog" || params["environment"] != "escalera" || params["query"] != "stairs" {
		t.Fatal(params)
	}
}

func TestToolResumeSwitchesLiveSession(t *testing.T) {
	t.Setenv("EXCALIBUR_ROBOTLAB", "1")
	resumes := 0
	sidecar := &toolSidecarFake{call: func(_ context.Context, method string, params any) (json.RawMessage, error) {
		switch method {
		case "session.resume":
			resumes++
			if resumes == 1 {
				return nil, &RuntimeError{Code: "conflict", Message: "another session is live"}
			}
			return json.RawMessage(`{"session":{"id":"saved","robot":"arm","environment":"estante-pequeno","status":"live","attempt":3},"card":{"id":"arm"},"skills":[]}`), nil
		case "status":
			return json.RawMessage(`{"live_session":"dog"}`), nil
		case "session.get":
			return json.RawMessage(`{"session":{"id":"saved","status":"stopped"}}`), nil
		case "session.stop":
			return json.RawMessage(`{"session":{"id":"dog","status":"stopped"}}`), nil
		default:
			t.Fatalf("unexpected call %s", method)
			return nil, nil
		}
	}}
	publisher := &fakePublisher{}
	ctx := whiteboard.WithConversation(context.Background(), "chat")
	result, err := (&Tool{Sidecar: sidecar, Publisher: publisher}).Invoke(ctx, tools.Invocation{Arguments: json.RawMessage(`{"action":"session_resume","session":"saved","reason":"Try again"}`)})
	if err != nil || result.Status != tools.StatusOK || !strings.Contains(result.Output, `"stopped_session":"dog"`) {
		t.Fatalf("%+v %v", result, err)
	}
	if got := strings.Join(sidecar.calls, ","); got != "session.resume,status,session.get,session.stop,session.resume" {
		t.Fatal(got)
	}
	stop := sidecar.params[3]
	if stop["session"] != "dog" || stop["by"] != "agent" || stop["reason"] != "switched to saved on user request" {
		t.Fatal(stop)
	}
	for _, index := range []int{0, 4} {
		if p := sidecar.params[index]; p["session"] != "saved" || p["conversation"] != "chat" || p["reason"] != "Try again" {
			t.Fatal(p)
		}
	}
	if len(publisher.names) != 1 || publisher.names[0] != "robotlab.focus" {
		t.Fatal(publisher)
	}
	focus := publisher.payloads[0].(map[string]any)
	if focus["session"] != "saved" || focus["robot"] != "arm" || focus["environment"] != "estante-pequeno" {
		t.Fatal(focus)
	}
}

func TestToolResumeAlreadyLiveSessionReturnsAndFocuses(t *testing.T) {
	sidecar := &toolSidecarFake{call: func(_ context.Context, method string, _ any) (json.RawMessage, error) {
		switch method {
		case "session.resume":
			return nil, &RuntimeError{Code: "conflict", Message: "already live"}
		case "status":
			return json.RawMessage(`{"live_session":"saved"}`), nil
		case "session.get":
			return json.RawMessage(`{"session":{"id":"saved","robot":"arm","environment":"estante-pequeno","status":"live","attempt":7}}`), nil
		default:
			t.Fatalf("unexpected mutation %s", method)
			return nil, nil
		}
	}}
	publisher := &fakePublisher{}
	result, err := (&Tool{Sidecar: sidecar, Publisher: publisher}).Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"session_resume","session":"saved"}`)})
	if err != nil || result.Status != tools.StatusOK || !strings.Contains(result.Output, `"attempt":7`) || strings.Contains(result.Output, "stopped_session") {
		t.Fatalf("%+v %v", result, err)
	}
	if len(sidecar.calls) != 3 || len(publisher.names) != 1 || publisher.payloads[0].(map[string]any)["session"] != "saved" {
		t.Fatal(sidecar.calls, publisher)
	}
}

func TestToolResumeSucceededSession(t *testing.T) {
	status := "succeeded"
	sidecar := &toolSidecarFake{call: func(_ context.Context, method string, _ any) (json.RawMessage, error) {
		if method != "session.resume" || status != "succeeded" {
			t.Fatalf("unexpected call %s with status %s", method, status)
		}
		status = "live"
		return json.RawMessage(`{"session":{"id":"saved","status":"live","attempt":2},"card":{"id":"arm"},"skills":[]}`), nil
	}}
	publisher := &fakePublisher{}
	result, err := (&Tool{Sidecar: sidecar, Publisher: publisher}).Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"session_resume","session":"saved"}`)})
	if err != nil || result.Status != tools.StatusOK || status != "live" || len(sidecar.calls) != 1 || len(publisher.names) != 1 {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestToolResumeConflictRecoveryIsBounded(t *testing.T) {
	for _, mode := range []string{"no-live", "missing-target", "stop-fails", "resume-fails", "live-changed"} {
		t.Run(mode, func(t *testing.T) {
			resumes, stops := 0, 0
			sidecar := &toolSidecarFake{call: func(_ context.Context, method string, _ any) (json.RawMessage, error) {
				switch method {
				case "session.resume":
					resumes++
					return nil, &RuntimeError{Code: "conflict", Message: "cannot resume"}
				case "status":
					if mode == "no-live" {
						return json.RawMessage(`{"live_session":null}`), nil
					}
					if mode == "live-changed" {
						return json.RawMessage(`{"live_session":"saved"}`), nil
					}
					return json.RawMessage(`{"live_session":"dog"}`), nil
				case "session.get":
					if mode == "missing-target" {
						return nil, &RuntimeError{Code: "not_found", Message: "missing"}
					}
					return json.RawMessage(`{"session":{"id":"saved","status":"stopped"}}`), nil
				case "session.stop":
					stops++
					if mode == "stop-fails" {
						return nil, errors.New("stop failed")
					}
					return json.RawMessage(`{}`), nil
				default:
					t.Fatal(method)
					return nil, nil
				}
			}}
			publisher := &fakePublisher{}
			result, err := (&Tool{Sidecar: sidecar, Publisher: publisher}).Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"session_resume","session":"saved"}`)})
			if err != nil || result.Status != tools.StatusToolError || len(publisher.names) != 0 {
				t.Fatalf("%+v %v", result, err)
			}
			wantResumes, wantStops := 1, 0
			if mode == "resume-fails" {
				wantResumes, wantStops = 2, 1
				if !strings.Contains(result.Output, "stopped session dog") {
					t.Fatal(result.Output)
				}
			}
			if mode == "stop-fails" {
				wantStops = 1
			}
			if resumes != wantResumes || stops != wantStops {
				t.Fatalf("resumes=%d stops=%d", resumes, stops)
			}
		})
	}
}

func TestToolDescriptionRoutesRetriesToSavedSessions(t *testing.T) {
	description := (&Tool{}).Definition().Description
	for _, text := range []string{"Retry/repeat", "vuelve a intentar", "sessions with robot/environment/query filters", "newest matching session", "session_resume(session)", "never session_start for a retry", "automatically stops another live session", "stopped_session", "Read skills with skill_use"} {
		if !strings.Contains(description, text) {
			t.Fatal("missing retry guidance: " + text)
		}
	}
}
