package memory

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// wireTestRegistry builds a registry with the H3 stubs, then wires
// the memory providers over them.
func wireTestRegistry(t *testing.T, svc *Service) *tools.MapRegistry {
	t.Helper()
	reg := tools.NewMapRegistry()
	for _, stub := range tools.CoreStubDefinitions() {
		def := stub
		if err := reg.Register(&tools.StubTool{Def: def}); err != nil {
			t.Fatal(err)
		}
	}
	WireMemoryTools(reg, svc)
	return reg
}

func invoke(t *testing.T, reg *tools.MapRegistry, name, args string) tools.Result {
	t.Helper()
	tool, err := reg.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tool.Invoke(context.Background(), tools.Invocation{Tool: name, Arguments: json.RawMessage(args)})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// TestWireMemoryToolsKeepsCatalogPrefix wires twice and asserts the
// catalog stays stable while providers answer (Section 14.12).
func TestWireMemoryToolsKeepsCatalogPrefix(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	reg := wireTestRegistry(t, svc)
	before := reg.Names()
	WireMemoryTools(reg, svc)
	after := reg.Names()
	if len(before) != len(after) {
		t.Fatalf("rewire changed catalog size %d -> %d", len(before), len(after))
	}
	res := invoke(t, reg, ToolMemoryWrite, `{"statement":"Wired memory works","scope":"general"}`)
	if res.Status != tools.StatusOK {
		t.Fatalf("memory_write: %s", res.Output)
	}
	res = invoke(t, reg, ToolMemorySearch, `{"query":"wired memory","scopes":["general"]}`)
	if res.Status != tools.StatusOK {
		t.Fatalf("memory_search: %s", res.Output)
	}
	var parsed struct {
		Items []struct {
			Statement string `json:"statement"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(res.Output), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Items) == 0 {
		t.Fatal("wired search found nothing")
	}
}

// TestMemoryToolsEndToEnd drives propose, history, docs and library
// through the tool surface.
func TestMemoryToolsEndToEnd(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")
	reg := wireTestRegistry(t, svc)

	res := invoke(t, reg, ToolMemoryPropose, `{"statement":"Candidate via tool","scope":"general","conversation":"cnv_tool"}`)
	if res.Status != tools.StatusOK {
		t.Fatalf("propose: %s", res.Output)
	}
	writeMessage(t, db, "msg_tool_1", "cnv_tool", "general", "user", "Candidate via tool discussion", now.Add(-time.Hour))
	res = invoke(t, reg, ToolHistorySearch, `{"text":"candidate via tool","scopes":["general"]}`)
	if res.Status != tools.StatusOK {
		t.Fatalf("history_search: %s", res.Output)
	}
	res = invoke(t, reg, ToolLibraryIngest, `{"scope":"general","title":"Guide","mime":"text/plain","text":"# S\n\nTool ingest paragraph."}`)
	if res.Status != tools.StatusOK {
		t.Fatalf("library_ingest: %s", res.Output)
	}
	res = invoke(t, reg, ToolLibrarySearch, `{"query":"ingest paragraph","scopes":["general"],"run_id":"run_tool"}`)
	if res.Status != tools.StatusOK || !json.Valid([]byte(res.Output)) {
		t.Fatalf("library_search: %s", res.Output)
	}
}

// TestDocToolsEndToEnd drives doc_read and doc_edit through the tool
// surface, including the mismatch redo payload.
func TestDocToolsEndToEnd(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")
	reg := wireTestRegistry(t, svc)

	docID, err := svc.ResolveDoc(context.Background(), "general", DocNotebook)
	if err != nil {
		t.Fatal(err)
	}
	frag, err := svc.AppendFragment(context.Background(), []string{"general"}, docID, "notes", "Tool note", "")
	if err != nil {
		t.Fatal(err)
	}
	res := invoke(t, reg, ToolDocRead, `{"scope":"general","doc":"`+docID+`","selector":"section:notes"}`)
	if res.Status != tools.StatusOK {
		t.Fatalf("doc_read: %s", res.Output)
	}
	res = invoke(t, reg, ToolDocEdit, `{"scope":"general","doc":"`+docID+`","fragment_id":"`+frag.ID+`","expected_hash":"stale","op":"replace","text":"x"}`)
	if res.Status != tools.StatusInputError {
		t.Fatalf("stale edit status = %q, want input_error with current text", res.Status)
	}
	var payload struct {
		Current Fragment `json:"current"`
	}
	if err := json.Unmarshal([]byte(res.Output), &payload); err != nil {
		t.Fatalf("mismatch payload: %v: %s", err, res.Output)
	}
	if payload.Current.Text != "Tool note" {
		t.Fatalf("current text = %q", payload.Current.Text)
	}
	res = invoke(t, reg, ToolMemoryWrite, `{"statement":"","scope":"general"}`)
	if res.Status != tools.StatusInputError {
		t.Fatalf("empty statement status = %q, want input_error", res.Status)
	}
}
