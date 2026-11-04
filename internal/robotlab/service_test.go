package robotlab

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

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
	{"media.ingest", `{"path":"/tmp/test.png","mime":"image/png","name":"test","spec":""}`}, {"media.get", `{"media_id":"m","max":8}`},
	{"settings.get", `{}`}, {"settings.set", `{"max_attempts":100,"sim_speed":0.25}`},
	{"session.start", `{"robot":"dog","environment":"room","task":"Hold ball","goal":{"type":"object_held","object":"ball"},"conversation":"c"}`},
	{"session.list", `{}`}, {"session.get", `{"session":"s"}`}, {"session.stop", `{"session":"s","by":"user"}`},
	{"session.scene", `{"session":"s"}`}, {"session.image", `{"session":"s","path":"attempts/1/looks/1.png"}`}, {"session.log", `{"session":"s","attempt":1,"since":0}`},
	{"attempt.reset", `{"session":"s","reflection":"Approached too quickly"}`}, {"attempt.frames", `{"session":"s","attempt":1,"from":0,"count":400}`},
	{"robot.act", `{"session":"s","tool":"walk","args":{"distance":1}}`}, {"robot.look", `{"session":"s","camera":"robot/head","width":640,"height":480}`}, {"robot.state", `{"session":"s"}`},
	{"skills.list", `{"session":"s"}`}, {"skills.read", `{"session":"s","name":"slow-walk"}`},
	{"skills.write", `{"session":"s","name":"slow-walk","description":"Walk slowly","instructions":"Use small steps","code":"def run(robot): return robot.state()","failures":[1]}`},
	{"skills.use", `{"session":"s","name":"slow-walk"}`}, {"skills.run", `{"session":"s","name":"slow-walk","args":{}}`},
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
		{"robot.act", `{"session":"s","tool":"walk","args":null}`}, {"robot.look", `{"session":"s","width":0}`}, {"robot.look", `{"session":"s","height":0}`},
		{"skills.write", `{"session":"s","name":"Bad","description":"d","instructions":"i"}`}, {"skills.write", `{"session":"s","name":"good","description":"d","instructions":"i","failures":[0]}`},
		{"log.note", `{"session":"s","text":"a","kind":"act"}`}, {"media.get", `{"media_id":"m","max":9}`}, {"mesh.get", `{"key":"not-a-key"}`},
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
	f := &fakeSidecarAPI{installed: true, result: json.RawMessage(`{"version":"2","live_session":"s","robots":4,"environments":3}`)}
	s := NewService(f, nil)
	defer s.Close()
	result, err := s.Op(context.Background(), "robotlab.status", nil)
	if err != nil {
		t.Fatal(err)
	}
	st := result.(Status)
	if st.LiveSession == nil || *st.LiveSession != "s" || st.Version != "2" || st.Robots != 4 || st.Environments != 3 || !st.Installed || !st.Running {
		t.Fatal(st)
	}
	f.installed = false
	f.calls = 0
	_, err = s.Op(context.Background(), "robotlab.status", nil)
	if err != nil || f.calls != 0 {
		t.Fatal("uninstalled status started runtime")
	}
}
func TestMediaUploadSniffsAndBounds(t *testing.T) {
	f := &fakeSidecarAPI{}
	s := NewService(f, nil)
	s.DataDir = t.TempDir()
	defer s.Close()
	png := []byte("\x89PNG\r\n\x1a\n")
	upload := func(data []byte, mime string) (any, error) {
		raw, _ := json.Marshal(map[string]any{"name": "../../user.png", "mime": mime, "data": base64.StdEncoding.EncodeToString(data), "spec": "A room"})
		return s.Op(context.Background(), "robotlab.media.upload", raw)
	}
	if _, err := upload(png, "image/png"); err != nil {
		t.Fatal(err)
	}
	p := f.params.(map[string]any)
	path := p["path"].(string)
	if f.method != "media.ingest" || p["mime"] != "image/png" || p["spec"] != "A room" || filepath.Dir(path) != filepath.Join(s.DataDir, "robotlab", "uploads") {
		t.Fatal(p)
	}
	if stat, err := os.Stat(path); err != nil || stat.Mode().Perm() != 0600 {
		t.Fatal(stat, err)
	}
	if _, err := upload([]byte("plain text"), "image/png"); err == nil {
		t.Fatal("accepted non-media")
	}
	padded := make([]byte, maxUploadBytes+1)
	copy(padded, png)
	if _, err := upload(padded[:maxUploadBytes], "image/png"); err != nil {
		t.Fatal("32 MiB boundary rejected")
	}
	before := f.calls
	if _, err := upload(padded, "image/png"); err == nil {
		t.Fatal("accepted oversized media")
	}
	raw := json.RawMessage(`{"name":"x","mime":"image/png","data":"%%%%","spec":""}`)
	if _, err := s.Op(context.Background(), "robotlab.media.upload", raw); err == nil {
		t.Fatal("accepted invalid base64")
	}
	if f.calls != before {
		t.Fatal("invalid upload reached sidecar")
	}
	f.err = &RuntimeError{"internal", "failed"}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if _, err := upload(png, "image/png"); err == nil {
		t.Fatal("lost ingest error")
	}
	after, _ := os.ReadDir(filepath.Dir(path))
	if len(after) != len(entries) {
		t.Fatal("failed upload leaked file")
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

func TestV2RequestsDoNotInheritLegacyObservationOrCodeCaps(t *testing.T) {
	runtime := &fakeSidecarAPI{}
	service := NewService(runtime, nil)
	defer service.Close()
	if _, err := service.Op(context.Background(), "robotlab.robot.look", json.RawMessage(`{"session":"s","width":2048,"height":2048}`)); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"session": "s", "name": "long-skill", "description": "A composed skill", "instructions": "Follow the body card", "code": strings.Repeat("# documentation\n", 3000)})
	if _, err := service.Op(context.Background(), "robotlab.skills.write", raw); err != nil {
		t.Fatal(err)
	}
}
