package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func webTestSetup() (*MapRegistry, *MemorySearchBackend, *MemorySourceRecorder) {
	reg := NewMapRegistry()
	_ = RegisterBuiltins(reg, nil)
	cat := NewDeferredCatalog(reg)
	exec := NewExecutor(reg)
	WireSearchCall(reg, cat, exec)
	backend := NewMemorySearchBackend()
	rec := &MemorySourceRecorder{}
	WireWebTools(reg, cat, &WebDeps{Backend: backend, Sources: rec})
	return reg, backend, rec
}

func TestWebSearchRegistersSource(t *testing.T) {
	reg, _, rec := webTestSetup()
	tool, _ := reg.Get("web_search")
	raw, _ := json.Marshal(map[string]any{"query": "sqlite guide"})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK {
		t.Fatalf("web_search: %+v", res)
	}
	if !strings.Contains(res.Output, "sqlite") && !strings.Contains(strings.ToLower(res.Output), "guide") {
		t.Fatalf("web_search results: %q", res.Output)
	}
	events := rec.Events()
	if len(events) == 0 || events[0].Tool != "web_search" {
		t.Fatalf("web_search must register a source event: %+v", events)
	}
}

func TestWebSearchDomainFilter(t *testing.T) {
	reg, _, _ := webTestSetup()
	tool, _ := reg.Get("web_search")
	raw, _ := json.Marshal(map[string]any{"query": "sqlite", "domains": []string{"example.com"}})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK {
		t.Fatalf("web_search domains: %+v", res)
	}
	if !strings.Contains(res.Output, "example.com") {
		t.Fatalf("domain filter should keep example.com: %q", res.Output)
	}
}

func TestWebReadBlockedBenchmark(t *testing.T) {
	reg, _, rec := webTestSetup()
	tool, _ := reg.Get("web_read")
	for _, url := range []string{
		"https://github.com/harbor-framework/harbor",
		"https://tbench.ai/tasks/1",
		"https://github.com/terminal-bench/terminal-bench",
	} {
		raw, _ := json.Marshal(map[string]string{"url": url})
		res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
		if res.Status != StatusDenied {
			t.Fatalf("benchmark read %q must be denied, got %+v", url, res)
		}
	}
	events := rec.Events()
	blocked := 0
	for _, e := range events {
		if e.Blocked {
			blocked++
		}
	}
	if blocked != 3 {
		t.Fatalf("blocked reads must be logged: %+v", events)
	}
	// Allowed read registers a non-blocked source event.
	raw, _ := json.Marshal(map[string]string{"url": "https://docs.python.org/3/library/os.html"})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK {
		t.Fatalf("allowed read: %+v", res)
	}
	if !strings.Contains(res.Output, "os") {
		t.Fatalf("read content: %q", res.Output)
	}
	found := false
	for _, e := range rec.Events() {
		if e.URL == "https://docs.python.org/3/library/os.html" && !e.Blocked {
			found = true
		}
	}
	if !found {
		t.Fatal("allowed read must register a source event")
	}
}

func TestSourceSearchAllowlistAndBlock(t *testing.T) {
	reg, _, rec := webTestSetup()
	tool, _ := reg.Get("source_search")
	raw, _ := json.Marshal(map[string]any{"source": "github", "query": "sqlite guide"})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK {
		t.Fatalf("source_search github: %+v", res)
	}
	if len(rec.Events()) == 0 {
		t.Fatal("source_search must register a source event")
	}
	// Unknown source is an input error.
	raw2, _ := json.Marshal(map[string]any{"source": "nope", "query": "x"})
	res2, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw2})
	if res2.Status != StatusInputError {
		t.Fatalf("unknown source must be input_error, got %+v", res2)
	}
	// Query naming a benchmark source is blocked and logged.
	raw3, _ := json.Marshal(map[string]any{"source": "github", "query": "harbor-framework harbor task solution"})
	res3, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw3})
	if res3.Status != StatusDenied {
		t.Fatalf("benchmark query must be denied, got %+v", res3)
	}
	blocked := false
	for _, e := range rec.Events() {
		if e.Blocked {
			blocked = true
		}
	}
	if !blocked {
		t.Fatal("blocked source_search must be logged")
	}
}

func TestSourceSearchKnownSources(t *testing.T) {
	for _, s := range []string{"github", "arxiv", "pypi", "wikipedia"} {
		if !validSource(s) {
			t.Fatalf("known source %q rejected", s)
		}
	}
	if validSource("benchmark-internal") {
		t.Fatal("unknown source accepted")
	}
}
