package robotlab

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestTurnStartTracksParentContext(t *testing.T) {
	s := NewService(nil, nil)
	defer s.Close()
	turn, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.RecordTurnStart(turn, "c")
	first := s.TurnStartedAt(turn, "c")
	if first.IsZero() || first.After(time.Now()) {
		t.Fatal("missing turn start")
	}
	s.RecordTurnStart(turn, "c")
	if s.TurnStartedAt(turn, "c") != first {
		t.Fatal("same parent turn changed start")
	}
	next, stop := context.WithCancel(context.Background())
	defer stop()
	s.RecordTurnStart(next, "c")
	if !s.TurnStartedAt(turn, "c").IsZero() || s.TurnStartedAt(next, "c").Before(first) {
		t.Fatal("new parent turn reused old marker")
	}
	if !s.TurnStartedAt(turn, "other").IsZero() {
		t.Fatal("conversation marker leaked")
	}
}

func TestLiveSessionRejectsStaleRuntimeState(t *testing.T) {
	for _, body := range []string{
		`{"session":{"id":"current","status":"stopped","conversation":"c"}}`,
		`{"session":{"id":"current","status":"succeeded","conversation":"c"}}`,
		`{"session":{"id":"old","status":"live","conversation":"c"}}`,
		`{"session":{"id":"current","status":"live","conversation":"other"}}`,
	} {
		t.Run(body, func(t *testing.T) {
			sidecar := &toolSidecarFake{call: func(_ context.Context, method string, _ any) (json.RawMessage, error) {
				if method == "status" {
					return json.RawMessage(`{"live_session":"current"}`), nil
				}
				return json.RawMessage(body), nil
			}}
			s := NewService(sidecar, nil)
			defer s.Close()
			state, err := s.LiveSession(context.Background(), "c")
			if err != nil || state != nil {
				t.Fatalf("stale session accepted: %+v %v", state, err)
			}
		})
	}
}

type fakeSidecarAPI struct {
	calls     int
	method    string
	params    any
	err       error
	result    json.RawMessage
	installed bool
	listener  func(Event)
}

func (f *fakeSidecarAPI) Call(_ context.Context, m string, p any) (json.RawMessage, error) {
	f.calls++
	f.method = m
	f.params = p
	if f.result != nil {
		return f.result, f.err
	}
	return json.RawMessage(`{"ok":true}`), f.err
}
func (f *fakeSidecarAPI) Subscribe(fn func(Event)) func() {
	f.listener = fn
	return func() { f.listener = nil }
}
func (f *fakeSidecarAPI) Status(context.Context) Status {
	return Status{Enabled: Enabled(), Installed: f.installed, Requirements: Requirements{Python: ">=3.10"}}
}

var forwardingCases = []struct{ method, params string }{
	{"robots.list", `{}`}, {"env.list", `{}`}, {"env.get", `{"id":"room"}`},
	{"env.save", `{"environment":{"version":1,"id":"room","name":"Room","kind":"room","objects":[]},"create":true}`},
	{"env.delete", `{"id":"room"}`}, {"env.thumbnail", `{"id":"room"}`}, {"env.scene", `{"id":"room"}`}, {"env.preview", `{"id":"room","robot":"dog"}`},
	{"settings.get", `{}`}, {"settings.set", `{"max_attempts":100,"sim_speed":0.25}`},
	{"session.start", `{"robot":"dog","environment":"room","task":"Hold ball","goal":{"type":"object_held","object":"ball"},"conversation":"c"}`},
	{"session.resume", `{"session":"s","reason":"Retry previous simulation","conversation":"c"}`},
	{"session.list", `{"robot":"dog","environment":"room","query":"Hold"}`}, {"session.get", `{"session":"s"}`}, {"session.stop", `{"session":"s","by":"user"}`},
	{"session.scene", `{"session":"s"}`}, {"session.image", `{"session":"s","path":"attempts/1/looks/1.png"}`}, {"session.log", `{"session":"s","attempt":1,"since":0}`},
	{"attempt.reset", `{"session":"s","reflection":"Approached too quickly"}`}, {"attempt.frames", `{"session":"s","attempt":1,"from":0,"count":400}`},
	{"robot.act", `{"session":"s","tool":"move_joints","args":{"joints":{"FL_hip":0},"duration":0.5}}`}, {"robot.look", `{"session":"s","camera":"robot/head","width":640,"height":480}`}, {"robot.state", `{"session":"s"}`},
	{"skills.list", `{"session":"s"}`}, {"skills.read", `{"session":"s","name":"steady-pose"}`},
	{"skills.write", `{"session":"s","name":"steady-pose","description":"Hold a steady pose","instructions":"Keep the planted feet fixed","failures":[1]}`},
	{"skills.use", `{"session":"s","name":"steady-pose"}`},
	{"skills.copy", `{"from":"previous","to":"s"}`}, {"log.note", `{"session":"s","text":"Approach slowly","kind":"plan"}`}, {"mesh.get", `{"key":"0123456789abcdef"}`},
}

