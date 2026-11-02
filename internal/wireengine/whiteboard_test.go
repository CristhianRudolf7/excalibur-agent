package wireengine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestWhiteboardDeferredDiscoveryAndDispatch(t *testing.T) {
	emb, err := BuildEmbedded(EmbeddedOptions{RunID: "board-run"})
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	if !emb.Catalog.IsDeferred("whiteboard") {
		t.Fatal("whiteboard must be deferred")
	}
	for _, args := range []struct{ name, raw string }{{"search_tools", `{"query":"whiteboard"}`}, {"call_tool", `{"name":"whiteboard","arguments":{"action":"open","title":"Embedded teaching"}}`}} {
		r, err := emb.Exec.Execute(context.Background(), args.name, json.RawMessage(args.raw), tools.ExecuteOptions{})
		if err != nil || r.Status != tools.StatusOK || strings.Contains(r.Output, "not wired") {
			t.Fatalf("%s: %+v %v", args.name, r, err)
		}
	}
	ix := skills.NewEvalBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	if _, ok := ix.Get("whiteboard-teaching"); ok {
		t.Fatal("assistant skill entered eval")
	}
	current := skills.NewBuiltinIndex()
	if err := current.Scan(); err != nil {
		t.Fatal(err)
	}
	if _, ok := current.Get("whiteboard-teaching"); !ok {
		t.Fatal("missing teaching skill")
	}
}
func TestWhiteboardDisabled(t *testing.T) {
	t.Setenv("EXCALIBUR_WHITEBOARD", "0")
	emb, err := BuildEmbedded(EmbeddedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	if _, err := emb.Registry.Get("whiteboard"); err == nil {
		t.Fatal("disabled tool registered")
	}
}

func TestEvalDoesNotRegisterWhiteboard(t *testing.T) {
	emb, err := BuildEmbedded(EmbeddedOptions{Profile: "eval"})
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	if _, err := emb.Registry.Get("whiteboard"); err == nil {
		t.Fatal("whiteboard registered in eval")
	}
	if emb.Catalog.IsDeferred("whiteboard") {
		t.Fatal("whiteboard in eval discovery")
	}
	r, err := emb.Exec.Execute(context.Background(), "call_tool", json.RawMessage(`{"name":"whiteboard","arguments":{"action":"open"}}`), tools.ExecuteOptions{})
	if err == nil && r.Status == tools.StatusOK {
		t.Fatalf("whiteboard executable in eval: %+v", r)
	}
}
