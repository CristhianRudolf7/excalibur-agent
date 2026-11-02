package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/automations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

type automationModel struct {
	prompt string
	cancel bool
	fail   bool
}

func (m *automationModel) Chat(ctx context.Context, _ types.ModelRole, r providers.ChatRequest) (providers.ChatResponse, models.CallMeta, error) {
	raw, _ := json.Marshal(r)
	m.prompt = string(raw)
	if m.cancel {
		<-ctx.Done()
		return providers.ChatResponse{}, models.CallMeta{}, ctx.Err()
	}
	if m.fail {
		return providers.ChatResponse{}, models.CallMeta{}, errors.New("model failed")
	}
	return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, Content: "Final researched answer"}}, models.CallMeta{}, nil
}
func TestAutomationAgentRunner(t *testing.T) {
	reg := tools.NewMapRegistry()
	svc := &Services{Tools: reg, Exec: tools.NewExecutor(reg)}
	model := &automationModel{}
	runner := &AutomationAgentRunner{Services: svc, Endpoint: model, ModelID: "fake"}
	out, e := runner.RunAgentStep(context.Background(), automations.Automation{Name: "Daily research"}, automations.Step{Instruction: "Find recent news"}, map[string]string{"s1": "prior result"})
	if e != nil || out != "Final researched answer" {
		t.Fatal(out, e)
	}
	for _, text := range []string{"Daily research", "Find recent news", "prior result", "unattended"} {
		if !strings.Contains(model.prompt, text) {
			t.Fatal("missing prompt context", text)
		}
	}
	model.fail = true
	if _, e = runner.RunAgentStep(context.Background(), automations.Automation{}, automations.Step{}, nil); e == nil {
		t.Fatal("model failure swallowed")
	}
	model.fail = false
	model.cancel = true
	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer timeoutCancel()
	if _, e = runner.RunAgentStep(timeoutCtx, automations.Automation{}, automations.Step{}, nil); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = runner.RunAgentStep(ctx, automations.Automation{}, automations.Step{}, nil); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestAutomationToolInvoker(t *testing.T) {
	reg := tools.NewMapRegistry()
	calls := 0
	def := automations.ToolDefinition()
	def.Name = "test_tool"
	def.InputSchema = json.RawMessage(`{"type":"object"}`)
	_ = reg.Register(&tools.StubTool{Def: def, Provider: func(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
		calls++
		if inv.IdempotencyKey == "" {
			t.Error("missing key")
		}
		if ctx.Err() != nil {
			return tools.Result{}, ctx.Err()
		}
		return tools.Result{Status: tools.StatusOK, Output: "real output"}, nil
	}})
	runner := &AutomationToolInvoker{Registry: reg, Executor: tools.NewExecutor(reg)}
	out, e := runner.InvokeTool(context.Background(), "test_tool", map[string]any{"query": "news"})
	if e != nil || out != "real output" || calls != 1 {
		t.Fatal(out, e, calls)
	}
	for _, name := range []string{"automations", "ask_user", "subagent", "call_tool", "unknown"} {
		if _, e = runner.InvokeTool(context.Background(), name, nil); e == nil {
			t.Fatal("accepted", name)
		}
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}
func TestAutomationNotifierPersistsAndPublishes(t *testing.T) {
	m := master.New(master.Config{}, master.NewMemStore(), nil, nil, nil, nil, nil)
	var event api.MessageEvent
	m.SetAssistantOutput(master.AssistantOutput{Publish: func(e api.MessageEvent) { event = e }})
	n := &AutomationNotifier{Master: m}
	if e := n.Notify(context.Background(), "Automation completed"); e != nil {
		t.Fatal(e)
	}
	history := m.History("cnv_web_excalibur", 20)
	if len(history) != 1 || history[0].Text != "Automation completed" || event.Text != history[0].Text || event.ConversationID != "cnv_web_excalibur" {
		t.Fatal(history, event)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := n.Notify(ctx, "cancelled"); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if len(m.History("cnv_web_excalibur", 20)) != 1 {
		t.Fatal("cancelled notice persisted")
	}
}

func TestAutomationCatalogKeepsEvalSurface(t *testing.T) {
	emb, e := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{Profile: "eval"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = emb.Registry.Get("automations"); e == nil {
		t.Fatal("eval gained automations")
	}
	defaultCatalog, e := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = defaultCatalog.Registry.Get("automations"); e != nil {
		t.Fatal(e)
	}
}
