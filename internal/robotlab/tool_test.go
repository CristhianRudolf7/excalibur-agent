package robotlab

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

type toolSidecarFake struct {
	calls  []string
	params []string
	call   func(context.Context, string, any) (json.RawMessage, error)
}

func (f *toolSidecarFake) Call(ctx context.Context, m string, p any) (json.RawMessage, error) {
	f.calls = append(f.calls, m)
	raw, _ := json.Marshal(p)
	f.params = append(f.params, string(raw))
	if f.call != nil {
		return f.call(ctx, m, p)
	}
	switch m {
	case "session.snapshot":
		return json.RawMessage(`{"image_png_b64":"image-secret","width":640,"height":480,"marks":[{"id":1,"bbox":[1,2,3,4],"label":"ground-truth-secret","name":"secret"}]}`), nil
	case "experiment.metrics":
		return json.RawMessage(`{"points":[{"run":"r","step":1,"episode_return":2},{"run":"r","step":4,"episode_return":5},{"run":"r","step":7,"episode_return":3}],"next":3}`), nil
	case "session.skill":
		return json.RawMessage(`{"ok":true}`), nil
	}
	return json.RawMessage(`{"session":"s","id":"e","code":"print('hello')","scene":{"frames":"secret"}}`), nil
}
func (*toolSidecarFake) Subscribe(func(Event)) func()  { return func() {} }
func (*toolSidecarFake) Status(context.Context) Status { return Status{Enabled: true, Installed: true} }

type fakePublisher struct {
	names    []string
	payloads []string
}

func (f *fakePublisher) Publish(name string, p any) {
	f.names = append(f.names, name)
	raw, _ := json.Marshal(p)
	f.payloads = append(f.payloads, string(raw))
}

type fakeAsker struct {
	answers []string
	prompts []string
	call    func(context.Context) (string, string, error)
}

func (f *fakeAsker) AskImage(ctx context.Context, prompt, image string) (string, string, error) {
	f.prompts = append(f.prompts, prompt)
	if f.call != nil {
		return f.call(ctx)
	}
	a := f.answers[0]
	f.answers = f.answers[1:]
	return a, "fake-vision", nil
}

const goodDecision = `{"objects":[{"mark":1,"class":"red cube","reason":"red square"}],"action":{"mark":1,"target":"A"},"explanation":"Sort by color"}`

