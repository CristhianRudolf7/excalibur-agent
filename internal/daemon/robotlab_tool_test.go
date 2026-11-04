package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/robotlab"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

type labSidecar struct{ methods []string }

func (s *labSidecar) Call(_ context.Context, method string, _ any) (json.RawMessage, error) {
	s.methods = append(s.methods, method)
	switch method {
	case "robot.look", "env.preview":
		return json.RawMessage(`{"png":"cG5n","visible":[],"state":{}}`), nil
	case "media.get":
		return json.RawMessage(`{"media_id":"m","spec":"A room","frames":["cG5n"]}`), nil
	}
	return json.RawMessage(`{"session":{"id":"dog-room-abcd","status":"live"},"ok":true}`), nil
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
				{`{"action":"robots"}`, "robots.list"},
				{`{"action":"environments"}`, "env.list"},
				{`{"action":"environment_get","id":"room"}`, "env.get"},
				{`{"action":"environment_create","environment":{"version":1,"id":"room","name":"Room","kind":"room","objects":[]}}`, "env.save"},
				{`{"action":"environment_update","environment":{"version":1,"id":"room","name":"Room","kind":"room","objects":[]}}`, "env.save"},
				{`{"action":"environment_delete","id":"room"}`, "env.delete"},
				{`{"action":"environment_preview","id":"room"}`, "env.preview"},
				{`{"action":"media","media_id":"m"}`, "media.get"},
				{`{"action":"session_start","robot":"dog","environment":"room","task":"Hold ball","goal":{"type":"object_held","object":"ball"}}`, "session.start"},
				{`{"action":"session_status","session":"s"}`, "session.get"},
				{`{"action":"session_stop","session":"s"}`, "session.stop"},
				{`{"action":"attempt_reset","session":"s","reflection":"Try shorter steps"}`, "attempt.reset"},
				{`{"action":"act","session":"s","tool":"walk","args":{}}`, "robot.act"},
				{`{"action":"look","session":"s"}`, "robot.look"},
				{`{"action":"skills","session":"s"}`, "skills.list"},
				{`{"action":"skill_read","session":"s","name":"walk"}`, "skills.read"},
				{`{"action":"skill_write","session":"s","name":"walk","description":"Walk","instructions":"Short steps"}`, "skills.write"},
				{`{"action":"skill_use","session":"s","name":"walk"}`, "skills.use"},
				{`{"action":"skill_run","session":"s","name":"walk"}`, "skills.run"},
				{`{"action":"skills_copy","from":"old","to":"s"}`, "skills.copy"},
				{`{"action":"note","session":"s","text":"Approach slowly","kind":"plan"}`, "log.note"},
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
					if strings.Contains(r.Output, "hidden") || strings.Contains(r.Output, "png") {
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
				r, err := e.Exec.Execute(context.Background(), "call_tool", json.RawMessage(`{"name":"robot_lab","arguments":{"action":"robots"}}`), tools.ExecuteOptions{})
				if err == nil && r.Status == tools.StatusOK {
					t.Fatal("eval invoked Robot Lab")
				}
			} else {
				if err != nil {
					t.Fatal("disabled tool vanished")
				}
				r, _ := e.Exec.Execute(context.Background(), "robot_lab", json.RawMessage(`{"action":"robots"}`), tools.ExecuteOptions{})
				if !strings.Contains(r.Output, "unavailable") {
					t.Fatal(r)
				}
			}
		})
	}
}

func TestRobotLabAssembledToolSharesService(t *testing.T) {
	runtime := &labSidecar{}
	service := robotlab.NewService(runtime, nil)
	defer service.Close()
	services, err := Assemble(AssembleOptions{RobotLab: service})
	if err != nil {
		t.Fatal(err)
	}
	if services.RobotLabTool.Sidecar != runtime || services.RobotLabTool.Publisher != service {
		t.Fatal("tool and service not shared")
	}
	if _, err = service.Op(context.Background(), "robotlab.vision.step", json.RawMessage(`{"session":"s","instruction":"sort"}`)); err == nil {
		t.Fatal("retired RPC remained callable")
	}
}

type labTrainingSidecar struct {
	mu      sync.Mutex
	session robotlab.SessionState
}

