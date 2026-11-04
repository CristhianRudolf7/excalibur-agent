package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
)

// Section 14.13 verification: every row runs against simulations that
// honor the capability contracts, until the real ones exist.

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func extSetup() (*MapRegistry, *DeferredCatalog, *Executor, *Loader) {
	reg := NewMapRegistry()
	if err := RegisterBuiltins(reg, nil); err != nil {
		panic(err)
	}
	cat := NewDeferredCatalog(reg)
	ex := NewExecutor(reg)
	WireSearchCall(reg, cat, ex)
	return reg, cat, ex, NewLoader(reg, cat, ex)
}

func writeManifest(t *testing.T, dir string, m Manifest) {
	t.Helper()
	raw, err := marshalManifestYAML(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ExtensionFileName), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func probesFor(t *testing.T, dir string) []ConformanceProbe {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "probes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var probes []ConformanceProbe
	if err := json.Unmarshal(raw, &probes); err != nil {
		t.Fatal(err)
	}
	return probes
}

func invoke(t *testing.T, exec *Executor, tool string, args map[string]any) Result {
	t.Helper()
	raw, _ := json.Marshal(args)
	res, err := exec.Execute(context.Background(), tool, raw, ExecuteOptions{IdempotencyKey: "h13-" + tool})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return res
}

// Row 1: one extension per route appears in search_tools without a
// restart or recompile, the fixed prefix never changes, and all four
// run through the same pipeline.
func TestFourRoutesHotAdd(t *testing.T) {
	if _, err := osexec.LookPath("go"); err != nil {
		t.Skip("go toolchain is required to build the route-1 fixture")
	}
	root := repoRoot(t)
	_, cat, exec, loader := extSetup()
	defer loader.Close()
	before := cat.PrefixHash()

	// Route 4: builtin.
	builtinDir := filepath.Join(root, "extensions", "example-builtin")
	loader.ProvideBuiltin("example-builtin", echoDoubles(t, builtinDir))
	builtinExt, err := loader.LoadDir(builtinDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := loader.Activate(builtinExt); err != nil {
		t.Fatal(err)
	}

	// Route 3: package.
	packageExt, err := loader.LoadDir(filepath.Join(root, "extensions", "example-package"))
	if err != nil {
		t.Fatal(err)
	}
	if err := loader.Activate(packageExt); err != nil {
		t.Fatal(err)
	}

	// Route 2: app.
	appDir := filepath.Join(root, "extensions", "example-app")
	loader.BindApp("example-app", NewMemoryAppChannel())
	appExt, err := loader.LoadDir(appDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := loader.Activate(appExt); err != nil {
		t.Fatal(err)
	}

	// Route 1: MCP server, built to a temp dir and dialed by command.
	// Only the manifest files travel to the work dir; the server
	// binary is built there from the repo sources.
	mcpSrc := filepath.Join(root, "extensions", "example-mcp")
	work := t.TempDir()
	for _, name := range []string{ExtensionFileName, "probes.json"} {
		raw, err := os.ReadFile(filepath.Join(mcpSrc, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(work, name), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	serverName := "server"
	buildArgs := []string{"build"}
	if runtime.GOOS == "windows" {
		serverName += ".exe"
		// Instrument the native fixture too on the Windows race-test host.
		buildArgs = append(buildArgs, "-race")
	}
	buildArgs = append(buildArgs, "-o", filepath.Join(work, serverName), filepath.Join(mcpSrc, "server"))
	build := osexec.Command("go", buildArgs...)
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build example server: %v\n%s", err, out)
	}
	mcpExt, err := loader.LoadDir(work)
	if err != nil {
		t.Fatal(err)
	}
	if err := loader.Activate(mcpExt); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loader.Disable(mcpExt.Manifest.ID) }()

	if got := cat.PrefixHash(); got != before {
		t.Fatal("hot add must not change the fixed prefix hash")
	}
	for _, tc := range []struct{ query, tool string }{
		{"echo in-process fixture", "example_echo"},
		{"greet package hello", "example_greet"},
		{"board move card app", "example_board_move"},
		{"cube volume mcp geometry", "mcp__example_mcp__cube_volume"},
	} {
		found := false
		for _, h := range cat.Search(tc.query, 5) {
			if h.Definition.Name == tc.tool {
				found = true
			}
		}
		if !found {
			t.Fatalf("%q missing from search_tools for %q", tc.tool, tc.query)
		}
	}
	if res := invoke(t, exec, "example_echo", map[string]any{}); res.Status != StatusOK || res.Output != "echo ok" {
		t.Fatalf("builtin route: %+v", res)
	}
	if res := invoke(t, exec, "example_greet", map[string]any{"name": "Ada"}); res.Status != StatusOK || !strings.Contains(res.Output, "Ada") {
		t.Fatalf("package route: %+v", res)
	}
	if res := invoke(t, exec, "example_board_move", map[string]any{"instance": "board-1", "ops": "move card A"}); res.Status != StatusOK {
		t.Fatalf("app route: %+v", res)
	}
	if res := invoke(t, exec, "mcp__example_mcp__cube_volume", map[string]any{"size": 3}); res.Status != StatusOK || !strings.Contains(res.Output, "27") {
		t.Fatalf("mcp route: %+v", res)
	}
	issues := loader.Doctor()
	for _, is := range issues {
		if is.Level != "ok" {
			t.Fatalf("doctor: %+v", is)
		}
	}
}

func echoDoubles(t *testing.T, dir string) []Tool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "builtin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name   string `json:"name"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	// Pair fixtures with the manifest entries by name.
	mraw, err := os.ReadFile(filepath.Join(dir, ExtensionFileName))
	if err != nil {
		t.Fatal(err)
	}
	m, err := loadManifestYAML(mraw)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]string{}
	for _, f := range fixtures {
		byName[f.Name] = f.Output
	}
	out := []Tool{}
	for _, entry := range m.Tools {
		output, ok := byName[entry.Name]
		if !ok {
			t.Fatalf("no fixture for %q", entry.Name)
		}
		out = append(out, NewEchoTool(entry.Definition, output))
	}
	return out
}

// countTool repeats effects: its output changes on every call, so the
// same-key conformance probe must fail it.
type countTool struct {
	def Definition
	n   atomic.Int64
}

func (t *countTool) Definition() Definition { return t.def }
func (t *countTool) Invoke(_ context.Context, _ Invocation) (Result, error) {
	return Result{Status: StatusOK, Output: fmt.Sprintf("call %d", t.n.Add(1))}, nil
}
func (t *countTool) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }

// hangTool ignores cancellation, so the cancel probe must fail it.
type hangTool struct{ def Definition }

func (t *hangTool) Definition() Definition { return t.def }
func (t *hangTool) Invoke(_ context.Context, _ Invocation) (Result, error) {
	time.Sleep(4 * time.Second)
	return Result{Status: StatusOK, Output: "slow"}, nil
}
func (t *hangTool) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }

