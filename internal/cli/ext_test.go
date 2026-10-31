package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/pkg/extsdk"
)

func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }

func marshalManifest(m tools.Manifest) ([]byte, error) {
	conv, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var sdkM extsdk.Manifest
	if err := json.Unmarshal(conv, &sdkM); err != nil {
		return nil, err
	}
	return extsdk.MarshalManifestYAML(sdkM)
}

// The ext family is local-first: without a daemon the commands run
// against EXCALIBUR_EXTENSIONS_DIR or an explicit root.

func TestRunExtUsage(t *testing.T) {
	out, code := RunExt(nil)
	if code != 0 || !strings.Contains(out, "excalibur ext") {
		t.Fatalf("usage: %q %d", out, code)
	}
	if _, code := RunExt([]string{"frobnicate"}); code == 0 {
		t.Fatal("unknown subcommand must fail")
	}
}

func TestExtNewListDoctor(t *testing.T) {
	work := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	for _, kind := range []string{"mcp", "app", "package", "builtin"} {
		out, code := RunExt([]string{"new", "--kind", kind, "demo-" + kind})
		if code != 0 {
			t.Fatalf("new %s: %q", kind, out)
		}
	}
	t.Setenv("EXCALIBUR_EXTENSIONS_DIR", filepath.Join(work, "root"))
	out, code := RunExt([]string{"list"})
	if code != 0 || !strings.Contains(out, "no extensions") {
		t.Fatalf("empty list: %q %d", out, code)
	}
	out, code = RunExt([]string{"doctor"})
	if code != 0 || !strings.Contains(out, "no extensions") {
		t.Fatalf("empty doctor: %q %d", out, code)
	}
	if _, code := RunExt([]string{"new"}); code == 0 {
		t.Fatal("new without kind must fail")
	}
}

func scaffoldPackage(t *testing.T, dir string) {
	t.Helper()
	manifest := tools.Manifest{
		ID: "demo-pkg", Version: "0.1.0", Contract: tools.ExtensionContract,
		Kind: tools.ExtensionPackage, Lifecycle: tools.LifecycleLazy,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Tools: []tools.ExtensionTool{{Definition: tools.Definition{
			Name: "demo_greet", Description: "Greet",
			InputSchema: jsonRaw(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`),
			Effect:      "read_only", Idempotency: "pure", Cancel: "immediate",
			Timeout: 60_000_000_000, OutputBudget: 1 << 16,
		}}},
	}
	raw, err := marshalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, tools.ExtensionFileName), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	toolJSON := `{"name":"demo_greet","version":"0.1.0","description":"Greet",` +
		`"input_schema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]},` +
		`"effect":"read_only","idempotency":"pure","cancel":"immediate","fast":false,` +
		`"implementation":{"kind":"script","script_cmd":"echo"},` +
		`"tests":[{"name":"greet","input":{"name":"Ada"},"exact":"script echo: {\"name\":\"Ada\"}"}]}`
	if err := os.WriteFile(filepath.Join(dir, "tool.json"), []byte(toolJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probes.json"), []byte(`[{"tool":"demo_greet","args":{"name":"Ada"}}]`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExtTestPackageAndDoctor(t *testing.T) {
	work := t.TempDir()
	dir := filepath.Join(work, "demo-pkg")
	scaffoldPackage(t, dir)
	out, code := RunExt([]string{"test", dir})
	if code != 0 {
		t.Fatalf("ext test must pass:\n%s", out)
	}
	if !strings.Contains(out, "PASS") {
		t.Fatalf("report must say PASS:\n%s", out)
	}
	out, code = RunExt([]string{"doctor", work})
	if code != 0 || !strings.Contains(out, "demo-pkg") {
		t.Fatalf("doctor must list the extension:\n%s", out)
	}
	if _, code := RunExt([]string{"test", filepath.Join(work, "missing")}); code == 0 {
		t.Fatal("test on a missing dir must fail")
	}
}

func TestExtInstallEnableDisableRemoveLogs(t *testing.T) {
	work := t.TempDir()
	root := filepath.Join(work, "root")
	t.Setenv("EXCALIBUR_EXTENSIONS_DIR", root)
	src := filepath.Join(work, "src-pkg")
	scaffoldPackage(t, src)
	out, code := RunExt([]string{"install", src})
	if code != 0 {
		t.Fatalf("install: %s", out)
	}
	out, code = RunExt([]string{"list"})
	if code != 0 || !strings.Contains(out, "src-pkg") {
		t.Fatalf("list after install: %s", out)
	}
	out, code = RunExt([]string{"disable", "src-pkg"})
	if code != 0 {
		t.Fatalf("disable: %s", out)
	}
	out, code = RunExt([]string{"enable", "src-pkg"})
	if code != 0 || !strings.Contains(out, "enabled") {
		t.Fatalf("enable: %s", out)
	}
	out, code = RunExt([]string{"logs", "src-pkg"})
	if code != 0 || !strings.Contains(out, "no local logs") {
		t.Fatalf("logs: %s", out)
	}
	out, code = RunExt([]string{"remove", "src-pkg"})
	if code != 0 {
		t.Fatalf("remove: %s", out)
	}
	if _, code := RunExt([]string{"remove", "src-pkg"}); code == 0 {
		t.Fatal("second remove must fail")
	}
}
