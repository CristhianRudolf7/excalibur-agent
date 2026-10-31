package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/pkg/extsdk"
)

// Extension CLI (Section 14.12, excalibur ext). The commands are thin
// layers over the shared loader and conformance suite in
// internal/tools: no agent or provider logic lives here. Without a
// daemon they operate on a local extensions root
// ($EXCALIBUR_EXTENSIONS_DIR or the data-dir default); with a daemon
// the same calls go through the gate capability family (Section 3.5).

// ExtensionsRoot resolves the local extensions root.
func ExtensionsRoot() string {
	if dir := os.Getenv("EXCALIBUR_EXTENSIONS_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "excalibur-extensions")
	}
	return filepath.Join(home, ".local", "share", "excalibur", "extensions")
}

// RunExt dispatches one excalibur ext subcommand and returns its
// printable output. Exit codes follow cmd/excalibur: 0 on success,
// 1 on usage or backend errors, 69 when the live daemon is required.
func RunExt(args []string) (string, int) {
	if len(args) == 0 {
		return extUsage(), 0
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "new":
		return extNew(rest)
	case "list":
		return extList(rest)
	case "install":
		return extInstall(rest)
	case "enable":
		return extEnable(rest, false)
	case "disable":
		return extEnable(rest, true)
	case "remove":
		return extRemove(rest)
	case "logs":
		return extLogs(rest)
	case "test":
		return extTest(rest)
	case "doctor":
		return extDoctor(rest)
	case "--help", "-h", "help":
		return extUsage(), 0
	default:
		return fmt.Sprintf("excalibur ext: unknown subcommand %q\n\n%s", sub, extUsage()), 1
	}
}

func extUsage() string {
	return "Usage: excalibur ext <subcommand> [args]\n\n" +
		"  new --kind mcp|app|package|builtin <name>   Scaffold an extension\n" +
		"  list [root]                                List extensions\n" +
		"  install <dir|store-ref>                    Install an extension\n" +
		"  enable <id> | disable <id>                 Enable or disable an extension\n" +
		"  remove <id>                                Remove an extension\n" +
		"  logs <id>                                  Show extension logs\n" +
		"  test <dir>                                 Run the contract conformance suite\n" +
		"  doctor [root]                              Report validation, health and versions\n"
}

// extNew scaffolds excalibur-extension.yaml plus the minimal route
// files and a probes.json for ext test.
func extNew(args []string) (string, int) {
	kind := ""
	name := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--kind" && i+1 < len(args) {
			kind = args[i+1]
			i++
			continue
		}
		if !strings.HasPrefix(args[i], "-") && name == "" {
			name = args[i]
		}
	}
	full := map[string]string{"mcp": "mcp", "app": "app", "package": "package", "builtin": "builtin"}
	resolved, ok := full[kind]
	if !ok {
		return "excalibur ext new: --kind mcp|app|package|builtin and a <name> are required", 1
	}
	if name == "" {
		return "excalibur ext new: a <name> is required", 1
	}
	dir := name
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Sprintf("excalibur ext new: %v", err), 1
	}
	toolName := strings.ReplaceAll(strings.ToLower(name), "-", "_") + "_run"
	manifest := tools.Manifest{
		ID: name, Version: "0.1.0", Contract: tools.ExtensionContract,
		Kind: resolved, Lifecycle: tools.LifecycleLazy,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Tools: []tools.ExtensionTool{{Definition: tools.Definition{
			Name: toolName, Description: "Run the " + name + " operation",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Effect:      "exec", Idempotency: "keyed",
			Timeout: 60_000_000_000, OutputBudget: 64 * 1024,
			Cancel: "graceful", Fast: false, Group: "extensions",
			Tags: []string{"extensions"}, Keywords: []string{name},
			DryRun: "prints the planned operation without executing it",
		}}},
	}
	switch resolved {
	case tools.ExtensionMCP:
		manifest.Transport = "stdio"
		manifest.Command = []string{"./server"}
	case tools.ExtensionApp:
		manifest.App = &tools.AppBinding{StateForm: "compact state with selection", Events: []string{"user_change"}, Scope: "app:" + name}
	case tools.ExtensionPackage:
		manifest.Tools[0].DryRun = "validates the inputs without running the script"
	case tools.ExtensionBuiltin:
		manifest.Lifecycle = tools.LifecycleResident
	}
	sdkRaw, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Sprintf("excalibur ext new: %v", err), 1
	}
	var sdkM extsdk.Manifest
	if err := json.Unmarshal(sdkRaw, &sdkM); err != nil {
		return fmt.Sprintf("excalibur ext new: %v", err), 1
	}
	raw, err := extsdk.MarshalManifestYAML(sdkM)
	if err != nil {
		return fmt.Sprintf("excalibur ext new: %v", err), 1
	}
	if err := os.WriteFile(filepath.Join(dir, tools.ExtensionFileName), raw, 0o644); err != nil {
		return fmt.Sprintf("excalibur ext new: %v", err), 1
	}
	probes := []tools.ConformanceProbe{{Tool: toolName, Args: json.RawMessage(`{}`)}}
	praw, _ := json.MarshalIndent(probes, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "probes.json"), praw, 0o644)
	notes := map[string]string{
		"mcp":     "server: implement the stdio MCP server (see extensions/example-mcp) and declare excalibur/ _meta per tool.",
		"app":     "app: bind the channel with BindApp after capability.register; emit user changes with capability.event.",
		"package": "package: add tool.json with the PackageManifest and the implementation script.",
		"builtin": "builtin: implement extsdk.Tool in Go and register it with ProvideBuiltin.",
	}
	return fmt.Sprintf("scaffolded extension %s (%s) in ./%s\n%s", name, resolved, dir, notes[resolved]), 0
}

