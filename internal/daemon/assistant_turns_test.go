package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

type turnProvider struct {
	*fake.FakeProvider
	chat func(context.Context, providers.ChatRequest) (providers.ChatResponse, error)
}

func (p *turnProvider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	return p.chat(ctx, req)
}

func assistantTestWiring(t *testing.T, library bool, chat func(context.Context, providers.ChatRequest) (providers.ChatResponse, error), configs ...master.Config) (*DaemonBackend, *Services) {
	t.Helper()
	var b *DaemonBackend
	if library {
		b = citationTestBackend(t)
	} else {
		b = NewDaemonBackend(master.New(master.Config{}, nil, nil, nil, nil, nil, nil), &OrchControl{Store: orchestrator.NewMemoryStore()}, telemetry.NewBus(nil))
		b.AttachServices(BackendServices{})
	}
	if len(configs) > 0 {
		b.Master = master.New(configs[0], nil, nil, nil, nil, nil, nil)
		b.configureAssistantOutput()
	}
	var endpoint engine.ModelEndpoint
	if chat != nil {
		g := models.NewGateway(models.GatewayConfig{MaxAttempts: 1, DisableBudgetRetries: true, Roles: map[types.ModelRole]models.RoleEntry{types.RoleInterlocutor: {Primary: models.ModelRef{Provider: "assistant-test", Model: "test"}}}})
		g.Register(&turnProvider{FakeProvider: fake.New("assistant-test"), chat: chat})
		t.Cleanup(func() { _ = g.Close() })
		endpoint = g
	}
	s, err := Assemble(AssembleOptions{Memory: b.Services.Memory, Models: endpoint})
	if err != nil {
		t.Fatal(err)
	}
	WireAssistantTurns(b, s, endpoint, "test")
	t.Cleanup(func() {
		s.delegations.stop()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = b.Master.Turns().Stop(ctx)
	})
	return b, s
}
func awaitAssistant(t *testing.T, b *DaemonBackend, conv string, n int) []master.HistoryMessage {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !b.Master.Turns().Active(conv) {
			history := b.Master.History(conv, 0)
			count := 0
			for _, m := range history {
				if m.Role == "assistant" {
					count++
				}
			}
			if count >= n {
				return history
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("assistant did not finish: %+v", b.Master.History(conv, 0))
	return nil
}
func textReply(text string) providers.ChatResponse {
	return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, Content: text}}
}