func badManifest() Manifest {
	mkdef := func(name string) Definition {
		return Definition{
			Name: name, Description: "bad tool " + name,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Effect:      "exec", Idempotency: "keyed",
			Cancel: "cooperative", Timeout: 60_000_000_000, OutputBudget: 1 << 16,
			DryRun:    "would run without effects",
			Platforms: []string{"linux"}, Profiles: []string{"default"},
		}
	}
	return Manifest{
		ID: "bad-ext", Version: "0.1.0", Contract: ExtensionContract,
		Kind: ExtensionBuiltin, Lifecycle: LifecycleLazy,
		Tools: []ExtensionTool{{Definition: mkdef("bad_repeat")}, {Definition: mkdef("bad_hang")}},
	}
}

// Row 2: the conformance suite passes a good extension and fails one
// that repeats effects under the same key or ignores cancellation;
// the bad one never activates.
func TestConformanceGate(t *testing.T) {
	root := repoRoot(t)
	_, _, _, loader := extSetup()
	defer loader.Close()

	goodDir := filepath.Join(root, "extensions", "example-builtin")
	loader.ProvideBuiltin("example-builtin", echoDoubles(t, goodDir))
	good, err := loader.LoadDir(goodDir)
	if err != nil {
		t.Fatal(err)
	}
	report, err := loader.ActivateChecked(good, probesFor(t, goodDir))
	if err != nil || !report.Passed {
		t.Fatalf("good extension must pass: %v\n%s", err, FormatReport(report))
	}

	badDir := t.TempDir()
	writeManifest(t, badDir, badManifest())
	loader.ProvideBuiltin("bad-ext", []Tool{&countTool{def: badManifest().Tools[0].Definition}, &hangTool{def: badManifest().Tools[1].Definition}})
	bad, err := loader.LoadDir(badDir)
	if err != nil {
		t.Fatal(err)
	}
	probes := []ConformanceProbe{{Tool: "bad_repeat", Args: json.RawMessage(`{}`)}, {Tool: "bad_hang", Args: json.RawMessage(`{}`)}}
	report, err = loader.ActivateChecked(bad, probes)
	if err == nil || report.Passed {
		t.Fatalf("bad extension must fail and stay out:\n%s", FormatReport(report))
	}
	text := FormatReport(report)
	if !strings.Contains(text, "bad_repeat") || !strings.Contains(text, "bad_hang") {
		t.Fatalf("report must name the exact breach:\n%s", text)
	}
	for _, name := range []string{"bad_repeat", "bad_hang"} {
		if _, gerr := loader.reg.Get(name); gerr == nil {
			t.Fatalf("failing tool %q must not activate", name)
		}
	}
}

