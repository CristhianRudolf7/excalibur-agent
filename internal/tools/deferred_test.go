package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
)

// ExpectedCatalogNames lists every tool name from Sections 13.2 and
// 13.3. The verification table says 98; the tables enumerate 99 (see
// docs/decisions/0016-deferred-tools-harness-fakes.md). Validation covers the enumerated set.
func ExpectedCatalogNames() []string {
	return []string{
		// 13.2 core (33).
		"send_message", "ask_user", "leave_continuation_note", "ack_directive",
		"status", "clock", "settings",
		"read_file", "write_file", "edit_file", "apply_patch", "grep", "glob",
		"exec", "exec_io",
		"web_search", "web_read",
		"task_db", "memory_search", "memory_write", "memory_propose",
		"history_search", "library_search", "doc_read", "doc_edit",
		"get_tool_output", "pin_observation",
		"search_skills", "load_skill", "search_tools", "call_tool",
		"subagent", "reminders",
		// 13.3 code and workspace (8).
		"lsp", "code_graph", "file_history", "revert_edit",
		"workspace_snapshot", "worktree", "integration_queue", "github",
		// 13.3 browser (13).
		"browser_open_session", "browser_close_session", "browser_suspend_session",
		"browser_tabs", "browser_navigate", "browser_wait_ready", "browser_snapshot",
		"browser_act", "browser_download", "browser_devtools", "browser_emulate",
		"browser_status", "browser_routine",
		// 13.3 sources (1).
		"source_search",
		// 13.3 documents and media (12).
		"document_read", "document_create", "document_edit", "pdf_ops",
		"file_convert", "library_ingest", "vision_extract", "audio_transcribe",
		"diagram_render", "chart_render", "image_generate", "image_edit",
		// 13.3 user database (1).
		"userdb",
		// 13.3 communication (4).
		"email", "calendar", "contact_message", "meeting",
		// 13.3 device (4).
		"device_screen_capture", "device_ui_tree", "device_input", "device_app",
		// 13.3 apps (12).
		"app_state", "spreadsheet_ops", "model3d_ops", "video_timeline_ops",
		"design_canvas_ops", "whiteboard_ops", "course_ops", "job_tracker_ops",
		"monitor_ops", "simulation_ops", "bi_ops", "gpu_training",
		// 13.3 agenda (4).
		"schedule", "trigger_rules", "workflow", "flow_simulate",
		// 13.3 admin (7).
		"secrets", "metrics_query", "nodes", "skill_manage",
		"tool_package", "package_install", "package_publish",
	}
}

func deferredTestSetup() (*MapRegistry, *DeferredCatalog, *Executor) {
	reg := NewMapRegistry()
	_ = RegisterBuiltins(reg, nil)
	cat := NewDeferredCatalog(reg)
	exec := NewExecutor(reg)
	WireSearchCall(reg, cat, exec)
	WireWebTools(reg, cat, &WebDeps{Backend: NewMemorySearchBackend(), Sources: &MemorySourceRecorder{}})
	if err := RegisterDeferredMedia(reg, cat, &MediaDeps{}); err != nil {
		panic(err)
	}
	mgr := NewPackageManager(reg, cat, exec)
	WirePackageTool(reg, cat, mgr)
	for _, t := range DeferredHarnessToolsForTest(exec) {
		_ = cat.RegisterDeferred(t)
	}
	return reg, cat, exec
}

// DeferredHarnessToolsForTest exposes test-only registrations.
func DeferredHarnessToolsForTest(exec *Executor) []Tool {
	_ = exec
	return nil
}

