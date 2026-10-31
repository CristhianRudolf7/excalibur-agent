package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func packageTestSetup() (*MapRegistry, *DeferredCatalog, *Executor, *PackageManager) {
	reg := NewMapRegistry()
	_ = RegisterBuiltins(reg, nil)
	cat := NewDeferredCatalog(reg)
	exec := NewExecutor(reg)
	WireSearchCall(reg, cat, exec)
	mgr := NewPackageManager(reg, cat, exec)
	WirePackageTool(reg, cat, mgr)
	return reg, cat, exec, mgr
}

func validPackage(name string, tests []PackageTest) PackageManifest {
	return PackageManifest{
		Name: name, Version: "0.1.0", Description: "Create a cube in the modeling app",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"size":{"type":"number"}},"required":["size"]}`),
		Effect:      "read_only", Idempotency: "pure", Cancel: "immediate",
		Group: "apps", Tags: []string{"model3d"}, Keywords: []string{"cube", "create", "model3d"},
		Implementation: PackageImpl{Kind: PackageScript, ScriptCmd: "make-cube", DryRun: "dry-run ok"},
		Tests:          tests,
	}
}

func TestToolPackageCreateActivate(t *testing.T) {
	_, cat, _, mgr := packageTestSetup()
	pm := validPackage("cube_create", []PackageTest{
		{Name: "small", Input: json.RawMessage(`{"size": 2}`), Contains: "make-cube"},
	})
	rec, err := mgr.Create(context.Background(), pm)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != PackageActive {
		t.Fatalf("package should activate, got %+v", rec)
	}
	if !cat.IsDeferred("cube_create") {
		t.Fatal("active package must be deferred-searchable")
	}
	hits := cat.Search("create cube model3d", 5)
	found := false
	for _, h := range hits {
		if h.Definition.Name == "cube_create" {
			found = true
		}
	}
	if !found {
		t.Fatal("new package not found by search_tools")
	}
	// The activated tool runs through the pipeline.
	tool, err := catSearchRegistry(cat, "cube_create")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"size": 2})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK || !strings.Contains(res.Output, "make-cube") {
		t.Fatalf("package invoke: %+v", res)
	}
	// Stats report calls.
	got, _ := mgr.Get("cube_create")
	if got.Calls != 1 {
		t.Fatalf("usage stats: %+v", got)
	}
}

func catSearchRegistry(cat *DeferredCatalog, name string) (Tool, error) {
	return cat.reg.Get(name)
}

func TestToolPackageBadManifestNotActivated(t *testing.T) {
	_, cat, _, mgr := packageTestSetup()
	pm := validPackage("Bad-Name", []PackageTest{
		{Name: "t", Input: json.RawMessage(`{}`)},
	})
	if _, err := mgr.Create(context.Background(), pm); err == nil {
		t.Fatal("bad name must not validate")
	}
	if cat.IsDeferred("Bad-Name") {
		t.Fatal("invalid package must not activate")
	}
	pm2 := validPackage("no_tests", nil)
	if _, err := mgr.Create(context.Background(), pm2); err == nil {
		t.Fatal("package without tests must not validate")
	}
}

func TestToolPackageFailingTestNotActivated(t *testing.T) {
	_, cat, _, mgr := packageTestSetup()
	pm := validPackage("cube_fail", []PackageTest{
		{Name: "wrong", Input: json.RawMessage(`{"size": 1}`), Exact: "never this output"},
	})
	rec, err := mgr.Create(context.Background(), pm)
	if err == nil {
		t.Fatal("failing test must block activation")
	}
	if rec == nil || rec.Status != PackageFailed {
		t.Fatalf("failed package state: %+v", rec)
	}
	if cat.IsDeferred("cube_fail") {
		t.Fatal("failing package must not be searchable")
	}
}

func TestToolPackageEffectfulDryRun(t *testing.T) {
	_, _, _, mgr := packageTestSetup()
	pm := validPackage("cube_dry", []PackageTest{
		{Name: "dry", Input: json.RawMessage(`{"size": 3}`), Contains: "dry-run"},
	})
	pm.Effect = "write_external"
	pm.Implementation.DryRun = "dry-run: would create cube size 3"
	rec, err := mgr.Create(context.Background(), pm)
	if err != nil {
		t.Fatalf("effectful package with dry-run tests should activate: %v", err)
	}
	if rec.Status != PackageActive {
		t.Fatalf("status: %+v", rec)
	}
}

func TestToolPackageToolValidation(t *testing.T) {
	reg, _, _, mgr := packageTestSetup()
	tool, err := reg.Get("tool_package")
	if err != nil {
		t.Fatal(err)
	}
	// create via the tool itself.
	raw, _ := json.Marshal(map[string]any{
		"action": "create", "name": "via_tool", "description": "d",
		"input_schema": map[string]any{"type": "object"},
		"effect":       "read_only", "idempotency": "pure",
		"kind": "script", "script_cmd": "echo",
		"tests": []any{map[string]any{"name": "t", "input": map[string]any{}, "contains": "echo"}},
	})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK {
		t.Fatalf("tool_package create: %+v", res)
	}
	if _, err := mgr.Get("via_tool"); err != nil {
		t.Fatalf("created package missing: %v", err)
	}
	// stats and list.
	raw2, _ := json.Marshal(map[string]string{"action": "stats", "name": "via_tool"})
	res2, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw2})
	if res2.Status != StatusOK || !strings.Contains(res2.Output, "via_tool") {
		t.Fatalf("stats: %+v", res2)
	}
	raw3, _ := json.Marshal(map[string]string{"action": "list"})
	res3, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw3})
	if res3.Status != StatusOK || !strings.Contains(res3.Output, "via_tool") {
		t.Fatalf("list: %+v", res3)
	}
}
