package robotlab

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

type fakeSidecarAPI struct {
	calls    int
	method   string
	params   any
	err      error
	listener func(Event)
}

func (f *fakeSidecarAPI) Call(_ context.Context, m string, p any) (json.RawMessage, error) {
	f.calls++
	f.method = m
	f.params = p
	return json.RawMessage(`{"ok":true}`), f.err
}
func (f *fakeSidecarAPI) Subscribe(fn func(Event)) func() {
	f.listener = fn
	return func() { f.listener = nil }
}
func (f *fakeSidecarAPI) Status(context.Context) Status {
	return Status{Enabled: Enabled(), Requirements: Requirements{Python: ">=3.10"}}
}
func TestServiceMethods(t *testing.T) {
	t.Setenv("EXCALIBUR_ROBOTLAB", "1")
	f := &fakeSidecarAPI{}
	s := NewService(f, nil)
	defer s.Close()
	cases := []struct{ method, params, target string }{
		{"scenarios", "{}", "scenarios.list"},
		{"session.open", `{"scenario":"cartpole","seed":7}`, "session.open"},
		{"session.close", `{"session":"s"}`, "session.close"},
		{"session.reset", `{"session":"s"}`, "session.reset"},
		{"session.params", `{"session":"s","params":{}}`, "session.set_params"},
		{"session.control", `{"session":"s","mode":"manual","inputs":{"motor":0.5}}`, "session.control"},
		{"session.policy", `{"session":"s","pretrained":true}`, "session.load_policy"},
		{"session.snapshot", `{"session":"s","marks":true}`, "session.snapshot"},
		{"experiment.list", `{}`, "experiment.list"},
		{"experiment.get", `{"experiment":"e"}`, "experiment.get"},
		{"experiment.create", `{"name":"test","scenario":"cartpole","runs":[{"label":"a","algorithm":"PPO"}]}`, "experiment.create"},
		{"experiment.code", `{"experiment":"e","code":""}`, "experiment.write_code"},
		{"experiment.start", `{"experiment":"e"}`, "experiment.start"},
		{"experiment.stop", `{"experiment":"e"}`, "experiment.stop"},
		{"experiment.metrics", `{"experiment":"e","since":0}`, "experiment.metrics"},
		{"teleop.get", `{"session":"s"}`, "teleop.get_mapping"},
		{"teleop.mapping", `{"session":"s","code":null}`, "teleop.set_mapping"},
	}
	for _, tt := range cases {
		t.Run(tt.method, func(t *testing.T) {
			_, err := s.Op(context.Background(), "robotlab."+tt.method, json.RawMessage(tt.params))
			if err != nil {
				t.Fatal(err)
			}
			if f.method != tt.target {
				t.Fatalf("target %s", f.method)
			}
		})
	}
	s.VisionStep = func(_ context.Context, session, instruction string) (any, error) { return session + instruction, nil }
	result, err := s.Op(context.Background(), "robotlab.vision.step", json.RawMessage(`{"session":"s","instruction":"pick"}`))
	if err != nil || result != "spick" {
		t.Fatalf("vision hook: %v %v", result, err)
	}
}
func TestServiceValidationAndErrors(t *testing.T) {
	f := &fakeSidecarAPI{}
	s := NewService(f, nil)
	defer s.Close()
	cases := []struct{ method, params string }{
		{"teleop.get", `{}`}, {"teleop.get", `{"session":""}`}, {"teleop.get", `{"session":null}`}, {"teleop.get", `{"session":"s","code":"x"}`},
		{"scenarios", `{"surprise":true}`}, {"status", `null`}, {"install", `[]`}, {"session.open", `{}`}, {"session.open", `{"scenario":null}`}, {"session.open", `{"scenario":"cartpole"} {}`},
		{"session.control", `{"session":"s","mode":"bad"}`}, {"session.control", `{"session":"s","mode":"manual","inputs":{"a":2}}`},
		{"session.control", `{"session":"s","mode":"pose","pose":{"landmarks":[],"t":0,"video":"bad"}}`},
		{"session.policy", `{"session":"s"}`}, {"session.policy", `{"session":"s","pretrained":true,"experiment":"e"}`},
		{"session.snapshot", `{"session":"s","width":-1}`},
		{"experiment.create", `{"name":"e","scenario":"cartpole","runs":[{"label":"p","algorithm":"PPO","unknown":1}]}`},
		{"experiment.metrics", `{"experiment":"e","since":-1}`},
		{"experiment.code", `{"experiment":"e","code":"` + strings.Repeat("x", (256<<10)+1) + `"}`},
	}
	for _, tt := range cases {
		t.Run(tt.method, func(t *testing.T) {
			_, err := s.Op(context.Background(), "robotlab."+tt.method, json.RawMessage(tt.params))
			var ae *api.Error
			if !errors.As(err, &ae) || ae.Code != api.CodeInvalidParams {
				t.Fatalf("validation error: %v", err)
			}
		})
	}
	if f.calls != 0 {
		t.Fatal("invalid params reached sidecar")
	}
	for _, tt := range []struct {
		code string
		want int
	}{{"invalid_params", api.CodeInvalidParams}, {"not_found", api.CodeUnknownTarget}, {"conflict", api.CodeConflict}, {"unavailable", api.CodeRobotLabUnavailable}, {"internal", api.CodeInternal}} {
		f.err = &RuntimeError{tt.code, "failed"}
		_, err := s.Op(context.Background(), "robotlab.scenarios", json.RawMessage(`{}`))
		var ae *api.Error
		if !errors.As(err, &ae) || ae.Code != tt.want {
			t.Fatal(err)
		}
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
		t.Fatal("frame retained or not forwarded")
	}
	f.listener(Event{Name: "metrics", Data: json.RawMessage(`{"experiment":"e","points":[]}`)})
	if bus.Len() != 1 {
		t.Fatal("metrics not published")
	}
	t.Setenv("EXCALIBUR_ROBOTLAB", "off")
	for _, m := range []string{"scenarios", "install", "session.open", "vision.step"} {
		_, err := s.Op(context.Background(), "robotlab."+m, json.RawMessage(`{}`))
		var ae *api.Error
		if !errors.As(err, &ae) || ae.Code != api.CodeRobotLabUnavailable {
			t.Fatal(err)
		}
	}
	result, err := s.Op(context.Background(), "robotlab.status", nil)
	if err != nil || result.(Status).Enabled {
		t.Fatalf("disabled status: %v %v", result, err)
	}
	if f.calls != 0 {
		t.Fatal("disabled sidecar started")
	}
}
func TestRealRuntimeOptIn(t *testing.T) {
	if osPython := os.Getenv("EXCALIBUR_ROBOTLAB_PYTHON"); osPython == "" {
		t.Skip("set EXCALIBUR_ROBOTLAB_PYTHON for the local runtime integration check")
	}
	s := NewSidecar(t.TempDir())
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	raw, err := s.Call(ctx, "scenarios.list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Scenarios []struct {
			ID string `json:"id"`
		} `json:"scenarios"`
	}
	if err = json.Unmarshal(raw, &result); err != nil || len(result.Scenarios) == 0 {
		t.Fatalf("scenarios: %s %v", raw, err)
	}
	if _, err = s.Call(ctx, "session.open", map[string]any{"scenario": result.Scenarios[0].ID}); err != nil {
		t.Fatal(err)
	}
}