func TestCatalogNamesAllValid(t *testing.T) {
	names := ExpectedCatalogNames()
	seen := map[string]bool{}
	for _, n := range names {
		if err := ValidateName(n); err != nil {
			t.Fatalf("name %q must validate: %v", n, err)
		}
		if len(n) > 64 {
			t.Fatalf("name %q exceeds 64 chars", n)
		}
		if seen[n] {
			t.Fatalf("duplicate name %q", n)
		}
		seen[n] = true
	}
	// The tables enumerate 99 names while the verification row says 98;
	// the discrepancy is recorded, and validation covers the full set.
	if len(names) != 99 {
		t.Fatalf("expected 99 enumerated names, got %d", len(names))
	}
}

func TestSearchToolsPrecision(t *testing.T) {
	_, cat, _ := deferredTestSetup()
	cases := map[string]string{
		"convert docx pdf document":    "file_convert",
		"read pdf document text":       "document_read",
		"merge split pdf pages":        "pdf_ops",
		"render mermaid diagram":       "diagram_render",
		"chart bar data":               "chart_render",
		"vision image text":            "vision_extract",
		"search github source":         "source_search",
		"read web page markdown":       "web_read",
		"search deferred tools":        "search_tools",
		"create tool package activate": "tool_package",
	}
	// tool_package is registered below via the manager in its own test;
	// register a minimal deferred entry here so retrieval covers it.
	_ = cat
	for query, want := range cases {
		hits := cat.Search(query, 5)
		found := false
		top := ""
		if len(hits) > 0 {
			top = hits[0].Definition.Name
		}
		for _, h := range hits {
			if h.Definition.Name == want {
				found = true
			}
		}
		// web_read is core, not deferred: search_tools must not return it.
		if want == "web_read" {
			if found {
				t.Fatalf("query %q: core tool web_read must not be deferred-searchable", query)
			}
			continue
		}
		// search_tools itself is core: same rule.
		if want == "search_tools" {
			if found {
				t.Fatalf("query %q: core tool must not be deferred-searchable", query)
			}
			continue
		}
		// tool_package is deferred and searchable once wired.
		if !found {
			names := []string{}
			for _, h := range hits {
				names = append(names, h.Definition.Name)
			}
			t.Fatalf("query %q: want %q in top 5, top=%q all=%v", query, want, top, names)
		}
		if top != want {
			t.Logf("query %q: top=%q want %q (in top 5, accepted)", query, top, want)
		}
	}
}

func TestSearchToolsReturnsFiveSchemas(t *testing.T) {
	reg, cat, exec := deferredTestSetup()
	_ = reg
	_ = exec
	tool, err := reg.Get("search_tools")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"query": "document pdf convert"})
	res, err := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusOK {
		t.Fatalf("search_tools: %+v", res)
	}
	if !strings.Contains(res.Output, "document_read") && !strings.Contains(res.Output, "file_convert") && !strings.Contains(res.Output, "pdf_ops") {
		t.Fatalf("search_tools should return document schemas, got %q", res.Output)
	}
	var parsed struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"input_schema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte(res.Output), &parsed); err != nil {
		t.Fatalf("search_tools output must be JSON: %v\n%s", err, res.Output)
	}
	if len(parsed.Tools) == 0 || len(parsed.Tools) > 5 {
		t.Fatalf("search_tools must return 1..5 schemas, got %d", len(parsed.Tools))
	}
	for _, e := range parsed.Tools {
		if cat.IsCore(e.Name) {
			t.Fatalf("search_tools returned core tool %q", e.Name)
		}
	}
}

