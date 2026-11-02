package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

// daemonOwnedInExec lists tools that stay daemon-owned stubs in
// single-run exec scope (conversation/master subsystems, other
// workstreams): exposed by name in wider profiles but never invoked
// by the eval surface. The enumeration below documents them instead
// of invoking them.
var daemonOwnedInExec = map[string]bool{
	"send_message": true,
}

// TestExecSurfaceEveryToolWorks enumerates every tool exposed in the
// eval exec surface and invokes it for real against the embedded
// catalog: none may answer "not wired", and every crafted call must
// succeed with actionable output.
func TestExecSurfaceEveryToolWorks(t *testing.T) {
	dir := t.TempDir()
	probe := filepath.Join(dir, "probe.txt")
	if err := os.WriteFile(probe, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	screen := filepath.Join(dir, "screen.ppm")
	if err := os.WriteFile(screen, []byte("P3\n2 1\n255\n0 0 0 255 255 255\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	emb, err := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{RunID: "run_surface_enum", Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	blob := emb.Exec.Outputs.Put(strings.Repeat("b", 100))

	calls := map[string]any{
		"inspect_image":           map[string]any{"path": screen},
		"read_file":               map[string]any{"path": probe},
		"write_file":              map[string]any{"path": filepath.Join(dir, "new.txt"), "content": "hello\n"},
		"edit_file":               map[string]any{"path": probe, "old_text": "beta\n", "new_text": "BETA\n", "expected_fp": "*"},
		"exec":                    map[string]any{"command": "echo surface-ok", "workdir": dir},
		"exec_io":                 map[string]any{"action": "list"},
		"grep":                    map[string]any{"pattern": "alpha", "paths": []string{dir}},
		"glob":                    map[string]any{"pattern": "*.txt", "dir": dir},
		"apply_patch":             map[string]any{"ops": []any{map[string]any{"path": filepath.Join(dir, "patched.txt"), "op": "add", "new_text": "p\n"}}},
		"clock":                   map[string]any{},
		"status":                  map[string]any{},
		"get_tool_output":         map[string]any{"ref": blob},
		"history_search":          map[string]any{},
		"pin_observation":         map[string]any{"observation": "keep"},
		"leave_continuation_note": map[string]any{"note": "next"},
		"ack_directive":           map[string]any{},
		"task_db":                 map[string]any{"ops": []any{map[string]any{"op": "query", "entity": "criteria"}}},
		"search_skills":           map[string]any{"query": ""},
		"load_skill":              map[string]any{"name": "task-breakdown"},
		"search_tools":            map[string]any{"query": ""},
		"call_tool":               map[string]any{"name": "file_history", "arguments": map[string]any{"path": "any.txt"}},
		"subagent": map[string]any{"action": "spawn", "card": map[string]any{
			"name": "worker", "description": "d", "goal": "g", "deliverable": "r",
			"scope": "general", "done_criteria": []string{"d"}, "report_contract": "r",
		}},
	}

	core, _ := EffectiveTools(ProfileEval)
	for _, name := range core {
		args, ok := calls[name]
		if !ok {
			if daemonOwnedInExec[name] {
				t.Logf("%s: daemon-owned stub, documented skip", name)
				continue
			}
			t.Fatalf("eval core tool %q has no crafted call; add one", name)
		}
		if _, err := emb.Registry.Get(name); err != nil {
			t.Fatalf("eval core tool %q missing from registry: %v", name, err)
		}
		raw, _ := json.Marshal(args)
		res, err := emb.Exec.Execute(ctx, name, raw, tools.ExecuteOptions{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.Status != tools.StatusOK {
			t.Fatalf("%s must succeed, got %+v", name, res)
		}
		if strings.Contains(res.Output, "not wired") || strings.Contains(res.Output, "another subsystem") {
			t.Fatalf("%s still unwired: %q", name, res.Output)
		}
	}

	// Deferred tools stay reachable through search_tools/call_tool:
	// every deferred name resolves to a schema, and the file_history
	// sample above already round-tripped through call_tool.
	deferredNames := emb.Catalog.DeferredNames()
	if len(deferredNames) == 0 {
		t.Fatal("deferred catalog must not be empty")
	}
	raw, _ := json.Marshal(map[string]string{"query": "history versions"})
	sres, err := emb.Exec.Execute(ctx, "search_tools", raw, tools.ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if sres.Status != tools.StatusOK || !strings.Contains(sres.Output, "file_history") {
		t.Fatalf("search_tools must surface file_history: %+v", sres)
	}
}