// Row 3: a route-2 app emitting a manual change while the agent works
// delivers the event, and the next operation starts from updated state.
func TestAppManualChangeSync(t *testing.T) {
	ch := NewMemoryAppChannel()
	ch.Apps().Register("board-9", "generic")
	if _, err := ch.Invoke(context.Background(), "example_board_move",
		json.RawMessage(`{"instance":"board-9","ops":"move card A to doing"}`), "k1"); err != nil {
		t.Fatal(err)
	}
	seq := ch.Apps().EmitUserChange("board-9", "user moved card B to done")
	delta := ch.EventsSince("board-9", seq-1)
	if len(delta) != 1 || delta[0].Actor != "user" || !strings.Contains(delta[0].Change, "card B") {
		t.Fatalf("manual change must arrive as a delta event: %+v", delta)
	}
	res, err := ch.Invoke(context.Background(), "example_board_move",
		json.RawMessage(`{"instance":"board-9","ops":"move card C"}`), "k2")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != capabilities.StateCompleted {
		t.Fatalf("next op must start from updated state: %+v", res)
	}
	state, err := ch.Apps().Invoke("app_state", json.RawMessage(`{"instance":"board-9"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(state.Payload), "move card C") {
		t.Fatalf("state must reflect the latest op: %s", state.Payload)
	}
}

// Row 4: a long operation in working persists its task id; after a
// restart the harness re-polls by id without repeating the effect.
func TestDurableTaskResume(t *testing.T) {
	b := capabilities.NewBrowserFake()
	open, _ := b.Invoke("browser_open_session", json.RawMessage(`{}`), "k")
	var opened map[string]any
	_ = json.Unmarshal(open.Payload, &opened)
	sess := opened["session_id"].(string)
	tab, _ := b.Invoke("browser_tabs", json.RawMessage(`{"session_id":"`+sess+`","action":"new","url":"https://x.example/captcha"}`), "k")
	var tabbed map[string]any
	_ = json.Unmarshal(tab.Payload, &tabbed)
	nav, _ := b.Invoke("browser_navigate", json.RawMessage(`{"session_id":"`+sess+`","tab_id":"`+tabbed["tab_id"].(string)+`","action":"go","url":"https://x.example/captcha"}`), "k-nav")
	if nav.State != capabilities.StateWorking {
		t.Fatalf("want working: %+v", nav)
	}
	// The journal persists this record across the restart.
	rec := capabilities.NewTaskRecord("browser", "browser_navigate", nav.TaskID, "k-nav", nil, time.Now())
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var restored capabilities.TaskRecord
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.TaskID != nav.TaskID || restored.IdempotencyKey != "k-nav" {
		t.Fatalf("task record must round-trip: %+v", restored)
	}
	resumed, _ := b.Poll(restored.TaskID)
	if resumed.State != capabilities.StateInputRequired {
		t.Fatalf("resume must re-poll to input_required: %+v", resumed)
	}
	done, _ := b.Update(restored.TaskID, json.RawMessage(`{"ok":true}`))
	if done.State != capabilities.StateCompleted {
		t.Fatalf("update must complete: %+v", done)
	}
	if got := b.Effects("browser_navigate"); got != 1 {
		t.Fatalf("the effect must happen once, got %d", got)
	}
}

// gateTool blocks until released, for the update-gating test.
type gateTool struct {
	def     Definition
	release chan struct{}
}

func (t *gateTool) Definition() Definition { return t.def }
func (t *gateTool) Invoke(ctx context.Context, _ Invocation) (Result, error) {
	select {
	case <-t.release:
		return Result{Status: StatusOK, Output: "released"}, nil
	case <-ctx.Done():
		return Result{Status: StatusCanceled, Output: "canceled"}, nil
	}
}
func (t *gateTool) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }

// Row 5: a new version waits for in-flight calls; an unsupported
// contract is rejected with its reason.
func TestUpdateGatedAndContractRejected(t *testing.T) {
	reg, _, exec, loader := extSetup()
	_ = exec
	defer loader.Close()
	dir := t.TempDir()
	def := Definition{
		Name: "gated_op", Description: "gated op",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Effect:      "exec", Idempotency: "keyed", Cancel: "graceful",
		Timeout: 60_000_000_000, OutputBudget: 1 << 16, DryRun: "would run",
		Platforms: []string{"linux"}, Profiles: []string{"default"},
	}
	writeManifest(t, dir, Manifest{ID: "gated", Version: "0.1.0", Contract: ExtensionContract,
		Kind: ExtensionBuiltin, Tools: []ExtensionTool{{Definition: def}}})
	release := make(chan struct{})
	loader.ProvideBuiltin("gated", []Tool{&gateTool{def: def, release: release}})
	ext, err := loader.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := loader.Activate(ext); err != nil {
		t.Fatal(err)
	}
	done := make(chan Result, 1)
	go func() {
		res, _ := exec.Execute(context.Background(), "gated_op", json.RawMessage(`{}`), ExecuteOptions{})
		done <- res
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if got, ok := loader.Get("gated"); ok && got.InFlight() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("call never went in flight")
		}
		time.Sleep(10 * time.Millisecond)
	}
	writeManifest(t, dir, Manifest{ID: "gated", Version: "0.2.0", Contract: ExtensionContract,
		Kind: ExtensionBuiltin, Tools: []ExtensionTool{{Definition: def}}})
	if status, err := loader.Update(dir); err == nil || status != "pending" {
		t.Fatalf("update with calls in flight must stay pending: %q %v", status, err)
	}
	close(release)
	if res := <-done; res.Status != StatusOK {
		t.Fatalf("gated call: %+v", res)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		if status, err := loader.Update(dir); err == nil && status == "activated" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("update never activated after the calls drained")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := reg.Get("gated_op"); err != nil {
		t.Fatalf("new version must serve: %v", err)
	}
	writeManifest(t, dir, Manifest{ID: "gated", Version: "9.0.0", Contract: "excalibur-ext/9",
		Kind: ExtensionBuiltin, Tools: []ExtensionTool{{Definition: def}}})
	if _, err := loader.LoadDir(dir); err == nil || !strings.Contains(err.Error(), "excalibur-ext/9") {
		t.Fatalf("unsupported contract must be rejected with its reason: %v", err)
	}
}

// Row 6 (surfacing): a blocked capability operation reaches the tool
// result as input_required for the engine ask_user.
func TestCapabilityInputRequiredSurfacing(t *testing.T) {
	reg := NewMapRegistry()
	b := capabilities.NewBrowserFake()
	tools, err := ToolsFromCapability(capabilityShim{Capability: b})
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range tools {
		if err := reg.Register(tl); err != nil {
			t.Fatal(err)
		}
	}
	ex := NewExecutor(reg)
	open, _ := ex.Execute(context.Background(), "browser_open_session", json.RawMessage(`{"profile":"t"}`), ExecuteOptions{})
	var opened map[string]any
	_ = json.Unmarshal([]byte(open.Output), &opened)
	sess := opened["session_id"].(string)
	tabbed, _ := ex.Execute(context.Background(), "browser_tabs", json.RawMessage(`{"session_id":"`+sess+`","action":"new","url":"https://x.example/login"}`), ExecuteOptions{})
	var tab map[string]any
	_ = json.Unmarshal([]byte(tabbed.Output), &tab)
	res, _ := ex.Execute(context.Background(), "browser_navigate", json.RawMessage(`{"session_id":"`+sess+`","tab_id":"`+tab["tab_id"].(string)+`","action":"go","url":"https://x.example/login"}`), ExecuteOptions{})
	if res.Status != StatusOK || !strings.Contains(res.Output, "input_required") || !strings.Contains(res.Output, capabilities.BlockedLogin) {
		t.Fatalf("blocked op must surface its need: %+v", res)
	}
}

// Row 9: every allowed read records a source event; only those sources
// may back the answer.
func TestWebReadsRecordSources(t *testing.T) {
	reg := NewMapRegistry()
	if err := RegisterBuiltins(reg, nil); err != nil {
		t.Fatal(err)
	}
	cat := NewDeferredCatalog(reg)
	ex := NewExecutor(reg)
	sources := &MemorySourceRecorder{}
	WireSearchCall(reg, cat, ex)
	WireWebTools(reg, cat, &WebDeps{Backend: NewMemorySearchBackend(), Sources: sources})
	read, _ := ex.Execute(context.Background(), "web_read", json.RawMessage(`{"url":"https://example.com/go-sqlite-guide"}`), ExecuteOptions{})
	if read.Status != StatusOK {
		t.Fatalf("web_read: %+v", read)
	}
	denied, _ := ex.Execute(context.Background(), "web_read", json.RawMessage(`{"url":"https://github.com/terminal-bench/terminal-bench"}`), ExecuteOptions{})
	if denied.Status != StatusDenied {
		t.Fatalf("benchmark sources must be refused: %+v", denied)
	}
	events := sources.Events()
	allowed := []SourceEvent{}
	blocked := 0
	for _, e := range events {
		if e.Blocked {
			blocked++
			continue
		}
		allowed = append(allowed, e)
	}
	if len(allowed) != 1 || !strings.Contains(allowed[0].URL, "example.com/go-sqlite-guide") {
		t.Fatalf("exactly the allowed read may back the answer: %+v", events)
	}
	if blocked != 1 {
		t.Fatalf("the refused read must be logged as blocked: %+v", events)
	}
}

// Row 13: exported documents keep content and structure; a manual
// visual check on small and large screens stays out of band.
func TestDocumentExportRoundTrip(t *testing.T) {
	d := capabilities.NewDocumentsFake()
	created, _ := d.Invoke("document_create", json.RawMessage(`{"format":"md","spec":"# title\n\nbody text","path":"report.md"}`), "k")
	if created.State != capabilities.StateCompleted {
		t.Fatalf("create: %+v", created)
	}
	read, _ := d.Invoke("document_read", json.RawMessage(`{"path":"report.md"}`), "")
	var payload map[string]any
	_ = json.Unmarshal(read.Payload, &payload)
	pages := payload["pages"].([]any)
	text := pages[0].(map[string]any)["text"].(string)
	if text != "# title\n\nbody text" {
		t.Fatalf("content must survive the round trip: %q", text)
	}
	if payload["format"] != "md" {
		t.Fatalf("format must survive: %v", payload)
	}
}

// Loader doctor reports validation, health and versions.
func TestLoaderDoctor(t *testing.T) {
	_, _, _, loader := extSetup()
	defer loader.Close()
	if _, err := loader.LoadDir(filepath.Join(repoRoot(t), "extensions", "example-builtin")); err != nil {
		t.Fatal(err)
	}
	issues := loader.Doctor()
	if len(issues) != 0 {
		t.Fatalf("nothing loaded, nothing to report: %+v", issues)
	}
}

// Hot add through the file watcher: a manifest dropped into a watched
// root activates without a restart and the prefix stays fixed.
func TestWatchHotAdd(t *testing.T) {
	reg, cat, _, loader := extSetup()
	defer loader.Close()
	before := cat.PrefixHash()
	root := t.TempDir()
	sub := filepath.Join(root, "watched")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := loader.Watch(root); err != nil {
		t.Fatal(err)
	}
	def := Definition{
		Name: "watched_op", Description: "watched op",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Effect:      "read_only", Idempotency: "pure", Cancel: "immediate",
		Timeout: 60_000_000_000, OutputBudget: 1 << 16,
		Platforms: []string{"linux"}, Profiles: []string{"default"},
	}
	loader.ProvideBuiltin("watched", []Tool{NewEchoTool(def, "watch ok")})
	// The subdirectory predates Watch, so the manifest write lands in
	// an already-watched directory and the kernel buffers the event
	// even if the serve loop is still starting.
	writeManifest(t, sub, Manifest{ID: "watched", Version: "0.1.0", Contract: ExtensionContract,
		Kind: ExtensionBuiltin, Tools: []ExtensionTool{{Definition: def}}})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := reg.Get("watched_op"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("watched extension never activated")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := cat.PrefixHash(); got != before {
		t.Fatal("watch activation must not change the fixed prefix hash")
	}
}