// Every action runs through the direct seam and the real executor/discovery path.
func TestToolActions(t *testing.T) {
	cases := []string{`{"action":"scenarios"}`, `{"action":"open","scenario":"arm"}`, `{"action":"control","session":"s","mode":"manual","inputs":{"joint":0.5}}`, `{"action":"reset","session":"s"}`, `{"action":"set_params","session":"s","params":{"task":"sort"}}`, `{"action":"snapshot","session":"s"}`, `{"action":"create_experiment","name":"Compare","scenario":"cartpole","runs":[{"label":"PPO","algorithm":"PPO"}]}`, `{"action":"write_code","experiment":"e","code":"print('hello')"}`, `{"action":"read_code","experiment":"e"}`, `{"action":"start","experiment":"e"}`, `{"action":"stop","experiment":"e"}`, `{"action":"status"}`, `{"action":"status","experiment":"e"}`, `{"action":"metrics","experiment":"e"}`, `{"action":"load_policy","session":"s","pretrained":true}`, `{"action":"vision_step","session":"s","instruction":"Sort red"}`, `{"action":"set_teleop_mapping","session":"s","code":null}`}
	for _, mode := range []string{"direct", "executor", "call_tool"} {
		for _, args := range cases {
			t.Run(mode+args, func(t *testing.T) {
				s := &toolSidecarFake{}
				p := &fakePublisher{}
				tool := &Tool{Sidecar: s, Publisher: p, Vision: &VisionLoop{Sidecar: s, Publisher: p, Asker: &fakeAsker{answers: []string{goodDecision, `{"verified":true}`}}}}
				reg := tools.NewMapRegistry()
				if err := tools.RegisterBuiltins(reg, nil); err != nil {
					t.Fatal(err)
				}
				cat := tools.NewDeferredCatalog(reg)
				if err := cat.RegisterDeferred(tool); err != nil {
					t.Fatal(err)
				}
				ex := tools.NewExecutor(reg)
				tools.WireSearchCall(reg, cat, ex)
				var r tools.Result
				var err error
				switch mode {
				case "direct":
					r, err = tool.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(args)})
				case "executor":
					r, err = ex.Execute(context.Background(), "robot_lab", json.RawMessage(args), tools.ExecuteOptions{})
				case "call_tool":
					raw, _ := json.Marshal(map[string]any{"name": "robot_lab", "arguments": json.RawMessage(args)})
					r, err = ex.Execute(context.Background(), "call_tool", raw, tools.ExecuteOptions{})
				}
				if err != nil || r.Status != tools.StatusOK {
					t.Fatalf("%+v %v", r, err)
				}
				if strings.Contains(r.Output, "image-secret") || strings.Contains(r.Output, "ground-truth-secret") {
					t.Fatal("snapshot leaked image or label")
				}
				if strings.Contains(args, `"metrics"`) && !strings.Contains(r.Output, `"trend":"improving"`) {
					t.Fatal(r.Output)
				}
				var input map[string]any
				_ = json.Unmarshal([]byte(args), &input)
				if toolActions[input["action"].(string)].view != "" && !strings.Contains(strings.Join(p.names, ","), "robotlab.focus") {
					t.Fatal("missing focus")
				}
			})
		}
	}
}
func TestToolValidationAndUnavailable(t *testing.T) {
	for _, args := range []string{`{}`, `{"action":"open"}`, `{"action":"reset","session":"s","code":"bad"}`, `{"action":"load_policy","session":"s"}`, `{"action":"write_code","experiment":"e","code":null}`, `{"action":"control","session":"s","mode":"bad"}`, `{"action":"bogus"}`, `{"action":"open","scenario":"bad"}`, `{"action":"snapshot","session":"s","width":0}`} {
		r, _ := (&Tool{}).Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(args)})
		if r.Status != tools.StatusInputError {
			t.Fatalf("%s: %+v", args, r)
		}
	}
	for _, disabled := range []bool{false, true} {
		t.Run(string(rune('a'+boolInt(disabled))), func(t *testing.T) {
			if disabled {
				t.Setenv("EXCALIBUR_ROBOTLAB", "off")
			}
			r, _ := (&Tool{}).Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"scenarios"}`)})
			if !strings.Contains(r.Output, "unavailable") || !strings.Contains(r.Output, "Install") {
				t.Fatal(r)
			}
		})
	}
}
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestToolPropagatesCancellationAndTimeout(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "timeout"}[timeout], func(t *testing.T) {
			var ctx context.Context
			var cancel context.CancelFunc
			if timeout {
				ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
			} else {
				ctx, cancel = context.WithCancel(context.Background())
				cancel()
			}
			defer cancel()
			sidecar := &toolSidecarFake{call: func(ctx context.Context, _ string, _ any) (json.RawMessage, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			}}
			result, err := (&Tool{Sidecar: sidecar}).Invoke(ctx, tools.Invocation{Arguments: json.RawMessage(`{"action":"scenarios"}`)})
			expected := tools.StatusCanceled
			if timeout {
				expected = tools.StatusTimeout
			}
			if err != nil || result.Status != expected {
				t.Fatalf("%+v %v", result, err)
			}
		})
	}
}

func TestMetricsSummaryPreservesRunBoundaries(t *testing.T) {
	var input map[string]any
	if err := json.Unmarshal([]byte(`{"points":[{"run":"a","step":1,"episode_return":2},{"run":"b","step":4,"loss":0.2},{"run":"a","step":5,"episode_return":9},{"run":"a","step":8,"episode_return":3}],"next":4}`), &input); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(summarizeMetrics(input))
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Runs []struct {
			Run   string   `json:"run"`
			Steps int      `json:"steps"`
			Last  *float64 `json:"last_return"`
			Best  *float64 `json:"best_return"`
			Trend string   `json:"trend"`
		}
		Next int
	}
	if err := json.Unmarshal(raw, &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Runs) != 2 || output.Next != 4 {
		t.Fatal(string(raw))
	}
	a, b := output.Runs[0], output.Runs[1]
	if a.Run != "a" || a.Steps != 8 || a.Last == nil || *a.Last != 3 || a.Best == nil || *a.Best != 9 || a.Trend != "improving" {
		t.Fatal(string(raw))
	}
	if b.Run != "b" || b.Steps != 4 || b.Last != nil || b.Best != nil || b.Trend != "unknown" {
		t.Fatal(string(raw))
	}
}

func TestToolUnavailableAfterLazyStartFailure(t *testing.T) {
	sidecar := &toolSidecarFake{call: func(context.Context, string, any) (json.RawMessage, error) {
		return nil, fmt.Errorf("sidecar failed to start")
	}}
	result, err := (&Tool{Sidecar: sidecar}).Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"scenarios"}`)})
	if err != nil || result.Status != tools.StatusToolError || !strings.Contains(result.Output, `"status":"unavailable"`) || !strings.Contains(result.Output, "Install") {
		t.Fatalf("%+v %v", result, err)
	}
}

// Status without an experiment lists stored experiments even when the runtime is idle.
func TestStatusListsExperiments(t *testing.T) {
	side := &toolSidecarFake{call: func(_ context.Context, m string, _ any) (json.RawMessage, error) {
		if m != "experiment.list" {
			return nil, fmt.Errorf("unexpected %s", m)
		}
		return json.RawMessage(`{"experiments":[{"id":"exp_1","name":"PPO vs A2C","scenario":"cartpole","status":"done","runs":[{"id":"run_1","label":"A2C","status":"done","best_return":993.7}]}]}`), nil
	}}
	tool := &Tool{Sidecar: side, Publisher: &fakePublisher{}}
	result, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"status"}`)})
	if err != nil || result.Status != tools.StatusOK {
		t.Fatalf("status: %v %+v", err, result)
	}
	for _, want := range []string{`"runtime"`, `"exp_1"`, `"done"`, `993.7`} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("status output lacks %s: %s", want, result.Output)
		}
	}
}