// loaderOver builds a throwaway loader for local ext commands.
func loaderOver() (*tools.Loader, *tools.MapRegistry, *tools.Executor, error) {
	reg := tools.NewMapRegistry()
	if err := tools.RegisterBuiltins(reg, nil); err != nil {
		return nil, nil, nil, err
	}
	cat := tools.NewDeferredCatalog(reg)
	exec := tools.NewExecutor(reg)
	tools.WireSearchCall(reg, cat, exec)
	return tools.NewLoader(reg, cat, exec), reg, exec, nil
}

func eachExtensionDir(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := []string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), tools.ExtensionFileName)); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func extList(args []string) (string, int) {
	root := ExtensionsRoot()
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		root = args[0]
	}
	dirs, err := eachExtensionDir(root)
	if err != nil {
		return fmt.Sprintf("excalibur ext list: %v", err), 1
	}
	if len(dirs) == 0 {
		return fmt.Sprintf("no extensions in %s", root), 0
	}
	return "extensions in " + root + ":\n  " + strings.Join(dirs, "\n  "), 0
}

func extInstall(args []string) (string, int) {
	if len(args) == 0 {
		return "excalibur ext install: a <dir> or store reference is required", 1
	}
	src := args[0]
	root := ExtensionsRoot()
	if st, err := os.Stat(filepath.Join(src, tools.ExtensionFileName)); err == nil && st != nil {
		id := filepath.Base(src)
		dst := filepath.Join(root, id)
		if err := copyDir(src, dst); err != nil {
			return fmt.Sprintf("excalibur ext install: %v", err), 1
		}
		abs, _ := filepath.Abs(src)
		if abs == dst {
			return fmt.Sprintf("extension %s is already at %s", id, dst), 0
		}
		return fmt.Sprintf("installed %s from %s", id, src), 0
	}
	return fmt.Sprintf("excalibur ext install: store installs need the store capability; local dir %q has no %s", src, tools.ExtensionFileName), 1
}

