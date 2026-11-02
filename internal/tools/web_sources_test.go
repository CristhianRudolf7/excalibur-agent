package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestWebSourceAttributionAndRetention(t *testing.T) {
	for _, toolName := range []string{"web_search", "source_search"} {
		t.Run(toolName, func(t *testing.T) {
			fake := NewMemorySearchBackend()
			fake.Docs = []SearchResult{{URL: "https://example.test/article", Title: "Article", Snippet: "transient snippet", SourceID: "github", Provider: "tavily"}}
			rec := &MemorySourceRecorder{}
			deps := &WebDeps{Backend: fake, Sources: rec}
			var tool Tool = &WebSearchTool{Deps: deps}
			args := json.RawMessage(`{"query":"Article"}`)
			if toolName == "source_search" {
				tool = &SourceSearchTool{Deps: deps}
				args = json.RawMessage(`{"source":"github","query":"Article"}`)
			}
			res, err := tool.Invoke(WithSourceRun(context.Background(), "run_a"), Invocation{Arguments: args})
			if err != nil || res.Status != StatusOK {
				t.Fatalf("invoke %+v %v", res, err)
			}
			events := rec.Events()
			if len(events) != 1 || events[0].Tool != toolName || events[0].RunID != "run_a" || events[0].At == "" {
				t.Fatalf("events %+v", events)
			}
			raw, _ := json.Marshal(events)
			if strings.Contains(string(raw), "transient snippet") || strings.Contains(res.Output, `"Provider"`) || strings.Contains(res.Output, "tavily") {
				t.Fatal("private data entered sources or provider metadata entered tool output")
			}
			fake.Docs[0].Provider = "brave"
			_, err = tool.Invoke(WithSourceRun(context.Background(), "run_b"), Invocation{Arguments: args})
			if err != nil || len(rec.Events()) != 1 {
				t.Fatal("Brave search result was retained")
			}
		})
	}
}
