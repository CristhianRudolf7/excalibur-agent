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
	"github.com/CristhianRudolf7/excalibur-agent/internal/whiteboard"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

type labSidecar struct{ methods []string }

func (s *labSidecar) Call(_ context.Context, method string, _ any) (json.RawMessage, error) {
	s.methods = append(s.methods, method)
	switch method {
	case "robot.look", "env.preview":
		return json.RawMessage(`{"png":"cG5n","visible":[],"state":{}}`), nil

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
				{`{"action":"session_start","robot":"dog","environment":"room","task":"Hold ball","goal":{"type":"object_held","object":"ball"}}`, "session.start"},
				{`{"action":"sessions","robot":"dog","query":"stairs"}`, "session.list"},
				{`{"action":"session_resume","session":"s"}`, "session.resume"},
				{`{"action":"session_status","session":"s"}`, "session.get"},
				{`{"action":"session_stop","session":"s"}`, "session.stop"},
				{`{"action":"attempt_reset","session":"s","reflection":"Try shorter steps"}`, "attempt.reset"},
				{`{"action":"act","session":"s","tool":"move_joints","args":{"joints":{"FL_hip":0},"duration":0.5}}`, "robot.act"},
				{`{"action":"look","session":"s"}`, "robot.look"},
				{`{"action":"skills","session":"s"}`, "skills.list"},
				{`{"action":"skill_read","session":"s","name":"walk"}`, "skills.read"},
				{`{"action":"skill_write","session":"s","name":"walk","description":"Walk","instructions":"Short steps"}`, "skills.write"},
				{`{"action":"skill_use","session":"s","name":"walk"}`, "skills.use"},
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
	mu           sync.Mutex
	session      robotlab.SessionState
	listeners    map[int]func(robotlab.Event)
	nextListener int
}

func (f *labTrainingSidecar) Call(_ context.Context, method string, params any) (json.RawMessage, error) {
	f.mu.Lock()
	stopped := false
	defer func() {
		f.mu.Unlock()
		if stopped {
			f.emitSession()
		}
	}()
	switch method {
	case "status":
		var live any
		if f.session.Status == "live" {
			live = f.session.ID
		}
		raw, _ := json.Marshal(map[string]any{"live_session": live, "robots": 4, "environments": 3})
		return raw, nil
	case "session.resume":
		raw, _ := json.Marshal(params)
		var p struct{ Session, Conversation string }
		_ = json.Unmarshal(raw, &p)
		f.session.ID, f.session.Conversation, f.session.Status = p.Session, p.Conversation, "live"
		f.session.Attempt++
		f.session.MaxAttempts = f.session.Attempt - 1 + 100
		raw, _ = json.Marshal(map[string]any{"session": f.session})
		return raw, nil
	case "session.get":
		raw, _ := json.Marshal(map[string]any{"session": f.session})
		return raw, nil
	case "session.stop":
		f.session.Status = "stopped"
		stopped = true
		raw, _ := json.Marshal(map[string]any{"session": f.session})
		return raw, nil
	default:
		return json.RawMessage(`{"png":"cG5n","visible":[],"state":{}}`), nil
	}
}
func (*labTrainingSidecar) Status(context.Context) robotlab.Status {
	return robotlab.Status{Enabled: true, Installed: true, Running: true}
}
func (f *labTrainingSidecar) Subscribe(fn func(robotlab.Event)) func() {
	f.mu.Lock()
	if f.listeners == nil {
		f.listeners = map[int]func(robotlab.Event){}
	}
	f.nextListener++
	id := f.nextListener
	f.listeners[id] = fn
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		delete(f.listeners, id)
		f.mu.Unlock()
	}
}
func (f *labTrainingSidecar) emitSession() {
	f.mu.Lock()
	raw, _ := json.Marshal(map[string]any{"session": f.session})
	listeners := make([]func(robotlab.Event), 0, len(f.listeners))
	for _, fn := range f.listeners {
		listeners = append(listeners, fn)
	}
	f.mu.Unlock()
	for _, fn := range listeners {
		fn(robotlab.Event{Name: "session", Data: raw})
	}
}
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

