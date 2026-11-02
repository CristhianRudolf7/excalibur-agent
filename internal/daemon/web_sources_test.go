package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

// sourceTurnEndpoint asks the real engine to read a fake page before answering.
type sourceTurnEndpoint struct{ calls int }

func (e *sourceTurnEndpoint) Chat(context.Context, types.ModelRole, providers.ChatRequest) (providers.ChatResponse, models.CallMeta, error) {
	e.calls++
	if e.calls == 1 {
		return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "read-page", Name: "web_read", Arguments: json.RawMessage(`{"url":"https://example.test/page"}`)}}}}, models.CallMeta{}, nil
	}
	return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, Content: "Answer from the page."}}, models.CallMeta{}, nil
}

func TestEngineWebReadSourcesPersistPublishAndHistory(t *testing.T) {
	ctx := context.Background()
	backend := citationTestBackend(t)
	fake := tools.NewMemorySearchBackend()
	fake.Pages["https://example.test/page"] = tools.Page{URL: "https://example.test/page", Title: "Read page", Markdown: "Useful evidence"}
	endpoint := &sourceTurnEndpoint{}
	_, dt, err := buildEngine(engineWiring{Models: endpoint, BrowserWeb: &wireengine.BrowserWeb{Web: &tools.WebDeps{Backend: fake}}})
	if err != nil {
		t.Fatal(err)
	}
	backend.Services.Sources = dt.Sources
	eng := engine.New(engine.Deps{Models: endpoint, Context: daemonRequestBuilder(dt.Registry, ""), Tools: &daemonToolRunner{ex: dt.Exec, reg: dt.Registry}, Gate: engine.GateFunc(func(*engine.Run) tasks.GateReport { return tasks.GateReport{OK: true} }), SelfVerify: &tasks.SelfVerifyConfig{}, LongHorizon: &tasks.LongHorizonConfig{}})
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	run := engine.NewRun("run_web", "", engine.Profile{Name: "master", Limit: tasks.Limit{MaxSteps: 2, MaxSeconds: 60}})
	if err := eng.Admit(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := eng.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	events := dt.Sources.Events()
	if len(events) != 1 || events[0].RunID != "run_web" {
		t.Fatalf("run attribution %+v", events)
	}
	// Unrelated, blocked and invalid URLs must never leak into this answer.
	for _, e := range []tools.SourceEvent{
		{RunID: "run_other", Tool: "web_read", URL: "https://other.test/private"},
		{RunID: "run_web", Tool: "web_read", URL: "https://blocked.test", Blocked: true},
		{RunID: "run_web", Tool: "web_search", URL: "file:///tmp/private"},
	} {
		dt.Sources.Record(e)
	}
	// Library citations retain their original fields alongside web evidence.
	doc, err := backend.Services.Memory.Ingest(ctx, []string{"general"}, "guide.txt", "text/plain", []byte("The robin nests in spring hedges."), memory.IngestOptions{Title: "Guide"})
	if err != nil {
		t.Fatal(err)
	}
	hits, _, err := backend.Services.Memory.LibrarySearch(ctx, []string{"general"}, "run_web", "robin nests", 1)
	if err != nil || len(hits) == 0 {
		t.Fatalf("library fixture %v", err)
	}
	ev, err := backend.Master.SendAssistant(ctx, "cnv_web", "Answer [[doc:"+doc.ID+"#"+hits[0].Chunk.ID+"]].", []string{"run_web"})
	if err != nil || len(ev.Sources) != 2 {
		t.Fatalf("sources %+v %v", ev, err)
	}
	if ev.Sources[0].Kind != "document" || ev.Sources[0].DocumentID != doc.ID || ev.Sources[0].Excerpt == "" || ev.Sources[0].MIME != "text/plain" {
		t.Fatalf("library changed %+v", ev.Sources[0])
	}
	source := ev.Sources[1]
	if source.Kind != "web" || source.URL != "https://example.test/page" || source.Title != "Read page" || source.Domain != "example.test" || source.Tool != "web_read" || source.ReadAt == "" {
		t.Fatalf("web source %+v", source)
	}
	cfg := gateway.DefaultConfig()
	cfg.AllowRemote = true
	g := gateway.New(cfg, backend, nil, nil)
	var history struct {
		Messages []gateway.StoredMessage `json:"messages"`
	}
	if err := audioRPC(t, g, api.MethodConversationHistory, map[string]any{"conversation_id": "cnv_web"}, &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Messages) != 1 || !sameWireSources(history.Messages[0].Sources, ev.Sources) {
		t.Fatalf("history %+v", history)
	}
	found := false
	for _, event := range backend.Bus.Replay(types.Cursor{}) {
		if event.Type == types.EventMessageCreated {
			var published api.MessageEvent
			_ = json.Unmarshal(event.Payload, &published)
			if published.Role == "assistant" {
				found = sameWireSources(published.Sources, ev.Sources)
			}
		}
	}
	if !found {
		t.Fatal("message.created omitted sources")
	}
}

func TestBraveSnippetsAndUnreadResultsExcluded(t *testing.T) {
	b := citationTestBackend(t)
	rec := &tools.MemorySourceRecorder{}
	fake := tools.NewMemorySearchBackend()
	fake.Docs = []tools.SearchResult{{URL: "https://brave.test/unread", Title: "Unread", Snippet: "BRAVE_PRIVATE_SNIPPET", Provider: "brave"}}
	deps := &tools.WebDeps{Backend: fake, Sources: rec}
	ctx := tools.WithSourceRun(context.Background(), "run_brave")
	res, err := (&tools.WebSearchTool{Deps: deps}).Invoke(ctx, tools.Invocation{Arguments: json.RawMessage(`{"query":"Unread"}`)})
	if err != nil || res.Status != tools.StatusOK {
		t.Fatalf("search %+v %v", res, err)
	}
	if len(rec.Events()) != 0 {
		t.Fatal("persisted unread Brave result")
	}
	fake.Pages["https://brave.test/read"] = tools.Page{URL: "https://brave.test/read", Title: "Read", Markdown: "Page body"}
	res, err = (&tools.WebReadTool{Deps: deps}).Invoke(ctx, tools.Invocation{Arguments: json.RawMessage(`{"url":"https://brave.test/read"}`)})
	if err != nil || res.Status != tools.StatusOK {
		t.Fatalf("read %+v %v", res, err)
	}
	b.Services.Sources = rec
	ev, err := b.Master.SendAssistant(ctx, "cnv_brave", "Answer", []string{"run_brave"})
	if err != nil || len(ev.Sources) != 1 || ev.Sources[0].URL != "https://brave.test/read" {
		t.Fatalf("Brave sources %+v %v", ev, err)
	}
	raw, _ := json.Marshal(ev)
	if strings.Contains(string(raw), "BRAVE_PRIVATE_SNIPPET") || strings.Contains(string(raw), "Page body") || strings.Contains(string(raw), "unread") {
		t.Fatalf("retained search/page content %s", raw)
	}
}

// JSON omits empty boxes; compare wire values rather than nil/empty slice identity.
func sameWireSources(a, b []api.SourceRef) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