func TestEngineAssistantSearchCitations(t *testing.T) {
	t.Setenv("EXCALIBUR_MASTER_ENGINE_TURNS", "1")
	t.Setenv("EXCALIBUR_CITATIONS_PIPELINE", "on")
	calls := 0
	var ref string
	b, _ := assistantTestWiring(t, true, func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		calls++
		if calls == 1 {
			names := map[string]bool{}
			for _, d := range req.Tools {
				names[d.Name] = true
			}
			for _, name := range []string{"library_search", "library_list", "library_view", "library_grep", "library_status", "doc_read", "clock"} {
				if !names[name] {
					t.Errorf("missing master tool %s", name)
				}
			}
			for _, name := range []string{"exec", "write_file", "call_tool", "search_tools", "document_create"} {
				if names[name] {
					t.Errorf("unsafe master tool %s", name)
				}
			}
			var all strings.Builder
			for _, m := range req.Messages {
				all.WriteString(m.Content)
			}
			if !strings.Contains(all.String(), "status=ready") || !strings.Contains(all.String(), "Field guide") || !strings.Contains(all.String(), "Prefers concise bird explanations.") {
				t.Errorf("missing attachment metadata: %s", all.String())
			}
			return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "search-1", Name: "library_search", Arguments: json.RawMessage(`{"query":"robin nests","k":1,"run_id":"forged"}`)}}}}, nil
		}
		found := false
		for _, m := range req.Messages {
			if m.ToolResult != nil && strings.Contains(m.ToolResult.Output, ref) {
				found = true
			}
		}
		if !found {
			t.Error("model did not receive retrieved citation")
		}
		return textReply("Robins nest in hedges " + ref + "."), nil
	})
	ctx := context.Background()
	doc, err := b.Services.Memory.Ingest(ctx, []string{"general"}, "guide.txt", "text/plain", []byte("The robin nests in spring hedges."), memory.IngestOptions{Title: "Field guide"})
	if err != nil {
		t.Fatal(err)
	}
	hits, _, err := b.Services.Memory.LibrarySearch(ctx, nil, "fixture", "robin nests", 1)
	if err != nil || len(hits) == 0 {
		t.Fatalf("fixture %v %+v", err, hits)
	}
	profile, err := b.Services.Memory.ResolveDoc(ctx, "general", memory.DocUser)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Services.Memory.AppendFragment(ctx, nil, profile, "preferences", "Prefers concise bird explanations.", ""); err != nil {
		t.Fatal(err)
	}
	ref = "[[doc:" + doc.ID + "#" + hits[0].Chunk.ID + "]]"
	_, _, err = b.ConversationSend(ctx, "client", "first", "cnv_answer", "Tell me about robin nests.\nAttached: Field guide ("+doc.ID+")")
	if err != nil {
		t.Fatal(err)
	}
	history := awaitAssistant(t, b, "cnv_answer", 1)
	answer := history[len(history)-1]
	if answer.Text != "Robins nest in hedges [1]." || len(answer.Sources) != 1 {
		t.Fatalf("answer %+v", answer)
	}
	if calls != 2 {
		t.Fatalf("model calls %d", calls)
	}
	forged, _ := b.Services.Memory.ValidateCitations(ctx, []string{ref}, []string{"forged"})
	if len(forged) != 0 {
		t.Fatalf("model-selected run id accepted: %+v", forged)
	}
	published := false
	for _, e := range b.Bus.Replay(types.Cursor{}) {
		if e.Type == types.EventMessageCreated {
			var ev api.MessageEvent
			_ = json.Unmarshal(e.Payload, &ev)
			published = ev.Role == "assistant" && len(ev.Sources) == 1
		}
	}
	if !published {
		t.Fatal("no assistant message.created with sources")
	}
}

func TestEngineAssistantFailureMessages(t *testing.T) {
	for _, mode := range []string{"unconfigured", "provider-error", "empty", "disabled", "step-limit"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("EXCALIBUR_MASTER_ENGINE_TURNS", "1")
			t.Setenv("EXCALIBUR_MASTER_STEPS", "2")
			calls := 0
			var chat func(context.Context, providers.ChatRequest) (providers.ChatResponse, error)
			if mode != "unconfigured" {
				chat = func(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
					calls++
					if mode == "provider-error" {
						return providers.ChatResponse{}, errors.New("test outage")
					}
					if mode == "step-limit" {
						return providers.ChatResponse{Message: providers.Message{ToolCalls: []providers.ToolCall{{ID: "tick", Name: "clock", Arguments: json.RawMessage(`{}`)}}}}, nil
					}
					return textReply(""), nil
				}
			}
			if mode == "disabled" {
				t.Setenv("EXCALIBUR_MASTER_ENGINE_TURNS", "0")
			}
			b, _ := assistantTestWiring(t, false, chat)
			_, _, err := b.ConversationSend(context.Background(), "client", mode, "cnv_failure", "Hello")
			if err != nil {
				t.Fatal(err)
			}
			rows := awaitAssistant(t, b, "cnv_failure", 1)
			answer := rows[len(rows)-1]
			if answer.Text == "" || len(answer.Sources) != 0 {
				t.Fatalf("fallback %+v", answer)
			}
			if mode == "unconfigured" && answer.Text != "No model configured" {
				t.Fatalf("fallback %q", answer.Text)
			}
			if mode == "disabled" && calls != 0 {
				t.Fatal("disabled turn called model")
			}
			if calls > 2 {
				t.Fatalf("unbounded calls %d", calls)
			}
			if mode != "disabled" {
				notice := false
				for _, e := range b.Bus.Replay(types.Cursor{}) {
					notice = notice || e.Type == types.EventNoticePosted
				}
				if !notice {
					t.Fatal("missing failure notice")
				}
			}
		})
	}
}

