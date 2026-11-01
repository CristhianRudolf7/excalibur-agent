package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSkillsOffDiscoveryAndExecution(t *testing.T) {
	t.Setenv("EXCALIBUR_SKILLS", "off")
	reg := NewMapRegistry()
	_ = RegisterBuiltins(reg, nil)
	cat := NewDeferredCatalogWithCore(reg, []string{"search_skills"})
	for _, names := range [][]string{cat.CoreNames(), cat.DeferredNames()} {
		for _, name := range names {
			if name == "search_skills" || name == "load_skill" {
				t.Fatalf("disabled skill exposed: %s", name)
			}
		}
	}
	for _, hit := range cat.Search("load skill", 5) {
		if SkillToolDisabled(hit.Definition.Name) {
			t.Fatal("disabled skill in search")
		}
	}
	ex := &Executor{Registry: reg}
	call := &CallToolTool{Catalog: cat, Executor: ex}
	for _, name := range []string{"search_skills", "load_skill"} {
		res, err := ex.Execute(context.Background(), name, json.RawMessage("{}"), ExecuteOptions{})
		if err != nil || res.Status != StatusInputError || !strings.Contains(res.Output, "disabled") {
			t.Fatalf("direct %s: %+v %v", name, res, err)
		}
		args, _ := json.Marshal(map[string]any{"name": name, "arguments": map[string]any{}})
		res, err = call.Invoke(context.Background(), Invocation{Arguments: args})
		if err != nil || res.Status != StatusInputError || !strings.Contains(res.Output, "disabled") {
			t.Fatalf("wrapped %s: %+v %v", name, res, err)
		}
	}
	t.Setenv("EXCALIBUR_SKILLS", "")
	if SkillToolDisabled("load_skill") || len(cat.CoreNames()) != 1 {
		t.Fatal("assistant default changed")
	}
}
