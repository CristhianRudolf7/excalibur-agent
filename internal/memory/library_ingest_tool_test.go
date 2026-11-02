package memory

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

type fakeLibrarySubmitter struct {
	scope, path string
	meta        DocMeta
	calls       int
}

func (f *fakeLibrarySubmitter) SubmitLibrary(ctx context.Context, scope string, m DocMeta, path string) (DocumentInfo, bool, error) {
	f.calls++
	f.scope = scope
	f.meta = m
	f.path = path
	return DocumentInfo{ID: "doc-queued", Status: "pending", Version: 1}, false, nil
}
func TestLibraryIngestBackgroundArguments(t *testing.T) {
	svc := NewService(nil, WithScopes("general"))
	fake := &fakeLibrarySubmitter{}
	svc.BindLibraryIngester(fake)
	tool := &libraryIngestTool{svc: svc}
	for _, raw := range []string{`{"path":"/tmp/guide.pdf","title":"Guide"}`, `{"blob_digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","name":"guide.pdf","scope":"general"}`} {
		result, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(raw)})
		if err != nil || result.Status != tools.StatusOK {
			t.Fatalf("%s: %+v %v", raw, result, err)
		}
		var out struct {
			Document DocumentInfo `json:"document"`
		}
		if err = json.Unmarshal([]byte(result.Output), &out); err != nil || out.Document.Status != "pending" {
			t.Fatalf("%+v %v", out, err)
		}
		if fake.scope != "general" {
			t.Fatal(fake.scope)
		}
	}
	before := fake.calls
	for _, raw := range []string{`{"path":"file","text":"embedded"}`, `{"path":"file","scope":"project:other"}`, `{"path":12}`, `{`} {
		result, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(raw)})
		if err != nil || result.Status == tools.StatusOK {
			t.Fatalf("%s: %+v %v", raw, result, err)
		}
	}
	if fake.calls != before {
		t.Fatalf("invalid calls reached submitter: %d", fake.calls)
	}
	for _, profile := range tool.Definition().Profiles {
		if profile == "eval" {
			t.Fatal("eval exposed")
		}
	}
}
func TestLibraryIngestReplacesDeferredSimulation(t *testing.T) {
	svc := NewService(nil, WithScopes("general"))
	fake := &fakeLibrarySubmitter{}
	svc.BindLibraryIngester(fake)
	reg := tools.NewMapRegistry()
	if err := tools.RegisterBuiltins(reg, nil); err != nil {
		t.Fatal(err)
	}
	cat := tools.NewDeferredCatalog(reg)
	if err := tools.RegisterDeferredMedia(reg, cat, &tools.MediaDeps{}); err != nil {
		t.Fatal(err)
	}
	WireLibraryIngestTool(reg, nil)
	cat.MarkDeferred(ToolLibraryIngest)
	prefix := cat.PrefixHash()
	WireMemoryTools(reg, svc)
	if !cat.IsDeferred(ToolLibraryIngest) || prefix != cat.PrefixHash() {
		t.Fatal("ingest must remain deferred")
	}
	pipeline := tools.NewExecutor(reg)
	tools.WireSearchCall(reg, cat, pipeline)
	result, err := pipeline.Execute(context.Background(), "call_tool", json.RawMessage(`{"name":"library_ingest","arguments":{"path":"guide.pdf"}}`), tools.ExecuteOptions{})
	if err != nil || result.Status != tools.StatusOK || fake.calls != 1 {
		t.Fatalf("%+v %v calls=%d", result, err, fake.calls)
	}
}