func extEnable(args []string, disable bool) (string, int) {
	verb := "enable"
	if disable {
		verb = "disable"
	}
	if len(args) == 0 {
		return fmt.Sprintf("excalibur ext %s: an <id> is required", verb), 1
	}
	id := args[0]
	marker := filepath.Join(ExtensionsRoot(), id+".disabled")
	if disable {
		if err := os.MkdirAll(ExtensionsRoot(), 0o755); err != nil {
			return fmt.Sprintf("excalibur ext disable: %v", err), 1
		}
		stamp := time.Now().UTC().Format(time.RFC3339)
		if err := os.WriteFile(marker, []byte(stamp), 0o644); err != nil {
			return fmt.Sprintf("excalibur ext disable: %v", err), 1
		}
		return fmt.Sprintf("disabled %s", id), 0
	}
	if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
		return fmt.Sprintf("excalibur ext enable: %v", err), 1
	}
	return fmt.Sprintf("enabled %s", id), 0
}

func extRemove(args []string) (string, int) {
	if len(args) == 0 {
		return "excalibur ext remove: an <id> is required", 1
	}
	id := args[0]
	dir := filepath.Join(ExtensionsRoot(), id)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Sprintf("excalibur ext remove: unknown extension %q", id), 1
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Sprintf("excalibur ext remove: %v", err), 1
	}
	_ = os.Remove(dir + ".disabled")
	return fmt.Sprintf("removed %s", id), 0
}

func extLogs(args []string) (string, int) {
	if len(args) == 0 {
		return "excalibur ext logs: an <id> is required", 1
	}
	id := args[0]
	for _, candidate := range []string{
		filepath.Join(ExtensionsRoot(), id, "extension.log"),
		filepath.Join(ExtensionsRoot(), id+".log"),
	} {
		if raw, err := os.ReadFile(candidate); err == nil {
			return string(raw), 0
		}
	}
	return fmt.Sprintf("no local logs for %s: live logs stream from the daemon gate (Section 3.5)", id), 0
}

// extTest runs the conformance suite of one extension directory:
// static contract checks plus behavioral probes from probes.json.
func extTest(args []string) (string, int) {
	if len(args) == 0 {
		return "excalibur ext test: a <dir> is required", 1
	}
	dir := args[0]
	loader, _, _, err := loaderOver()
	if err != nil {
		return fmt.Sprintf("excalibur ext test: %v", err), 1
	}
	defer loader.Close()
	ext, err := loader.LoadDir(dir)
	if err != nil {
		return fmt.Sprintf("excalibur ext test: FAIL\n  manifest: %v", err), 1
	}
	// Route fixtures bind in-process doubles so ext test never needs a
	// live server: builtin tools come from the directory fixture when
	// present (builtin.json), apps bind a memory channel.
	if err := bindTestDoubles(loader, ext); err != nil {
		return fmt.Sprintf("excalibur ext test: FAIL\n  bind: %v", err), 1
	}
	before := loader.PrefixHash()
	probes := loadProbes(dir)
	// The gate activates in a sandbox first: a failing extension never
	// registers, and the fixed prefix never changes (Sections 11.6,
	// 13.3, 14.12).
	report, err := loader.ActivateChecked(ext, probes)
	if err != nil && !report.Passed {
		out := tools.FormatReport(report)
		if loader.PrefixHash() != before {
			out += "WARNING: activation changed the fixed prefix hash\n"
		}
		return out, 1
	}
	if err != nil {
		return tools.FormatReport(report) + "\n" + err.Error(), 1
	}
	out := tools.FormatReport(report)
	if loader.PrefixHash() != before {
		out += "WARNING: activation changed the fixed prefix hash\n"
		report.Passed = false
	}
	if !report.Passed {
		return out, 1
	}
	return out, 0
}