func TestServiceMethodsForwardOneToOne(t *testing.T) {
	t.Setenv("EXCALIBUR_ROBOTLAB", "1")
	f := &fakeSidecarAPI{}
	s := NewService(f, nil)
	defer s.Close()
	for _, tc := range forwardingCases {
		t.Run(tc.method, func(t *testing.T) {
			result, err := s.Op(context.Background(), "robotlab."+tc.method, json.RawMessage(tc.params))
			if err != nil {
				t.Fatal(err)
			}
			if f.method != tc.method || string(result.(json.RawMessage)) != `{"ok":true}` {
				t.Fatalf("incorrect forwarding: %s %v", f.method, result)
			}
			want := map[string]any{}
			_ = json.Unmarshal([]byte(tc.params), &want)
			got, _ := json.Marshal(f.params)
			expected, _ := json.Marshal(want)
			if string(got) != string(expected) {
				t.Fatalf("params changed: %s", got)
			}
		})
	}
	if f.calls != len(forwardingCases) {
		t.Fatal(f.calls)
	}
}

func TestSessionImageForwarding(t *testing.T) {
	t.Setenv("EXCALIBUR_ROBOTLAB", "1")
	f := &fakeSidecarAPI{result: json.RawMessage(`{"png":"cG5n"}`)}
	s := NewService(f, nil)
	defer s.Close()
	for _, path := range []string{"attempts/1/looks/1.png", "attempts/20/failure/image.png"} {
		params, err := json.Marshal(api.RobotLabSessionImageParams{Session: "s", Path: path})
		if err != nil {
			t.Fatal(err)
		}
		result, err := s.Op(context.Background(), api.MethodRobotLabSessionImage, params)
		if err != nil {
			t.Fatal(err)
		}
		if f.method != "session.image" || string(result.(json.RawMessage)) != string(f.result) {
			t.Fatalf("image response changed: %s %v", f.method, result)
		}
		got := f.params.(map[string]any)
		if got["session"] != "s" || got["path"] != path {
			t.Fatalf("image params changed: %v", got)
		}
	}
}

func TestAttemptResetSkillReason(t *testing.T) {
	t.Setenv("EXCALIBUR_ROBOTLAB", "1")
	for _, tc := range []struct {
		name   string
		reason any
		omit   bool
		valid  bool
	}{
		{name: "omitted", omit: true, valid: true},
		{name: "explanation", reason: "The attempt was interrupted before any action.", valid: true},
		{name: "limit", reason: strings.Repeat("x", 2000), valid: true},
		{name: "unicode limit", reason: strings.Repeat("界", 2000), valid: true},
		{name: "too long", reason: strings.Repeat("x", 2001)},
		{name: "unicode too long", reason: strings.Repeat("界", 2001)},
		{name: "null", reason: nil},
		{name: "number", reason: 1},
		{name: "boolean", reason: true},
		{name: "array", reason: []string{"interrupted"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeSidecarAPI{}
			s := NewService(f, nil)
			defer s.Close()
			params := map[string]any{"session": "s", "reflection": "Reassess the approach."}
			if !tc.omit {
				params["no_skill_reason"] = tc.reason
			}
			raw, _ := json.Marshal(params)
			_, err := s.Op(context.Background(), api.MethodRobotLabAttemptReset, raw)
			if !tc.valid {
				var ae *api.Error
				if !errors.As(err, &ae) || ae.Code != api.CodeInvalidParams || f.calls != 0 {
					t.Fatalf("invalid reason reached runtime: calls=%d err=%v", f.calls, err)
				}
				return
			}
			if err != nil || f.calls != 1 || f.method != "attempt.reset" {
				t.Fatalf("reset forwarding: calls=%d method=%s err=%v", f.calls, f.method, err)
			}
			got, _ := json.Marshal(f.params)
			if string(got) != string(raw) {
				t.Fatal("reset parameters changed")
			}
		})
	}
}