func (f *labRelayMaster) ReceiveRelay(ctx context.Context, receipt, conversation, text, by string) (types.MessageID, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
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
	if queued == nil || m.count() != 0 {
		t.Fatal("relayed before turn ended")
	}
	finish()
	relay := awaitLabRelay(t, m)
	if relay.receipt != "robotlab:dog-room-abcd:1" || relay.by != "robot-lab" || relay.conversation != "c" {
		t.Fatalf("%+v", relay)
	}
	for _, phrase := range []string{"Continue training session dog-room-abcd: attempt 2 of 2, status live.", "robot card", "markdown skills", "check with ik", "4–12 keyframes", "state after every call", "support, margin and terrain", "failed or abandoned", "lesson and exact keyframes", "failures=[N]", "then attempt_reset", "no_skill_reason", "goal.met=true"} {
		if !strings.Contains(relay.text, phrase) {
			t.Fatalf("missing continuation guidance %q: %s", phrase, relay.text)
		}
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

func TestRobotLabContinuationNewUserMessageWins(t *testing.T) {
	for _, when := range []string{"before-queue", "after-queue", "drained-input"} {
		t.Run(when, func(t *testing.T) {
			_, m, c := labContinuationFixture(t)
			turn, finish := context.WithCancel(context.Background())
			defer finish()
			robotLabTurnLimits(turn, c.service, "c", nil, 12, time.Minute)
			started := c.service.TurnStartedAt(turn, "c")
			if started.IsZero() {
				t.Fatal("turn start was not recorded")
			}
			if when == "after-queue" {
				c.queue(turn, "c", true)
			}
			m.mu.Lock()
			m.history = append(m.history, master.Normalized{Role: "user", Text: "Retry the arm with the box and shelf", CreatedAt: time.Now()})
			m.mu.Unlock()
			if when == "drained-input" {
				// The turn manager may drain another input with the same context.
				robotLabTurnLimits(turn, c.service, "c", nil, 12, time.Minute)
				if c.service.TurnStartedAt(turn, "c") != started {
					t.Fatal("draining input reset the parent turn start")
				}
			}
			if when != "after-queue" {
				c.queue(turn, "c", true)
			}
			finish()
			c.wg.Wait()
			if m.count() != 0 {
				t.Fatal("continuation overrode newer user input")
			}
			// Suppressing this turn must not consume a receipt or block a later turn.
			next, end := context.WithCancel(context.Background())
			defer end()
			robotLabTurnLimits(next, c.service, "c", nil, 12, time.Minute)
			c.queue(next, "c", true)
			end()
			if got := awaitLabRelay(t, m).receipt; got != "robotlab:dog-room-abcd:1" {
				t.Fatal(got)
			}
		})
	}
}

func TestRobotLabContinuationIgnoresOlderUserAndNewAssistantOrRelay(t *testing.T) {
	_, m, c := labContinuationFixture(t)
	turn, finish := context.WithCancel(context.Background())
	defer finish()
	robotLabTurnLimits(turn, c.service, "c", nil, 12, time.Minute)
	started := c.service.TurnStartedAt(turn, "c")
	m.history = []master.Normalized{
		{Role: "user", CreatedAt: started.Add(-time.Millisecond)},
		{Role: "assistant", CreatedAt: started.Add(time.Millisecond)},
		{Role: "user", RelayedBy: "robot-lab", CreatedAt: started.Add(time.Millisecond)},
	}
	c.queue(turn, "c", true)
	finish()
	awaitLabRelay(t, m)
}

func TestRobotLabContinuationStaysBoundToQueuedSession(t *testing.T) {
	for _, mode := range []string{"non-live", "non-current", "owner-changed"} {
		t.Run(mode, func(t *testing.T) {
			runtime, m, c := labContinuationFixture(t)
			turn, finish := context.WithCancel(context.Background())
			defer finish()
			c.queue(turn, "c", true)
			// No event: the final runtime recheck must still prevent a relay.
			runtime.mu.Lock()
			switch mode {
			case "non-live":
				runtime.session.Status = "stopped"
			case "non-current":
				runtime.session.ID = "arm-shelf"
			case "owner-changed":
				runtime.session.Conversation = "other"
			}
			runtime.mu.Unlock()
			finish()
			c.wg.Wait()
			if m.count() != 0 {
				t.Fatal("stale continuation delivered")
			}
		})
	}
}

func TestRobotLabStopCancelsPendingContinuationBeforeTurnEnds(t *testing.T) {
	for _, by := range []string{"agent", "user"} {
		t.Run(by, func(t *testing.T) {
			runtime, m, c := labContinuationFixture(t)
			turn, finish := context.WithCancel(context.Background())
			defer finish()
			c.queue(turn, "c", true)
			c.mu.Lock()
			pending := c.pending["c"]
			c.mu.Unlock()
			if pending == nil {
				t.Fatal("continuation was not queued")
			}
			if by == "agent" {
				tool := robotlab.NewTool(runtime, c.service, nil, nil)
				result, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"session_stop","session":"dog-room-abcd"}`)})
				if err != nil || result.Status != tools.StatusOK {
					t.Fatalf("stop: %+v %v", result, err)
				}
			} else if _, err := c.service.Op(context.Background(), "robotlab.session.stop", json.RawMessage(`{"session":"dog-room-abcd","by":"user"}`)); err != nil {
				t.Fatal(err)
			}
			if pending.ctx.Err() == nil {
				t.Fatalf("%s stop did not cancel the pending continuation", by)
			}
			c.wg.Wait() // Must finish while the triggering turn remains open.
			c.mu.Lock()
			queued := c.pending["c"]
			c.mu.Unlock()
			if queued != nil || m.count() != 0 {
				t.Fatal("stopped session kept a continuation")
			}
			// Resuming the same ID must not resurrect work from the old turn.
			runtime.setStatus("live")
			finish()
			if m.count() != 0 {
				t.Fatal("old continuation survived stop/resume")
			}
		})
	}
}

func TestRobotLabContinuationCapPersistsAndErrorsDoNotConsume(t *testing.T) {
	_, m, c := labContinuationFixture(t)
	m.fail = true
	c.send(context.Background(), "c", "dog-room-abcd", time.Time{})
	m.fail = false
	for range 7 {
		c.send(context.Background(), "c", "dog-room-abcd", time.Time{})
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
	restarted.send(context.Background(), "c", "dog-room-abcd", time.Time{})
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
		{"active", "other", "", []tools.Definition{{Name: "robot_lab"}}, 120, 30 * time.Minute},
		{"owned", "c", "", nil, 120, 30 * time.Minute},
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
	steps, timeout := robotLabTurnLimits(context.Background(), c.service, "c", nil, 12, 45*time.Minute)
	if steps != 120 || timeout != 45*time.Minute {
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
		if !ok || time.Until(deadline) < 29*time.Minute {
			t.Error("training timeout not extended")
		}
		calls++
		if calls <= 50 {
			return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: fmt.Sprint(calls), Name: "robot_lab", Arguments: json.RawMessage(`{"action":"look","session":"dog-room-abcd"}`)}}}}, nil
		}
		return textReply("Training continues"), nil
	}}}
	turn, finish := context.WithCancel(context.Background())
	defer finish()
	text, _, err := a.CompleteTurn(turn, "c", master.Normalized{Text: "Continue training"})
	if err != nil || text != "Training continues" || calls != 51 {
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
	for _, request := range []string{"Realiza una simulación con Perro en escalera-estrecha: sube al descanso", "Run a simulation with dog in escalera-estrecha: climb onto the landing"} {
		activation := services.chatToolActivation(request, "test", nil)
		steps, timeout := robotLabTurnLimits(context.Background(), services.RobotLab, "new", activation.Definitions, 12, 2*time.Minute)
		if steps != 120 || timeout < 30*time.Minute {
			t.Fatalf("training request did not activate robot_lab: %q", request)
		}
	}
}

func TestRobotLabResumeTransfersContinuationOwnershipAndExtendsBudget(t *testing.T) {
	runtime, relay, c := labContinuationFixture(t)
	runtime.mu.Lock()
	runtime.session.Status, runtime.session.Conversation = "stopped", "old-chat"
	runtime.session.Attempt, runtime.session.MaxAttempts = 100, 100
	runtime.mu.Unlock()
	tool := robotlab.NewTool(runtime, c.service, nil, nil)
	result, err := tool.Invoke(whiteboard.WithConversation(context.Background(), "new-chat"), tools.Invocation{Arguments: json.RawMessage(`{"action":"session_resume","session":"dog-room-abcd"}`)})
	if err != nil || result.Status != tools.StatusOK {
		t.Fatalf("%+v %v", result, err)
	}
	c.send(context.Background(), "old-chat", "dog-room-abcd", time.Time{})
	if relay.count() != 0 {
		t.Fatal("previous conversation kept ownership")
	}
	turn, finish := context.WithCancel(context.Background())
	c.queue(turn, "new-chat", true)
	finish()
	message := awaitLabRelay(t, relay)
	if message.conversation != "new-chat" || !strings.Contains(message.text, "attempt 101 of 200") {
		t.Fatalf("%+v", message)
	}
	steps, timeout := robotLabTurnLimits(context.Background(), c.service, "new-chat", nil, 12, time.Minute)
	if steps != 120 || timeout < 30*time.Minute {
		t.Fatal(steps, timeout)
	}
}