// bindTestDoubles wires throwaway route bindings for ext test.
func bindTestDoubles(loader *tools.Loader, ext *tools.LoadedExtension) error {
	switch ext.Manifest.Kind {
	case tools.ExtensionBuiltin:
		doubles := testBuiltinTools(ext)
		if doubles == nil {
			return fmt.Errorf("builtin extension %q needs builtin.json with tool fixtures for ext test", ext.Manifest.ID)
		}
		loader.ProvideBuiltin(ext.Manifest.ID, doubles)
	case tools.ExtensionApp:
		loader.BindApp(ext.Manifest.ID, tools.NewMemoryAppChannel())
	case tools.ExtensionPackage:
		// Bound from tool.json at activation; nothing to pre-wire.
	case tools.ExtensionMCP:
		// The loader resolves a relative server command against the
		// extension directory, so ext test works once the shipped
		// server is built next to the manifest.
		if len(ext.Manifest.Command) == 0 {
			return fmt.Errorf("mcp extension %q needs a command for ext test", ext.Manifest.ID)
		}
		if filepath.IsAbs(ext.Manifest.Command[0]) {
			if _, err := os.Stat(ext.Manifest.Command[0]); err != nil {
				return fmt.Errorf("mcp extension %q: server %q is not built: %v", ext.Manifest.ID, ext.Manifest.Command[0], err)
			}
			return nil
		}
		if _, err := os.Stat(filepath.Join(ext.Dir, ext.Manifest.Command[0])); err != nil {
			return fmt.Errorf("mcp extension %q: build the server first (missing %s): %v", ext.Manifest.ID, ext.Manifest.Command[0], err)
		}
		return nil
	}
	return nil
}

// testBuiltinTools builds echo tools from builtin.json fixtures.
func testBuiltinTools(ext *tools.LoadedExtension) []tools.Tool {
	raw, err := os.ReadFile(filepath.Join(ext.Dir, "builtin.json"))
	if err != nil {
		return nil
	}
	var fixtures []struct {
		Name   string `json:"name"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		return nil
	}
	byName := map[string]string{}
	for _, f := range fixtures {
		byName[f.Name] = f.Output
	}
	out := []tools.Tool{}
	for _, entry := range ext.Manifest.Tools {
		output, ok := byName[entry.Name]
		if !ok {
			return nil
		}
		out = append(out, tools.NewEchoTool(entry.Definition, output))
	}
	return out
}

func loadProbes(dir string) []tools.ConformanceProbe {
	raw, err := os.ReadFile(filepath.Join(dir, "probes.json"))
	if err != nil {
		return nil
	}
	var probes []tools.ConformanceProbe
	if err := json.Unmarshal(raw, &probes); err != nil {
		return nil
	}
	return probes
}

// extDoctor validates every extension in a root and reports versions.
func extDoctor(args []string) (string, int) {
	root := ExtensionsRoot()
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		root = args[0]
	}
	loader, _, _, err := loaderOver()
	if err != nil {
		return fmt.Sprintf("excalibur ext doctor: %v", err), 1
	}
	defer loader.Close()
	dirs, err := eachExtensionDir(root)
	if err != nil {
		return fmt.Sprintf("excalibur ext doctor: %v", err), 1
	}
	if len(dirs) == 0 {
		return fmt.Sprintf("no extensions in %s", root), 0
	}
	var b strings.Builder
	failed := false
	for _, name := range dirs {
		ext, err := loader.LoadDir(filepath.Join(root, name))
		if err != nil {
			fmt.Fprintf(&b, "[FAIL] %s: %v\n", name, err)
			failed = true
			continue
		}
		sdkManifest := extsdk.Manifest{}
		raw, _ := json.Marshal(ext.Manifest)
		if err := json.Unmarshal(raw, &sdkManifest); err != nil {
			fmt.Fprintf(&b, "[FAIL] %s: sdk parity: %v\n", name, err)
			failed = true
			continue
		}
		if err := sdkManifest.Validate(); err != nil {
			fmt.Fprintf(&b, "[FAIL] %s: %v\n", name, err)
			failed = true
			continue
		}
		fmt.Fprintf(&b, "[ok] %s %s (%s): %d tools, contract %s\n",
			ext.Manifest.ID, ext.Manifest.Version, ext.Manifest.Kind, len(ext.Manifest.Tools), ext.Manifest.Contract)
	}
	if failed {
		return b.String(), 1
	}
	return b.String(), 0
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		s, d := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(s, d); err != nil {
				return err
			}
			continue
		}
		raw, err := os.ReadFile(s)
		if err != nil {
			return err
		}
		if err := os.WriteFile(d, raw, 0o644); err != nil {
			return err
		}
	}
	return nil
}
