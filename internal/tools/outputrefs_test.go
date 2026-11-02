package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

// Tests for get_tool_output refs and list mode, the passive status
// tool, stub replies and search_tools catalogue, and the redaction
// write guard.

func getOutput(t *testing.T, g *GetToolOutputTool, args map[string]any) Result {
	t.Helper()
	return fileCall(t, g, args)
}

func TestGetToolOutputAcceptsCallID(t *testing.T) {
	store := NewMemoryOutputStore()
	ref := store.PutTagged(OutputTag{Tool: "exec", Summary: "make test"}, strings.Repeat("log line\n", 40))
	g := &GetToolOutputTool{Outputs: store}
	res := getOutput(t, g, map[string]any{"ref": "call-1", "offset": 0, "limit": 20})
	if res.Status != StatusOK || !strings.Contains(res.Output, "bytes 0..20 of 360") {
		t.Fatalf("call id must resolve to its stored output: %+v", res)
	}
	if direct := getOutput(t, g, map[string]any{"ref": ref, "offset": 0, "limit": 20}); direct.Output != res.Output {
		t.Fatal("a call id and its blob ref must return the same bytes")
	}
}

func TestGetToolOutputListShowsRefsSizesAndProducers(t *testing.T) {
	store := NewMemoryOutputStore()
	first := store.PutTagged(OutputTag{Tool: "exec", Summary: "make test"}, strings.Repeat("a", 500))
	second := store.PutTagged(OutputTag{Tool: "grep", Summary: `{"pattern":"TODO"}`}, strings.Repeat("b", 900))
	// Identical output keeps its first record and call id.
	if again := store.PutTagged(OutputTag{Tool: "exec", Summary: "other"}, strings.Repeat("a", 500)); again != first {
		t.Fatal("identical output must share its ref")
	}
	g := &GetToolOutputTool{Outputs: store}
	res := getOutput(t, g, map[string]any{"list": true})
	if res.Status != StatusOK {
		t.Fatalf("list: %+v", res)
	}
	for _, want := range []string{first, second, "call-1", "call-2", "500 bytes", "900 bytes", "make test", "grep"} {
		if !strings.Contains(res.Output, want) {
			t.Fatalf("list must show %q: %q", want, res.Output)
		}
	}
	if strings.Index(res.Output, "call-2") > strings.Index(res.Output, "call-1") {
		t.Fatalf("list must be newest first: %q", res.Output)
	}
}

func TestGetToolOutputListIncludesSessions(t *testing.T) {
	sess := NewSessionManager(TerminalDeps{})
	s, _, _ := sess.Start("default", "printf 'one\\ntwo\\n'", t.TempDir(), false, 1000, nil)
	g := &GetToolOutputTool{Sessions: sess}
	res := getOutput(t, g, map[string]any{"list": true})
	if !strings.Contains(res.Output, string(s.ID)) || !strings.Contains(res.Output, "printf") {
		t.Fatalf("list must show terminal sessions with their command: %q", res.Output)
	}
}

func TestGetToolOutputSessionRefPagesFullLog(t *testing.T) {
	sess := NewSessionManager(TerminalDeps{})
	s, _, _ := sess.Start("default", "printf 'abcdefghij'", t.TempDir(), false, 1000, nil)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if out, _, _, _ := sess.Read(s.ID, 0); strings.Contains(out, "abcdefghij") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	g := &GetToolOutputTool{Sessions: sess}
	res := getOutput(t, g, map[string]any{"ref": string(s.ID), "offset": 2, "limit": 3})
	if res.Status != StatusOK || !strings.Contains(res.Output, "bytes 2..5 of 10") || !strings.Contains(res.Output, "cde") {
		t.Fatalf("session ref must page its full log: %+v", res)
	}
}

func TestGetToolOutputRequiresRefOrListAndNamesForms(t *testing.T) {
	g := &GetToolOutputTool{Outputs: NewMemoryOutputStore()}
	res := getOutput(t, g, map[string]any{})
	if res.Status != StatusInputError || !strings.Contains(res.Output, "call-N") || !strings.Contains(res.Output, `"list": true`) {
		t.Fatalf("missing ref must name the valid forms: %+v", res)
	}
	res = getOutput(t, g, map[string]any{"ref": "blob-sha256:nope"})
	if res.Status != StatusInputError || !strings.Contains(res.Output, "call-N") || !strings.Contains(res.Output, "list") {
		t.Fatalf("unknown ref must name the valid forms: %+v", res)
	}
}