func TestEngineAssistantQueuesSecondMessage(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	calls := 0
	b, _ := assistantTestWiring(t, false, func(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return providers.ChatResponse{}, ctx.Err()
			}
		}
		return textReply("Answer"), nil
	}, master.Config{Turns: master.TurnConfig{CancelWindow: time.Nanosecond}})
	var err error
	_, _, err = b.ConversationSend(context.Background(), "client", "first", "cnv_queue", "First question")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("model not started")
	}
	// Intake itself is synchronous and must not wait on the model.
	_, _, err = b.ConversationSend(context.Background(), "client", "second", "cnv_queue", "Second question")
	if err != nil {
		t.Fatal(err)
	}
	// Delivery runs right after the receipt; wait for the second message to queue.
	deadline := time.Now().Add(3 * time.Second)
	for b.Master.Turns().PendingCount("cnv_queue") != 1 {
		if time.Now().After(deadline) {
			t.Fatal("second message not queued")
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	rows := awaitAssistant(t, b, "cnv_queue", 2)
	if len(rows) != 4 {
		t.Fatalf("history %+v", rows)
	}
}

func TestEngineAssistantTimeoutAndStop(t *testing.T) {
	for _, mode := range []string{"timeout", "stop"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("EXCALIBUR_MASTER_TIMEOUT", "500ms")
			started := make(chan struct{})
			b, _ := assistantTestWiring(t, false, func(ctx context.Context, _ providers.ChatRequest) (providers.ChatResponse, error) {
				close(started)
				<-ctx.Done()
				return providers.ChatResponse{}, ctx.Err()
			})
			_, _, err := b.ConversationSend(context.Background(), "client", "first", "cnv_stop", "Hello")
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("model not started")
			}
			if mode == "stop" {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := b.Master.Turns().Stop(ctx); err != nil {
					t.Fatal(err)
				}
				if b.Master.Turns().Active("cnv_stop") {
					t.Fatal("turn survived shutdown")
				}
				if len(b.Master.History("cnv_stop", 0)) != 1 {
					t.Fatal("shutdown produced a failure answer")
				}
			} else {
				rows := awaitAssistant(t, b, "cnv_stop", 1)
				if !strings.Contains(rows[len(rows)-1].Text, "couldn't complete") {
					t.Fatalf("timeout answer %+v", rows)
				}
			}
		})
	}
}

