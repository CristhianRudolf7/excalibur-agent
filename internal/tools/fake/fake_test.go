package fake

import (
	"context"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestScriptedTool(t *testing.T) {
	tool := New("grep")
	var _ tools.Tool = tool
	if err := tool.Definition().Validate(); err != nil {
		t.Fatal(err)
	}
	tool.Results = []tools.Result{{Status: tools.StatusOK, Output: "hit"}}
	tool.Keys = []string{"/repo/a.go"}
	res, err := tool.Invoke(context.Background(), tools.Invocation{Tool: "grep"})
	if err != nil || res.Output != "hit" {
		t.Fatalf("invoke: %+v %v", res, err)
	}
	res, err = tool.Invoke(context.Background(), tools.Invocation{Tool: "grep"})
	if err != nil || res.Status != tools.StatusOK {
		t.Fatal("exhausted script should return empty OK")
	}
	if got := tool.Recorded(); len(got) != 2 {
		t.Fatal("calls should be recorded")
	}
	keys, err := tool.ResourceKeys(nil)
	if err != nil || len(keys) != 1 {
		t.Fatal("resource keys")
	}
	reg := tools.NewMapRegistry()
	if err := reg.Register(tool); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(tool); err == nil {
		t.Fatal("expected duplicate error")
	}
	if got := reg.Names(); len(got) != 1 || got[0] != "grep" {
		t.Fatalf("names: %v", got)
	}
}