func TestServiceCodeAndPoseByteLimits(t *testing.T) {
	t.Setenv("EXCALIBUR_ROBOTLAB", "1")
	runtime := &fakeSidecarAPI{}
	service := NewService(runtime, nil)
	defer service.Close()
	ctx := context.Background()
	code, _ := json.Marshal(map[string]any{"experiment": "e", "code": strings.Repeat("x", 256<<10)})
	if _, err := service.Op(ctx, "robotlab.experiment.code", code); err != nil {
		t.Fatalf("code boundary: %v", err)
	}
	landmarks := make([][]float64, 33)
	for n := range landmarks {
		landmarks[n] = []float64{0, 0, 0, 1}
	}
	pose, _ := json.Marshal(map[string]any{"landmarks": landmarks, "t": 0})
	params := append([]byte(`{"session":"s","mode":"pose","pose":`), pose...)
	params = append(params, '}')
	if _, err := service.Op(ctx, "robotlab.session.control", params); err != nil {
		t.Fatalf("valid pose: %v", err)
	}
	// A valid landmark shape can still exceed the wire byte cap through JSON
	// whitespace. Reject it before forwarding rather than checking only shape.
	paddedPose := string(pose[:len(pose)-1]) + strings.Repeat(" ", 64<<10) + "}"
	oversized := json.RawMessage(`{"session":"s","mode":"pose","pose":` + paddedPose + `}`)
	if _, err := service.Op(ctx, "robotlab.session.control", oversized); err == nil || !strings.Contains(err.Error(), "pose exceeds") {
		t.Fatalf("pose cap: %v", err)
	}
	if runtime.calls != 2 {
		t.Fatal("oversized pose reached runtime")
	}
}