func TestGetToolOutputSwitchOffKeepsLegacyRefs(t *testing.T) {
	t.Setenv("EXCALIBUR_TOOL_OUTPUT_REFS", "off")
	store := NewMemoryOutputStore()
	store.PutTagged(OutputTag{Tool: "exec"}, "payload")
	g := &GetToolOutputTool{Outputs: store}
	if res := getOutput(t, g, map[string]any{"ref": "call-1"}); res.Status != StatusInputError {
		t.Fatalf("call ids are unknown with the switch off: %+v", res)
	}
	if res := getOutput(t, g, map[string]any{"list": true}); res.Status != StatusInputError {
		t.Fatalf("list needs a ref with the switch off: %+v", res)
	}
}

func TestExecutorRecordsProducerOfStoredOutput(t *testing.T) {
	def := echoDef()
	def.OutputBudget = 100
	reg := scriptedRegistry(def, func(_ context.Context, _ Invocation) (Result, error) {
		return Result{Status: StatusOK, Output: strings.Repeat("z", 1000)}, nil
	})
	ex := NewExecutor(reg)
	res, _ := ex.Execute(context.Background(), "echo_test", json.RawMessage(`{"msg":"hello"}`), ExecuteOptions{})
	if !res.Truncated {
		t.Fatalf("output must be stored: %+v", res)
	}
	g := &GetToolOutputTool{Outputs: ex.Outputs}
	list := getOutput(t, g, map[string]any{"list": true})
	if !strings.Contains(list.Output, res.BlobRef) || !strings.Contains(list.Output, "echo_test") || !strings.Contains(list.Output, `"msg":"hello"`) {
		t.Fatalf("list must name the producing call: %q", list.Output)
	}
}

// waitForSessionOutput polls until the session has printed want.
func waitForSessionOutput(t *testing.T, m *SessionManager, id SessionID, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if out, _, _, _ := m.Read(id, 0); strings.Contains(out, want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session %s never printed %q", id, want)
}

func TestStatusShowsOwnLastThreeLines(t *testing.T) {
	m := NewSessionManager(TerminalDeps{})
	s, _, _ := m.Start("default", "printf 'one\\ntwo\\nthree\\nfour\\n'", t.TempDir(), false, 1000, nil)
	waitForSessionOutput(t, m, s.ID, "four")
	st := &StatusTool{Sessions: m}
	res, _ := st.Invoke(context.Background(), Invocation{})
	for _, want := range []string{"tail: two", "tail: three", "tail: four"} {
		if !strings.Contains(res.Output, want) {
			t.Fatalf("status must show the last three lines (%s): %q", want, res.Output)
		}
	}
	if strings.Contains(res.Output, "tail: one") {
		t.Fatalf("status must not show older lines: %q", res.Output)
	}
}

func TestStatusIsReadOnlyInEveryMode(t *testing.T) {
	// Status never installs (it only reports), so it is read-only even
	// under auto provisioning; there is no legacy side-effect mode.
	m := NewSessionManager(TerminalDeps{})
	tool := &StatusTool{Sessions: m, Provisioner: &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("apt-get")}}
	if def := tool.Definition(); def.Effect != "read_only" || !def.Fast {
		t.Fatalf("status must stay read-only: %+v", def)
	}
}

func TestUnwiredStubAnswersWithoutError(t *testing.T) {
	var ack Definition
	for _, d := range CoreStubDefinitions() {
		if d.Name == "ack_directive" {
			ack = d
		}
	}
	stub := &StubTool{Def: ack}
	res, _ := stub.Invoke(context.Background(), Invocation{})
	if res.Status != StatusOK || !strings.Contains(res.Output, "no directives are pending") {
		t.Fatalf("ack_directive without a backend must report no pending directives: %+v", res)
	}
}

func TestUnwiredStubNeverClaimsAnotherOwner(t *testing.T) {
	for _, d := range CoreStubDefinitions() {
		res, _ := (&StubTool{Def: d}).Invoke(context.Background(), Invocation{})
		if strings.Contains(res.Output, "another subsystem") || strings.Contains(res.Output, "not wired") {
			t.Fatalf("%s must not name an owner subsystem: %q", d.Name, res.Output)
		}
		says := []string{"nothing was done", "nothing to acknowledge", "nothing was saved"}
		said := false
		for _, phrase := range says {
			if strings.Contains(res.Output, phrase) {
				said = true
			}
		}
		if res.Status != StatusOK || !said {
			t.Fatalf("%s must answer with a non-error statement that says what happened: %+v", d.Name, res)
		}
	}
}