func (f *labTrainingSidecar) Call(_ context.Context, method string, _ any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch method {
	case "status":
		var live any
		if f.session.Status == "live" {
			live = f.session.ID
		}
		raw, _ := json.Marshal(map[string]any{"live_session": live, "robots": 4, "environments": 3})
		return raw, nil
	case "session.get":
		raw, _ := json.Marshal(map[string]any{"session": f.session})
		return raw, nil
	default:
		return json.RawMessage(`{"png":"cG5n","visible":[],"state":{}}`), nil
	}
}
func (*labTrainingSidecar) Status(context.Context) robotlab.Status {
	return robotlab.Status{Enabled: true, Installed: true, Running: true}
}
func (*labTrainingSidecar) Subscribe(func(robotlab.Event)) func() { return func() {} }
func (f *labTrainingSidecar) setStatus(status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.session.Status = status
}

type labRelay struct{ receipt, conversation, text, by string }
type labRelayMaster struct {
	mu       sync.Mutex
	relays   []labRelay
	history  []master.Normalized
	received chan labRelay
	fail     bool
}

func (f *labRelayMaster) ReceiveRelay(_ context.Context, receipt, conversation, text, by string) (types.MessageID, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return "", false, fmt.Errorf("relay failed")
	}
	relay := labRelay{receipt, conversation, text, by}
	f.relays = append(f.relays, relay)
	f.history = append(f.history, master.Normalized{RelayedBy: by, Text: text})
	if f.received != nil {
		f.received <- relay
	}
	return "msg-test", false, nil
}
func (f *labRelayMaster) TurnHistory(string, int) []master.Normalized {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]master.Normalized(nil), f.history...)
}
func (f *labRelayMaster) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.relays) }
func labContinuationFixture(t *testing.T) (*labTrainingSidecar, *labRelayMaster, *robotLabContinuations) {
	t.Helper()
	t.Setenv("EXCALIBUR_ROBOTLAB", "1")
	runtime := &labTrainingSidecar{session: robotlab.SessionState{ID: "dog-room-abcd", Conversation: "c", Status: "live", Attempt: 2, MaxAttempts: 2}}
	service := robotlab.NewService(runtime, nil)
	t.Cleanup(func() { _ = service.Close() })
	relay := &labRelayMaster{received: make(chan labRelay, 32)}
	continuation := newRobotLabContinuations(service, relay, func(string) bool { return false })
	t.Cleanup(continuation.stop)
	return runtime, relay, continuation
}
func awaitLabRelay(t *testing.T, m *labRelayMaster) labRelay {
	t.Helper()
	select {
	case relay := <-m.received:
		return relay
	case <-time.After(3 * time.Second):
		t.Fatal("continuation not delivered")
		return labRelay{}
	}
}
func TestRobotLabContinuationAfterTurnEnds(t *testing.T) {
	_, m, c := labContinuationFixture(t)
	turn, finish := context.WithCancel(context.Background())
	defer finish()
	c.queue(turn, "c", true)
	c.mu.Lock()
	queued := c.pending["c"]
	c.mu.Unlock()
	if !queued || m.count() != 0 {
		t.Fatal("relayed before turn ended")
	}
	finish()
	relay := awaitLabRelay(t, m)
	if relay.receipt != "robotlab:dog-room-abcd:1" || relay.by != "robot-lab" || relay.conversation != "c" || relay.text != "Continue training session dog-room-abcd: attempt 2 of 2, status live. Keep going until the goal is met or the attempt limit is reached. Remember: write or improve a skill after each failed attempt." {
		t.Fatalf("%+v", relay)
	}
}
func TestRobotLabContinuationConditions(t *testing.T) {
	for _, mode := range []string{"no-call", "other-conversation", "stopped", "succeeded", "exhausted", "failed", "restart", "shutdown", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			runtime, m, c := labContinuationFixture(t)
			switch mode {
			case "stopped", "succeeded", "exhausted", "failed":
				runtime.setStatus(mode)
			case "restart":
				c.active = func(string) bool { return true }
			case "shutdown":
				c.stop()
			case "disabled":
				t.Setenv("EXCALIBUR_ROBOTLAB", "off")
			}
			turn, finish := context.WithCancel(context.Background())
			conversation := "c"
			if mode == "other-conversation" {
				conversation = "other"
			}
			c.queue(turn, conversation, mode != "no-call")
			finish()
			c.wg.Wait()
			if m.count() != 0 {
				t.Fatal("unexpected relay: " + mode)
			}
		})
	}
}
func TestRobotLabContinuationCapPersistsAndErrorsDoNotConsume(t *testing.T) {
	_, m, c := labContinuationFixture(t)
	m.fail = true
	c.send(context.Background(), "c")
	m.fail = false
	for range 7 {
		c.send(context.Background(), "c")
	}
	if m.count() != 6 {
		t.Fatal(m.count())
	}
	for i, relay := range m.relays {
		if relay.receipt != fmt.Sprintf("robotlab:dog-room-abcd:%d", i+1) {
			t.Fatal(relay)
		}
	}
	restarted := newRobotLabContinuations(c.service, m, nil)
	defer restarted.stop()
	restarted.send(context.Background(), "c")
	if m.count() != 6 {
		t.Fatal("restart reset continuation cap")
	}
}
func TestRobotLabTurnBudgets(t *testing.T) {
	_, _, c := labContinuationFixture(t)
	for _, tc := range []struct {
		name, conversation, stepsEnv string
		defs                         []tools.Definition
		wantSteps                    int
		wantTimeout                  time.Duration
	}{
		{"active", "other", "", []tools.Definition{{Name: "robot_lab"}}, 48, 15 * time.Minute},
		{"owned", "c", "", nil, 48, 15 * time.Minute},
		{"other", "other", "", nil, 12, 2 * time.Minute},
		{"explicit", "c", "20", nil, 12, 2 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("EXCALIBUR_MASTER_STEPS", tc.stepsEnv)
			steps, timeout := robotLabTurnLimits(context.Background(), c.service, tc.conversation, tc.defs, 12, 2*time.Minute)
			if steps != tc.wantSteps || timeout != tc.wantTimeout {
				t.Fatalf("%d %v", steps, timeout)
			}
		})
	}
	t.Setenv("EXCALIBUR_MASTER_STEPS", "")
	steps, timeout := robotLabTurnLimits(context.Background(), c.service, "c", nil, 12, 20*time.Minute)
	if steps != 48 || timeout != 20*time.Minute {
		t.Fatal(steps, timeout)
	}
	t.Setenv("EXCALIBUR_ROBOTLAB", "off")
	steps, timeout = robotLabTurnLimits(context.Background(), c.service, "c", []tools.Definition{{Name: "robot_lab"}}, 12, 2*time.Minute)
	if steps != 12 || timeout != 2*time.Minute {
		t.Fatal(steps, timeout)
	}
}
func TestRobotLabMasterTurnUsesBudgetAndContinues(t *testing.T) {
	t.Setenv("EXCALIBUR_MASTER_STEPS", "")
	t.Setenv("EXCALIBUR_MASTER_TIMEOUT", "30s")
	runtime, m, c := labContinuationFixture(t)
	b, s := assistantTestWiring(t, false, nil)
	s.robotLabContinue.stop()
	s.robotLabContinue = c
	s.RobotLab = c.service
	s.Tools = tools.NewMapRegistry()
	s.Exec = tools.NewExecutor(s.Tools)
	if err := s.Tools.Register(&robotlab.Tool{Sidecar: runtime}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	a := &assistantTurns{backend: b, services: s, modelID: "test", endpoint: timingEndpoint{chat: func(ctx context.Context, _ providers.ChatRequest) (providers.ChatResponse, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) < 14*time.Minute {
			t.Error("training timeout not extended")
		}
		calls++
		if calls <= 14 {
			return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: fmt.Sprint(calls), Name: "robot_lab", Arguments: json.RawMessage(`{"action":"look","session":"dog-room-abcd"}`)}}}}, nil
		}
		return textReply("Training continues"), nil
	}}}
	turn, finish := context.WithCancel(context.Background())
	defer finish()
	text, _, err := a.CompleteTurn(turn, "c", master.Normalized{Text: "Continue training"})
	if err != nil || text != "Training continues" || calls != 15 {
		t.Fatalf("%s %d %v", text, calls, err)
	}
	if m.count() != 0 {
		t.Fatal("relay before turn finished")
	}
	finish()
	awaitLabRelay(t, m)
}

func TestRobotLabWebTrainingIntentGetsTrainingBudget(t *testing.T) {
	t.Setenv("EXCALIBUR_MASTER_STEPS", "")
	services, err := Assemble(AssembleOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer services.RobotLab.Close()
	for _, request := range []string{"Realiza una simulación con Perro con brazo en el escenario mi-cuarto: trae la pelota", "Run a simulation with dog in mi-cuarto: bring the ball"} {
		activation := services.chatToolActivation(request, "test", nil)
		steps, timeout := robotLabTurnLimits(context.Background(), services.RobotLab, "new", activation.Definitions, 12, 2*time.Minute)
		if steps != 48 || timeout < 15*time.Minute {
			t.Fatalf("training request did not activate robot_lab: %q", request)
		}
	}
}