func TestEngineAssistantRestartsAndKeepsAttachments(t *testing.T) {
	started := make(chan struct{})
	calls := 0
	var seen string
	b, _ := assistantTestWiring(t, true, func(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		calls++
		if calls == 1 {
			close(started)
			<-ctx.Done()
			return providers.ChatResponse{}, ctx.Err()
		}
		for _, m := range req.Messages {
			seen += m.Content
		}
		return textReply("Answer"), nil
	})
	doc, err := b.Services.Memory.Ingest(context.Background(), nil, "guide.txt", "text/plain", []byte("Robins nest in hedges."), memory.IngestOptions{Title: "Guide"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = b.ConversationSendAttachments(context.Background(), "client", "first", "cnv_restart", "First question", []string{doc.ID})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	// Race-instrumented attachment reads can exceed one second on capped runners.
	case <-time.After(10 * time.Second):
		t.Fatal("model not started")
	}
	_, _, err = b.ConversationSend(context.Background(), "client", "second", "cnv_restart", "Second question")
	if err != nil {
		t.Fatal(err)
	}
	awaitAssistant(t, b, "cnv_restart", 2)
	if !strings.Contains(seen, "First question") || !strings.Contains(seen, "Second question") || !strings.Contains(seen, "Attached document: Guide") {
		t.Fatalf("restart context %s", seen)
	}
	if calls != 3 {
		t.Fatalf("calls %d", calls)
	}
}

func TestAssistantToolBoundaryAndTrustedRunTree(t *testing.T) {
	_, s := assistantTestWiring(t, false, nil)
	r := &assistantToolRunner{runID: "run_master", allowed: map[string]bool{}, children: []types.AgentID{"agt_child"}, childRuns: []string{"run_child"}}
	result, err := r.Execute(context.Background(), "exec", json.RawMessage(`{"command":"unused"}`), tools.ExecuteOptions{})
	if err != nil || result.Status != tools.StatusInputError {
		t.Fatalf("out-of-role execution %+v %v", result, err)
	}
	child := types.AgentID("agt_child")
	for _, a := range []types.Agent{{ID: child}, {ID: "agt_grandchild", ParentID: &child}, {ID: "agt_unrelated"}} {
		if err := s.Store.SaveAgent(a); err != nil {
			t.Fatal(err)
		}
	}
	for _, run := range []types.Run{{ID: "run_child", AgentID: child}, {ID: "run_grandchild", AgentID: "agt_grandchild"}, {ID: "run_unrelated", AgentID: "agt_unrelated"}} {
		if err := s.Store.CreateRun(orchestrator.RunRecord{Run: run}); err != nil {
			t.Fatal(err)
		}
	}
	s.History.Record("child.txt", nil, []byte("child"), "run_child", "")
	s.History.Record("nested.txt", nil, []byte("nested"), "run_grandchild", "")
	s.History.Record("unrelated.txt", nil, []byte("unrelated"), "run_unrelated", "")
	changes := s.History.Changes(r.runTree(s.Store), nil)
	if len(changes) != 2 || changes[0].Path != "child.txt" || changes[1].Path != "nested.txt" {
		t.Fatalf("turn changes: %+v", changes)
	}
	tree := strings.Join(r.runTree(s.Store), ",")
	if !strings.Contains(tree, "run_master") || !strings.Contains(tree, "run_grandchild") || strings.Contains(tree, "run_unrelated") {
		t.Fatalf("citation tree %s", tree)
	}
}

func TestAssistantContextIsBounded(t *testing.T) {
	store := master.NewMemStore()
	for i := 0; i < 30; i++ {
		if _, err := store.SaveMessage(context.Background(), "cnv_long", master.Normalized{Text: strings.Repeat("x", 5000)}); err != nil {
			t.Fatal(err)
		}
	}
	b := NewDaemonBackend(master.New(master.Config{}, store, nil, nil, nil, nil, nil), nil, nil)
	a := assistantTurns{backend: b, services: &Services{}}
	snap := a.snapshot(context.Background(), "cnv_long", master.Normalized{Text: "current"})
	total := 0
	for _, m := range snap.Activity {
		total += len([]rune(m.Content))
		if len([]rune(m.Content)) > 4001 {
			t.Fatal("unbounded message")
		}
	}
	if total > 24006 || len(snap.Activity) > 20 {
		t.Fatalf("unbounded context: %d chars, %d messages", total, len(snap.Activity))
	}
}

func TestAssistantFastLaneReplyIsDelivered(t *testing.T) {
	calls := 0
	b, _ := assistantTestWiring(t, false, func(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
		calls++
		return textReply("unexpected"), nil
	})
	_, _, err := b.ConversationSend(context.Background(), "client", "stop", "cnv_control", "/stop")
	if err != nil {
		t.Fatal(err)
	}
	rows := awaitAssistant(t, b, "cnv_control", 1)
	if calls != 0 || !strings.Contains(rows[len(rows)-1].Text, "Interrupted the master turn") {
		t.Fatalf("fast lane %+v, calls=%d", rows, calls)
	}
	_, _, err = b.ConversationSend(context.Background(), "client", "stop", "cnv_control", "/stop")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Master.History("cnv_control", 0)) != 2 {
		t.Fatal("duplicate control reply")
	}
}