func TestUnwiredStubSwitchOffKeepsError(t *testing.T) {
	t.Setenv("EXCALIBUR_STUB_REPLIES", "off")
	res, _ := (&StubTool{Def: CoreStubDefinitions()[0]}).Invoke(context.Background(), Invocation{})
	if res.Status != StatusToolError || strings.Contains(res.Output, "another subsystem") {
		t.Fatalf("switch off returns an error without the owner wording: %+v", res)
	}
}

func deferredEvalCatalog(t *testing.T) (*MapRegistry, *DeferredCatalog) {
	t.Helper()
	reg := NewMapRegistry()
	if err := RegisterBuiltins(reg, nil); err != nil {
		t.Fatal(err)
	}
	cat := NewDeferredCatalog(reg)
	if err := RegisterDeferredMedia(reg, cat, &MediaDeps{}); err != nil {
		t.Fatal(err)
	}
	return reg, cat
}

func TestSearchToolsEmptyQueryListsCatalogue(t *testing.T) {
	_, cat := deferredEvalCatalog(t)
	res, _ := (&SearchToolsTool{Catalog: cat}).Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"query":""}`)})
	if res.Status != StatusOK {
		t.Fatalf("empty query: %+v", res)
	}
	for _, want := range []string{"- document_read: ", "- vision_extract: ", "- pdf_ops: "} {
		if !strings.Contains(res.Output, want) {
			t.Fatalf("catalogue must list %q with a one-line description: %q", want, res.Output)
		}
	}
	if strings.Contains(res.Output, "input_schema") {
		t.Fatal("the catalogue lists names and one-line purposes, not schemas")
	}
}

func TestSearchToolsEmptyQuerySwitchOffKeepsPopularList(t *testing.T) {
	t.Setenv("EXCALIBUR_STUB_REPLIES", "off")
	_, cat := deferredEvalCatalog(t)
	res, _ := (&SearchToolsTool{Catalog: cat}).Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"query":""}`)})
	if !strings.Contains(res.Output, "input_schema") {
		t.Fatalf("switch off keeps the earlier schema list: %q", res.Output)
	}
}

func TestCallToolRunsDirectTool(t *testing.T) {
	reg, cat := deferredEvalCatalog(t)
	ex := NewExecutor(reg)
	call := &CallToolTool{Catalog: cat, Executor: ex}
	res, err := call.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"name":"clock","arguments":{"action":"now","zone":"UTC"}}`)})
	if err != nil || res.Status != StatusOK || !strings.Contains(res.Output, "UTC") {
		t.Fatalf("call_tool naming a direct tool must run it: %v %+v", err, res)
	}
}

// --- redaction write guard ---

// guardRegistry registers the write tools plus a recording exec stub.
// execRuns counts how many times the exec tool actually ran.
func guardRegistry(t *testing.T, execRuns *int) (*MapRegistry, *Executor) {
	t.Helper()
	reg := NewMapRegistry()
	deps := newFileTestDeps()
	for _, tool := range []Tool{&WriteFileTool{Deps: deps}, &EditFileTool{Deps: deps}, &ApplyPatchTool{Deps: deps}} {
		if err := reg.Register(tool); err != nil {
			t.Fatal(err)
		}
	}
	execDef := echoDef()
	execDef.Name = "exec"
	execDef.InputSchema = json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`)
	execFake := &funcTool{def: execDef, fn: func(_ context.Context, _ Invocation) (Result, error) {
		*execRuns++
		return Result{Status: StatusOK, Output: "ran"}, nil
	}}
	if err := reg.Register(execFake); err != nil {
		t.Fatal(err)
	}
	ex := NewExecutor(reg)
	if err := reg.Register(&CallToolTool{Executor: ex}); err != nil {
		t.Fatal(err)
	}
	return reg, ex
}

const guardMarker = vault.RedactPatternMarker