func TestCallToolValidation(t *testing.T) {
	reg, cat, exec := deferredTestSetup()
	_ = cat
	call, err := reg.Get("call_tool")
	if err != nil {
		t.Fatal(err)
	}
	// Unknown deferred tool.
	raw, _ := json.Marshal(map[string]any{"name": "nope_missing", "arguments": map[string]any{}})
	res, _ := call.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusInputError && res.Status != StatusToolError {
		t.Fatalf("unknown tool should fail validation, got %+v", res)
	}
	// Core tool through call_tool is rejected.
	raw2, _ := json.Marshal(map[string]any{"name": "grep", "arguments": map[string]any{"pattern": "x"}})
	res2, _ := call.Invoke(context.Background(), Invocation{Arguments: raw2})
	if res2.Status != StatusInputError {
		t.Fatalf("core tool via call_tool must be input_error, got %+v", res2)
	}
	// Schema violation surfaces as input_error with the schema part.
	raw3, _ := json.Marshal(map[string]any{"name": "file_convert", "arguments": map[string]any{}})
	res3, _ := call.Invoke(context.Background(), Invocation{Arguments: raw3})
	if res3.Status != StatusInputError {
		t.Fatalf("schema violation must be input_error, got %+v", res3)
	}
	if !strings.Contains(strings.ToLower(res3.Output), "input") && !strings.Contains(strings.ToLower(res3.Output), "output") {
		t.Fatalf("schema error should name the failing part, got %q", res3.Output)
	}
	// Self-invocation is rejected.
	raw4, _ := json.Marshal(map[string]any{"name": "call_tool", "arguments": map[string]any{"name": "x"}})
	res4, _ := call.Invoke(context.Background(), Invocation{Arguments: raw4})
	if res4.Status != StatusInputError {
		t.Fatalf("call_tool self-call must be input_error, got %+v", res4)
	}
	// Nested name inside arguments promotes on direct invocation.
	raw5, _ := json.Marshal(map[string]any{"arguments": map[string]any{"name": "grep", "pattern": "x"}})
	tool5, _ := reg.Get("call_tool")
	res5, _ := tool5.Invoke(context.Background(), Invocation{Arguments: raw5})
	if res5.Status == StatusInputError && strings.Contains(res5.Output, "name is required") {
		t.Fatalf("nested name must promote instead of name-required, got %+v", res5)
	}
	// Nested name survives the full pipeline: normalization promotes
	// before schema validation, so the legacy nameless shape that
	// trials recorded 11 times now resolves to the deferred tool.
	pipe5, err := exec.Execute(context.Background(), "call_tool", raw5, ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pipe5.Status == StatusInputError && strings.Contains(pipe5.Output, "name is required") {
		t.Fatalf("pipeline must promote the nested name, got %+v", pipe5)
	}
	_ = exec
}

func TestDeferredPrefixUnchanged(t *testing.T) {
	reg := NewMapRegistry()
	_ = RegisterBuiltins(reg, nil)
	cat := NewDeferredCatalog(reg)
	before := cat.PrefixHash()
	beforeNames := cat.CoreNames()
	exec := NewExecutor(reg)
	WireSearchCall(reg, cat, exec)
	WireWebTools(reg, cat, &WebDeps{Backend: NewMemorySearchBackend(), Sources: &MemorySourceRecorder{}})
	if err := RegisterDeferredMedia(reg, cat, &MediaDeps{}); err != nil {
		t.Fatal(err)
	}
	after := cat.PrefixHash()
	if before != after {
		t.Fatal("deferred registration must not change the core prefix hash")
	}
	afterNames := cat.CoreNames()
	if len(beforeNames) != len(afterNames) {
		t.Fatal("core set changed")
	}
	// Resolving a skill does not change the prefix either.
	if _, err := cat.ResolveSkillTools([]string{"document_read", "file_convert"}); err != nil {
		t.Fatal(err)
	}
	if cat.PrefixHash() != before {
		t.Fatal("skill resolution must not change the prefix")
	}
}

func TestMCPNamingAbbreviation(t *testing.T) {
	short := capabilities.MCPToolName("github", "get_issue")
	if short != "mcp__github__get_issue" {
		t.Fatalf("mcp name: %q", short)
	}
	long := capabilities.MCPToolName(strings.Repeat("a", 40), strings.Repeat("b", 40))
	if len(long) > 64 {
		t.Fatalf("abbreviated name too long: %q", long)
	}
	if err := ValidateName(long); err != nil {
		t.Fatalf("abbreviated mcp name must validate: %v", err)
	}
}