func TestSessionImageRejectsInvalidPaths(t *testing.T) {
	t.Setenv("EXCALIBUR_ROBOTLAB", "1")
	f := &fakeSidecarAPI{}
	s := NewService(f, nil)
	defer s.Close()
	for _, path := range []string{
		"", "/attempts/1/looks/1.png", "../attempts/1/looks/1.png",
		"attempts/0/looks/1.png", "attempts/1/looks/../failure/image.png",
		"attempts/1/looks/nested/1.png", `attempts/1/looks/..\image.png`,
		"attempts/1/failure/state.json", "attempts/1/failure/other.png", "attempts/1/looks/1.jpg",
	} {
		t.Run(path, func(t *testing.T) {
			params, _ := json.Marshal(map[string]any{"session": "s", "path": path})
			_, err := s.Op(context.Background(), api.MethodRobotLabSessionImage, params)
			var ae *api.Error
			if !errors.As(err, &ae) || ae.Code != api.CodeInvalidParams {
				t.Fatalf("expected invalid image path, got %v", err)
			}
		})
	}
	for _, params := range []string{`{}`, `{"session":"s"}`, `{"path":"attempts/1/looks/1.png"}`, `{"session":"s","path":null}`} {
		_, err := s.Op(context.Background(), api.MethodRobotLabSessionImage, json.RawMessage(params))
		var ae *api.Error
		if !errors.As(err, &ae) || ae.Code != api.CodeInvalidParams {
			t.Fatalf("expected invalid params, got %v", err)
		}
	}
	if f.calls != 0 {
		t.Fatal("invalid image params reached sidecar")
	}
}