func TestRedactionWriteGuardRefusesMarkerWrites(t *testing.T) {
	runs := 0
	_, ex := guardRegistry(t, &runs)
	ctx := context.Background()
	dir := t.TempDir()
	target := filepath.Join(dir, "config.env")

	raw, _ := json.Marshal(map[string]any{"path": target, "content": "TOKEN=" + guardMarker + "\n"})
	res, _ := ex.Execute(ctx, "write_file", raw, ExecuteOptions{})
	if res.Status != StatusInputError || !strings.Contains(res.Output, "redaction marker") || !strings.Contains(res.Output, "sed -E") {
		t.Fatalf("write_file with a marker must be refused: %+v", res)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("a refused write must not create the file")
	}

	existing := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(existing, []byte("TOKEN=real-value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(map[string]any{"path": existing, "old_text": "TOKEN=real-value", "new_text": "TOKEN=" + guardMarker})
	res, _ = ex.Execute(ctx, "edit_file", raw, ExecuteOptions{})
	if res.Status != StatusInputError || !strings.Contains(res.Output, "redaction marker") {
		t.Fatalf("edit_file with a marker in new_text must be refused: %+v", res)
	}
	raw, _ = json.Marshal(map[string]any{"path": existing, "old_text": guardMarker, "new_text": "x"})
	res, _ = ex.Execute(ctx, "edit_file", raw, ExecuteOptions{})
	if res.Status != StatusInputError {
		t.Fatalf("replacing a marker-containing old_text must be refused: %+v", res)
	}
	if data, _ := os.ReadFile(existing); string(data) != "TOKEN=real-value\n" {
		t.Fatalf("refused edits must not touch the file: %q", data)
	}

	patch, _ := json.Marshal(map[string]any{"ops": []map[string]any{{"path": existing, "op": "update", "old_text": "real-value", "new_text": guardMarker}}})
	res, _ = ex.Execute(ctx, "apply_patch", patch, ExecuteOptions{})
	if res.Status != StatusInputError {
		t.Fatalf("apply_patch with a marker must be refused: %+v", res)
	}

	raw, _ = json.Marshal(map[string]any{"command": "echo " + guardMarker})
	res, _ = ex.Execute(ctx, "exec", raw, ExecuteOptions{})
	if res.Status != StatusInputError || runs != 0 {
		t.Fatalf("exec with a marker must be refused before it runs (runs=%d): %+v", runs, res)
	}

	nested, _ := json.Marshal(map[string]any{"name": "write_file", "arguments": map[string]any{"path": target, "content": guardMarker}})
	res, _ = ex.Execute(ctx, "call_tool", nested, ExecuteOptions{})
	if !strings.Contains(res.Output, "redaction marker") {
		t.Fatalf("a nested call_tool write with a marker must be refused: %+v", res)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("nested refused write must not create the file")
	}
}

func TestRedactionWriteGuardAllowsOrdinaryWrites(t *testing.T) {
	runs := 0
	_, ex := guardRegistry(t, &runs)
	raw, _ := json.Marshal(map[string]any{"path": filepath.Join(t.TempDir(), "plain.txt"), "content": "no markers here"})
	if res, _ := ex.Execute(context.Background(), "write_file", raw, ExecuteOptions{}); res.Status != StatusOK {
		t.Fatalf("ordinary write must pass: %+v", res)
	}
}

func TestRedactionWriteGuardSwitchOff(t *testing.T) {
	t.Setenv("EXCALIBUR_REDACTION_WRITE_GUARD", "off")
	runs := 0
	_, ex := guardRegistry(t, &runs)
	target := filepath.Join(t.TempDir(), "legacy.txt")
	raw, _ := json.Marshal(map[string]any{"path": target, "content": guardMarker})
	if res, _ := ex.Execute(context.Background(), "write_file", raw, ExecuteOptions{}); res.Status != StatusOK {
		t.Fatalf("switch off restores the earlier write behaviour: %+v", res)
	}
}

func TestObservationCallIDRetrievesOriginalOutput(t *testing.T) {
	for _, size := range []int{80, 2000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			def := echoDef()
			def.OutputBudget = 1000
			full := strings.Repeat("x", size)
			reg := scriptedRegistry(def, func(context.Context, Invocation) (Result, error) { return Result{Status: StatusOK, Output: full}, nil })
			ex := NewExecutor(reg)
			id := "observation-alpha"
			_, err := ex.Execute(context.Background(), "echo_test", json.RawMessage(`{"msg":"hello"}`), ExecuteOptions{CallID: id})
			if err != nil {
				t.Fatal(err)
			}
			g := &GetToolOutputTool{Outputs: ex.Outputs}
			retrieved := getOutput(t, g, map[string]any{"ref": id, "limit": 4000})
			if retrieved.Status != StatusOK || !strings.HasSuffix(retrieved.Output, full) {
				t.Fatalf("original call output unavailable: %+v", retrieved)
			}
			store := ex.Outputs.(*MemoryOutputStore)
			store.BindCall(id, store.Put("[observation omitted]"))
			again := getOutput(t, g, map[string]any{"ref": id, "limit": 4000})
			if again.Output != retrieved.Output {
				t.Fatal("marker replaced immutable full output")
			}
		})
	}
}
