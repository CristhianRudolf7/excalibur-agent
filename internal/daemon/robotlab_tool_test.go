package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/robotlab"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

type labSidecar struct{ methods []string }

func (s *labSidecar) Call(_ context.Context, method string, _ any) (json.RawMessage, error) {
	s.methods = append(s.methods, method)
	switch method {
	case "session.snapshot":
		return json.RawMessage(`{"image_png_b64":"image","marks":[{"id":1,"bbox":[0,0,2,2],"label":"hidden"}]}`), nil
	case "experiment.metrics":
		return json.RawMessage(`{"points":[],"next":0}`), nil
	}
	return json.RawMessage(`{"id":"e","session":"s","ok":true,"code":"pass"}`), nil
}
func (*labSidecar) Status(context.Context) robotlab.Status {
	return robotlab.Status{Enabled: true, Installed: true}
}
func (*labSidecar) Subscribe(func(robotlab.Event)) func() { return func() {} }

type labAsker struct{}

func (labAsker) AskImage(context.Context, string, string) (string, string, error) {
	return `{"objects":[],"action":null,"explanation":"Nothing to move"}`, "fake", nil
}

func TestRobotLabEmbeddedDaemonActions(t *testing.T) {
	for _, surface := range []string{"embedded", "daemon"} {
		t.Run(surface, func(t *testing.T) {
			s := &labSidecar{}
			tool := robotlab.NewTool(s, nil, labAsker{}, nil)
			var reg *tools.MapRegistry
			var ex *tools.Executor
			var cat *tools.DeferredCatalog
			if surface == "embedded" {
				e, err := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{RobotLab: tool})
				if err != nil {
					t.Fatal(err)
				}
				defer e.Close()
				reg, ex, cat = e.Registry, e.Exec, e.Catalog
			} else {
				_, e, err := buildEngine(engineWiring{RobotLabTool: tool})
				if err != nil {
					t.Fatal(err)
				}
				reg, ex, cat = e.Registry, e.Exec, e.Catalog
			}
			if !cat.IsDeferred("robot_lab") {
				t.Fatal("Robot Lab must be deferred")
			}
			r, err := ex.Execute(context.Background(), "search_tools", json.RawMessage(`{"query":"robot_lab"}`), tools.ExecuteOptions{})
			if err != nil || !strings.Contains(r.Output, "robot_lab") {
				t.Fatalf("discovery %+v %v", r, err)
			}
			cases := []struct{ args, method string }{
				{`{"action":"scenarios"}`, "scenarios.list"}, {`{"action":"open","scenario":"arm"}`, "session.open"}, {`{"action":"control","session":"s","mode":"paused"}`, "session.control"}, {`{"action":"reset","session":"s"}`, "session.reset"}, {`{"action":"set_params","session":"s","params":{}}`, "session.set_params"}, {`{"action":"snapshot","session":"s"}`, "session.snapshot"}, {`{"action":"create_experiment","name":"test","scenario":"cartpole","runs":[{"label":"p","algorithm":"PPO"}]}`, "experiment.create"}, {`{"action":"write_code","experiment":"e","code":"pass"}`, "experiment.write_code"}, {`{"action":"read_code","experiment":"e"}`, "experiment.get"}, {`{"action":"start","experiment":"e"}`, "experiment.start"}, {`{"action":"stop","experiment":"e"}`, "experiment.stop"}, {`{"action":"status","experiment":"e"}`, "experiment.get"}, {`{"action":"metrics","experiment":"e"}`, "experiment.metrics"}, {`{"action":"load_policy","session":"s","pretrained":true}`, "session.load_policy"}, {`{"action":"vision_step","session":"s","instruction":"sort"}`, "session.snapshot"}, {`{"action":"set_teleop_mapping","session":"s","code":null}`, "teleop.set_mapping"},
			}
			for _, mode := range []string{"direct", "executor", "call_tool"} {
				for _, tc := range cases {
					before := len(s.methods)
					switch mode {
					case "direct":
						real, _ := reg.Get("robot_lab")
						r, err = real.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(tc.args)})
					case "executor":
						r, err = ex.Execute(context.Background(), "robot_lab", json.RawMessage(tc.args), tools.ExecuteOptions{})
					default:
						raw, _ := json.Marshal(map[string]any{"name": "robot_lab", "arguments": json.RawMessage(tc.args)})
						r, err = ex.Execute(context.Background(), "call_tool", raw, tools.ExecuteOptions{})
					}
					if err != nil || r.Status != tools.StatusOK {
						t.Fatalf("%s %s: %+v %v", mode, tc.args, r, err)
					}
					if len(s.methods) != before+1 || s.methods[before] != tc.method {
						t.Fatalf("unexpected sidecar operation %v", s.methods[before:])
					}
					if strings.Contains(r.Output, "hidden") || strings.Contains(r.Output, "image_png_b64") {
						t.Fatal("image/label leaked")
					}
				}
			}
		})
	}
}
func TestRobotLabEvalDeniedAndDisabledListed(t *testing.T) {
	for _, profile := range []string{"default", "delegate", "eval"} {
		t.Run(profile, func(t *testing.T) {
			t.Setenv("EXCALIBUR_ROBOTLAB", "off")
			e, err := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			_, err = e.Registry.Get("robot_lab")
			if profile == "eval" {
				if err == nil || e.Catalog.IsDeferred("robot_lab") {
					t.Fatal("eval gained tool")
				}
				r, err := e.Exec.Execute(context.Background(), "call_tool", json.RawMessage(`{"name":"robot_lab","arguments":{"action":"scenarios"}}`), tools.ExecuteOptions{})
				if err == nil && r.Status == tools.StatusOK {
					t.Fatal("eval invoked Robot Lab")
				}
			} else {
				if err != nil {
					t.Fatal("disabled tool vanished")
				}
				r, _ := e.Exec.Execute(context.Background(), "robot_lab", json.RawMessage(`{"action":"scenarios"}`), tools.ExecuteOptions{})
				if !strings.Contains(r.Output, "unavailable") {
					t.Fatal(r)
				}
			}
		})
	}
}