func TestServiceValidationAndErrors(t *testing.T) {
	f := &fakeSidecarAPI{}
	s := NewService(f, nil)
	defer s.Close()
	cases := []struct{ method, params string }{
		{"env.list", `{"unexpected":1}`}, {"status", `null`}, {"install", `[]`}, {"env.get", `{}`}, {"env.get", `{"id":null}`}, {"env.get", `{"id":""}`}, {"env.get", `{"id":"../bad"}`}, {"env.get", `{"id":"room"} {}`},
		{"session.start", `{"robot":"rocket","environment":"room","task":"Go","goal":{}}`},
		{"session.start", `{"robot":"dog","environment":"room","task":"Go","goal":{"type":"object_near","object":"ball","radius":1}}`},
		{"session.start", `{"robot":"dog","environment":"room","task":"Go","goal":{"type":"robot_near","target":{"robot_start":false},"radius":1}}`},
		{"session.start", `{"robot":"dog","environment":"room","task":"Go","goal":{"type":"all","goals":[]}}`},
		{"session.stop", `{"session":"s","by":"someone"}`}, {"attempt.reset", `{"session":"s","reflection":" "}`},
		{"settings.set", `{"max_attempts":0}`}, {"settings.set", `{"max_attempts":101}`}, {"settings.set", `{"max_attempts":1.5}`}, {"settings.set", `{"sim_speed":4.01}`}, {"settings.set", `{"sim_speed":0.24}`},
		{"attempt.frames", `{"session":"s","attempt":1,"from":0,"count":401}`}, {"attempt.frames", `{"session":"s","attempt":1,"from":-1,"count":1}`},
		{"robot.act", `{"session":"s","tool":"move_joints","args":null}`}, {"robot.look", `{"session":"s","width":0}`}, {"robot.look", `{"session":"s","height":0}`},
		{"skills.write", `{"session":"s","name":"Bad","description":"d","instructions":"i"}`}, {"skills.write", `{"session":"s","name":"good","description":"d","instructions":"i","failures":[0]}`},
		{"log.note", `{"session":"s","text":"a","kind":"act"}`}, {"mesh.get", `{"key":"not-a-key"}`},
		{"env.save", `{"environment":{"version":1,"id":"room","name":"Room","kind":"room","objects":[{"id":"ball","shape":"sphere","size":[1],"pos":[0,0],"color":"red"}]},"create":true}`},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			_, err := s.Op(context.Background(), "robotlab."+tc.method, json.RawMessage(tc.params))
			var ae *api.Error
			if !errors.As(err, &ae) || ae.Code != api.CodeInvalidParams {
				t.Fatalf("validation: %v", err)
			}
		})
	}
	if f.calls != 0 {
		t.Fatal("invalid params reached sidecar")
	}
	for _, tc := range []struct {
		code string
		want int
	}{{"invalid_params", api.CodeInvalidParams}, {"not_found", api.CodeUnknownTarget}, {"conflict", api.CodeConflict}, {"unavailable", api.CodeRobotLabUnavailable}, {"internal", api.CodeInternal}} {
		f.err = &RuntimeError{tc.code, "failed"}
		_, err := s.Op(context.Background(), "robotlab.env.list", nil)
		var ae *api.Error
		if !errors.As(err, &ae) || ae.Code != tc.want {
			t.Fatal(err)
		}
	}
	_, err := s.Op(context.Background(), "robotlab.session.open", nil)
	var ae *api.Error
	if !errors.As(err, &ae) || ae.Code != api.CodeMethodNotFound {
		t.Fatal(err)
	}
}
func TestServiceEventsAndSwitch(t *testing.T) {
	f := &fakeSidecarAPI{}
	bus := telemetry.NewBus(nil)
	s := NewService(f, bus)
	defer s.Close()
	var frame types.Event
	unsub := bus.SubscribeEphemeral(func(e types.Event) { frame = e })
	defer unsub()
	f.listener(Event{Name: "frame", Data: json.RawMessage(`{"session":"s"}`)})
	if frame.Type != "robotlab.frame" || bus.Len() != 0 {
		t.Fatal("frame retained or missing")
	}
	for _, name := range []string{"session", "log"} {
		f.listener(Event{Name: name, Data: json.RawMessage(`{}`)})
	}
	f.listener(Event{Name: "experiment", Data: json.RawMessage(`{}`)})
	if bus.Len() != 2 {
		t.Fatal("wrong persistent event count")
	}
	t.Setenv("EXCALIBUR_ROBOTLAB", "off")
	_, err := s.Op(context.Background(), "robotlab.robots.list", nil)
	var ae *api.Error
	if !errors.As(err, &ae) || ae.Code != api.CodeRobotLabUnavailable {
		t.Fatal(err)
	}
	st, err := s.Op(context.Background(), "robotlab.status", nil)
	if err != nil || st.(Status).Enabled || f.calls != 0 {
		t.Fatal(st, err)
	}
}
func TestStatusMergeAndLazyUnavailable(t *testing.T) {
	f := &fakeSidecarAPI{installed: true, result: json.RawMessage(`{"version":"3","live_session":"s","robots":2,"environments":2}`)}
	s := NewService(f, nil)
	defer s.Close()
	result, err := s.Op(context.Background(), "robotlab.status", nil)
	if err != nil {
		t.Fatal(err)
	}
	st := result.(Status)
	if st.LiveSession == nil || *st.LiveSession != "s" || st.Version != "3" || st.Robots != 2 || st.Environments != 2 || !st.Installed || !st.Running {
		t.Fatal(st)
	}
	f.installed = false
	f.calls = 0
	_, err = s.Op(context.Background(), "robotlab.status", nil)
	if err != nil || f.calls != 0 {
		t.Fatal("uninstalled status started runtime")
	}
}
func TestServiceLimitsAndCancellation(t *testing.T) {
	s := NewService(&fakeSidecarAPI{}, nil)
	defer s.Close()
	if _, err := s.Op(context.Background(), "robotlab.env.list", json.RawMessage(strings.Repeat(" ", 8<<20)+"{}")); err == nil {
		t.Fatal("accepted oversized request")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Op(ctx, "robotlab.env.list", nil); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestObservationsRetainLargeImageSupport(t *testing.T) {
	runtime := &fakeSidecarAPI{}
	service := NewService(runtime, nil)
	defer service.Close()
	if _, err := service.Op(context.Background(), "robotlab.robot.look", json.RawMessage(`{"session":"s","width":2048,"height":2048}`)); err != nil {
		t.Fatal(err)
	}
}

func TestRetiredOperationsAreNotForwarded(t *testing.T) {
	t.Setenv("EXCALIBUR_ROBOTLAB", "1")
	runtime := &fakeSidecarAPI{}
	service := NewService(runtime, nil)
	defer service.Close()
	for _, method := range []string{"media.upload", "media.ingest", "media.get", "skills.run", "vision.step"} {
		_, err := service.Op(context.Background(), "robotlab."+method, json.RawMessage(`{}`))
		var apiErr *api.Error
		if !errors.As(err, &apiErr) || apiErr.Code != api.CodeMethodNotFound {
			t.Fatalf("retired %s: %v", method, err)
		}
	}
	if runtime.calls != 0 {
		t.Fatal("retired operation reached sidecar")
	}
	if service.VisionStep == nil {
		t.Fatal("missing compatibility hook")
	}
	if result, err := service.VisionStep(context.Background(), "s", "observe"); result != nil || err != nil {
		t.Fatalf("compatibility hook returned %v, %v", result, err)
	}
	if runtime.calls != 0 {
		t.Fatal("compatibility hook reached sidecar")
	}
}

func TestMarkdownSkillWritesRejectCode(t *testing.T) {
	t.Setenv("EXCALIBUR_ROBOTLAB", "1")
	for _, code := range []any{"", "return 1", nil} {
		runtime := &fakeSidecarAPI{}
		service := NewService(runtime, nil)
		raw, _ := json.Marshal(map[string]any{"session": "s", "name": "steady-pose", "description": "Hold", "instructions": "Use keyframes", "code": code})
		_, err := service.Op(context.Background(), "robotlab.skills.write", raw)
		service.Close()
		var apiErr *api.Error
		if !errors.As(err, &apiErr) || apiErr.Code != api.CodeInvalidParams || runtime.calls != 0 {
			t.Fatalf("skill code was accepted: calls=%d err=%v", runtime.calls, err)
		}
	}
}