// labVisionBackend exercises the same media dependency selected for document
// vision without invoking a provider or starting Python.
type labVisionBackend struct{ calls int }

func (*labVisionBackend) Extract(context.Context, string, string, []int) (tools.VisionResult, error) {
	return tools.VisionResult{}, nil
}
func (b *labVisionBackend) AskImage(_ context.Context, prompt, image string) (string, string, error) {
	b.calls++
	return labAsker{}.AskImage(context.Background(), prompt, image)
}

func TestRobotLabAssembledClientAndToolShareVision(t *testing.T) {
	for _, available := range []bool{true, false} {
		t.Run(map[bool]string{true: "configured", false: "unavailable"}[available], func(t *testing.T) {
			runtime := &labSidecar{}
			service := robotlab.NewService(runtime, nil)
			defer service.Close()
			backend := &labVisionBackend{}
			opts := AssembleOptions{RobotLab: service}
			if available {
				opts.Media = &tools.MediaDeps{Vision: backend}
			}
			services, err := Assemble(opts)
			if err != nil {
				t.Fatal(err)
			}
			if service.VisionStep == nil || services.RobotLabTool.Sidecar != runtime || services.RobotLabTool.Publisher != service {
				t.Fatal("service and tool were not bound together")
			}
			decision, clientErr := service.Op(context.Background(), "robotlab.vision.step", json.RawMessage(`{"session":"s","instruction":"sort"}`))
			tool, err := services.Tools.Get("robot_lab")
			if err != nil {
				t.Fatal(err)
			}
			result, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"vision_step","session":"s","instruction":"sort"}`)})
			if err != nil {
				t.Fatal(err)
			}
			if available {
				if clientErr != nil || result.Status != tools.StatusOK || backend.calls != 2 {
					t.Fatalf("client=%v tool=%+v calls=%d", clientErr, result, backend.calls)
				}
				if d, ok := decision.(robotlab.VisionDecision); !ok || d.Explanation != "Nothing to move" {
					t.Fatalf("client decision: %#v", decision)
				}
			} else if clientErr == nil || !strings.Contains(clientErr.Error(), "vision unavailable") || !strings.Contains(result.Output, "vision unavailable") || len(runtime.methods) != 0 {
				t.Fatalf("client=%v tool=%+v calls=%v", clientErr, result, runtime.methods)
			}
		})
	}
}
