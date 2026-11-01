package tools

import (
	"context"
	"strings"
	"testing"
)

func fakeLookPath(present ...string) LookPathFunc {
	have := map[string]bool{}
	for _, p := range present {
		have[p] = true
	}
	return func(name string) (string, error) {
		if have[name] {
			return "/usr/bin/" + name, nil
		}
		return "", &fakeLookErr{name}
	}
}

type fakeLookErr struct{ name string }

func (e *fakeLookErr) Error() string { return "not found: " + e.name }

func TestDetectMissingFromRecordedOutputs(t *testing.T) {
	ResetAptUpdateCache()
	cases := []struct {
		name    string
		command string
		output  string
		kind    MissingKind
		dep     string
		// wantNil means no provisioning (stdlib, relative, local).
		wantNil bool
	}{
		{
			name:    "python3 missing (adaptive-rejection-sampler)",
			command: "python3 -c 1",
			output:  "/usr/bin/bash: line 1: python3: command not found\nNORMAL: FAIL (missing value)",
			kind:    KindCommand, dep: "python3",
		},
		{
			name:    "file missing (extract-elf)",
			command: "file /app/a.out",
			output:  "session: sess-2 alive: false\n/usr/bin/bash: line 1: file: command not found\nhint: command not found: file.",
			kind:    KindCommand, dep: "file",
		},
		{
			name:    "ps missing (pypi-server)",
			command: "ps aux",
			output:  "session: sess-7 alive: false\n/usr/bin/bash: line 1: ps: command not found",
			kind:    KindCommand, dep: "ps",
		},
		{
			name:    "ss missing (qemu-alpine-ssh)",
			command: "ss -tln",
			output:  "/bin/bash: line 1: ss: command not found\nhint: command not found: ss.",
			kind:    KindCommand, dep: "ss",
		},
		{
			name:    "chess module missing",
			command: "python3 play.py",
			output:  "Traceback (most recent call last):\n  File \"play.py\", line 6, in <module>\nModuleNotFoundError: No module named 'chess'\n",
			kind:    KindPythonMod, dep: "chess",
		},
		{
			name:    "cv2 module missing (sam-cell-seg)",
			command: "python3 seg.py",
			output:  "Traceback (most recent call last):\n  File \"seg.py\", line 6, in <module>\n    import cv2\nModuleNotFoundError: No module named 'cv2'\n",
			kind:    KindPythonMod, dep: "cv2",
		},
		{
			name:    "lapack shared lib (train-fasttext)",
			command: "./train",
			output:  "ImportError: liblapack.so.3: cannot open shared object file: No such file or directory",
			kind:    KindSharedLib, dep: "liblapack.so.3",
		},
		{
			name:    "R missing package",
			command: "Rscript fit.R",
			output:  "Error in library(foo) : there is no package called ‘foo’\nExecution halted",
			kind:    KindRPackage, dep: "foo",
		},
		{
			name:    "ruby gem",
			command: "ruby app.rb",
			output:  "app.rb:1:in `require': cannot load such file -- rspec (LoadError)",
			kind:    KindRubyGem, dep: "rspec",
		},
		{
			name:    "perl module",
			command: "perl script.pl",
			output:  "Can't locate Foo/Bar.pm in @INC (you may need to install the Foo::Bar module)",
			kind:    KindPerlMod, dep: "Foo::Bar",
		},
		{
			name:    "node module",
			command: "node app.js",
			output:  "Error: Cannot find module 'express'\nRequire stack:\n- /app/app.js",
			kind:    KindNodeMod, dep: "express",
		},
		{
			name:    "node relative is local",
			command: "node app.js",
			output:  "Error: Cannot find module './local'",
			wantNil: true,
		},
		{
			name:    "node builtin never provisions",
			command: "node app.js",
			output:  "Error: Cannot find module 'fs'",
			wantNil: true,
		},
		{
			name:    "python stdlib never provisions",
			command: "python3 x.py",
			output:  "ModuleNotFoundError: No module named 'os'",
			wantNil: true,
		},
		{
			name:    "install output detects missing module",
			command: "pip install foo",
			output:  "ModuleNotFoundError: No module named 'bar'",
			kind:    KindPythonMod, dep: "bar",
		},
		{
			name:    "generic no such file is not provisioning",
			command: "ls /app/polyglot/cmain",
			output:  "/app/polyglot/cmain: No such file or directory",
			wantNil: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectMissing(tc.command, tc.output)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("want nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("want %s %q, got nil", tc.kind, tc.dep)
			}
			if got.Kind != tc.kind || got.Name != tc.dep {
				t.Fatalf("got %+v, want kind=%s name=%q", got, tc.kind, tc.dep)
			}
		})
	}
}

func TestDetectMissingGoDiagnostics(t *testing.T) {
	cases := []struct{ output, want string }{
		{`cannot find package "github.com/acme/widget" in any of:`, "github.com/acme/widget"},
		{`no required module provides package example.com/acme/pkg; to add it:`, "example.com/acme/pkg"},
		{`package fmt is not in GOROOT (/usr/local/go/src/fmt)`, ""},
		{`package example.com/acme/pkg is not in GOROOT`, "example.com/acme/pkg"},
	}
	for _, tc := range cases {
		got := DetectMissing("go build", tc.output)
		if tc.want == "" {
			if got != nil {
				t.Errorf("%q: got %+v, want nil", tc.output, got)
			}
			continue
		}
		if got == nil || got.Kind != KindGoPackage || got.Name != tc.want {
			t.Errorf("%q: got %+v, want Go package %q", tc.output, got, tc.want)
		}
	}
}

func TestPlanCuratedMapsAndFlags(t *testing.T) {
	ResetAptUpdateCache()
	apt := fakeLookPath("apt-get", "sudo", "python3", "pip3")
	cases := []struct {
		dep      MissingDep
		manager  string
		pkg      string
		contains []string
	}{
		{MissingDep{KindCommand, "ps", ""}, "apt", "procps", []string{"install -y --no-install-recommends procps", "command -v ps"}},
		{MissingDep{KindCommand, "ss", ""}, "apt", "iproute2", []string{"iproute2"}},
		{MissingDep{KindCommand, "file", ""}, "apt", "file", []string{"install -y"}},
		{MissingDep{KindCommand, "mytool", ""}, "apt", "mytool", []string{"mytool"}},
		{MissingDep{KindPythonMod, "cv2", ""}, "pip", "opencv-python-headless", []string{"opencv-python-headless", "import cv2"}},
		{MissingDep{KindPythonMod, "chess", ""}, "pip", "chess", []string{"chess"}},
		{MissingDep{KindRPackage, "ggplot2", ""}, "R", "ggplot2", []string{"install.packages", "ggplot2"}},
		{MissingDep{KindRubyGem, "rspec", ""}, "gem", "rspec", []string{"gem install --no-document rspec"}},
		{MissingDep{KindSharedLib, "liblapack.so.3", ""}, "apt", "liblapack3", []string{"liblapack3"}},
		{MissingDep{KindNodeMod, "express", ""}, "npm", "express", []string{"npm install"}},
	}
	for _, tc := range cases {
		plan := PlanInstall(&tc.dep, apt)
		if plan == nil {
			t.Fatalf("%+v: want plan, got nil", tc.dep)
		}
		if plan.Manager != tc.manager {
			t.Fatalf("%+v: manager=%q want %q", tc.dep, plan.Manager, tc.manager)
		}
		if plan.Package != tc.pkg {
			t.Fatalf("%+v: package=%q want %q", tc.dep, plan.Package, tc.pkg)
		}
		joined := strings.Join(plan.Commands, " && ") + " " + plan.Verify
		for _, want := range tc.contains {
			if !strings.Contains(joined, want) {
				t.Fatalf("%+v: plan %q missing %q", tc.dep, joined, want)
			}
		}
		if plan.Verify == "" || len(plan.Commands) == 0 {
			t.Fatalf("%+v: plan needs commands and verify", tc.dep)
		}
	}
}

func TestPlanPipBreaksSystemPackages(t *testing.T) {
	ResetAptUpdateCache()
	apt := fakeLookPath("apt-get", "python3")
	plan := PlanInstall(&MissingDep{KindPythonMod, "chess", ""}, apt)
	if plan == nil {
		t.Fatal("want pip plan")
	}
	if !strings.Contains(plan.Commands[0], "--break-system-packages") {
		t.Fatalf("pip plan must handle PEP 668 in containers: %q", plan.Commands[0])
	}
	if isForbiddenProvisionCommand(plan.Commands[0]) {
		t.Fatalf("break-system-packages must not trip the upgrade guard: %q", plan.Commands[0])
	}
}

func TestPlanAptUpdateOnce(t *testing.T) {
	ResetAptUpdateCache()
	apt := fakeLookPath("apt-get")
	first := PlanInstall(&MissingDep{KindCommand, "ps", ""}, apt)
	if first == nil {
		t.Fatal("first plan nil")
	}
	hasUpdate := false
	for _, c := range first.Commands {
		if strings.Contains(c, "apt-get update") {
			hasUpdate = true
		}
	}
	if !hasUpdate {
		t.Fatalf("first apt plan must carry update: %v", first.Commands)
	}
	MarkAptUpdatedIfApt(first)
	second := PlanInstall(&MissingDep{KindCommand, "file", ""}, apt)
	for _, c := range second.Commands {
		if strings.Contains(c, "apt-get update") {
			t.Fatalf("second apt plan must skip update (update-once): %v", second.Commands)
		}
	}
}

func TestPlanNeverUpgradesOrRemoves(t *testing.T) {
	ResetAptUpdateCache()
	apt := fakeLookPath("apt-get", "python3", "npm", "gem", "Rscript", "cpanm", "go", "cargo")
	deps := []*MissingDep{
		{Kind: KindCommand, Name: "ps"},
		{Kind: KindCommand, Name: "mytool"},
		{Kind: KindPythonMod, Name: "cv2"},
		{Kind: KindPythonMod, Name: "requests"},
		{Kind: KindNodeMod, Name: "express"},
		{Kind: KindRubyGem, Name: "rspec"},
		{Kind: KindRPackage, Name: "ggplot2"},
		{Kind: KindPerlMod, Name: "Foo::Bar"},
		{Kind: KindSharedLib, Name: "liblapack.so.3"},
		{Kind: KindGoPackage, Name: "github.com/foo/bar"},
		{Kind: KindRustCrate, Name: "serde"},
	}
	for _, d := range deps {
		plan := PlanInstall(d, apt)
		if plan == nil {
			continue
		}
		for _, cmd := range plan.Commands {
			if isForbiddenProvisionCommand(cmd) {
				t.Fatalf("%+v: forbidden upgrade/remove in %q", d, cmd)
			}
			l := strings.ToLower(cmd)
			if strings.Contains(l, "upgrade") || strings.Contains(l, "remove") || strings.Contains(l, "purge") || strings.Contains(l, "autoremove") {
				t.Fatalf("%+v: upgrade/remove word in %q", d, cmd)
			}
		}
	}
}

func TestProvisionPolicyModes(t *testing.T) {
	ResetAptUpdateCache()
	look := fakeLookPath("apt-get", "python3")
	depOut := "/usr/bin/bash: line 1: file: command not found"
	newProv := func(mode ProvisionMode, network bool, run func(ctx context.Context, cmd, dir string) (string, int)) *Provisioner {
		var net func(ctx context.Context) bool
		if network {
			net = func(ctx context.Context) bool { return true }
		} else {
			net = func(ctx context.Context) bool { return false }
		}
		return &Provisioner{Mode: mode, LookPath: look, Run: run, Network: net}
	}
	// Off: silent, never runs.
	called := false
	p := newProv(ProvisionOff, true, func(ctx context.Context, cmd, dir string) (string, int) {
		called = true
		return "", 0
	})
	if got := p.MaybeProvision(context.Background(), "file x", depOut, ""); got != "" {
		t.Fatalf("off must be silent, got %q", got)
	}
	if called {
		t.Fatal("off must never run")
	}
	// Suggest: report only.
	called = false
	p = newProv(ProvisionSuggest, true, func(ctx context.Context, cmd, dir string) (string, int) {
		called = true
		return "", 0
	})
	got := p.MaybeProvision(context.Background(), "file x", depOut, "")
	if !strings.Contains(got, "provision:") || !strings.Contains(got, "suggest") {
		t.Fatalf("suggest must report without running: %q", got)
	}
	if called {
		t.Fatal("suggest must not run")
	}
	// Auto offline: suggest, no run.
	called = false
	p = newProv(ProvisionAuto, false, func(ctx context.Context, cmd, dir string) (string, int) {
		called = true
		return "", 0
	})
	got = p.MaybeProvision(context.Background(), "file x", depOut, "")
	if !strings.Contains(got, "provision:") || !strings.Contains(got, "network unavailable") {
		t.Fatalf("offline auto must suggest: %q", got)
	}
	if called {
		t.Fatal("offline must not run")
	}
	// Auto online success.
	ResetAptUpdateCache()
	called = false
	p = newProv(ProvisionAuto, true, func(ctx context.Context, cmd, dir string) (string, int) {
		called = true
		return "Setting up file (1:5.44-3) ...", 0
	})
	got = p.MaybeProvision(context.Background(), "file x", depOut, "")
	if !strings.Contains(got, "provision: installed") || !strings.Contains(got, "re-run") {
		t.Fatalf("auto success must report install: %q", got)
	}
	if !called {
		t.Fatal("auto online must run")
	}
	// Auto online failure (update ok, install fails).
	ResetAptUpdateCache()
	p = newProv(ProvisionAuto, true, func(ctx context.Context, cmd, dir string) (string, int) {
		if strings.Contains(cmd, "update") {
			return "", 0
		}
		return "E: Unable to locate package nope", 100
	})
	got = p.MaybeProvision(context.Background(), "file x", depOut, "")
	if !strings.Contains(got, "provision: install failed") {
		t.Fatalf("auto failure must report: %q", got)
	}
}

func TestProvisionModeParsing(t *testing.T) {
	if ParseProvisionMode("") != ProvisionAuto {
		t.Fatal("empty defaults to auto")
	}
	if ParseProvisionMode("auto") != ProvisionAuto || ParseProvisionMode("1") != ProvisionAuto {
		t.Fatal("auto parsing")
	}
	if ParseProvisionMode("suggest") != ProvisionSuggest {
		t.Fatal("suggest parsing")
	}
	if ParseProvisionMode("off") != ProvisionOff || ParseProvisionMode("0") != ProvisionOff {
		t.Fatal("off parsing")
	}
	if ProvisionModeFromEnv() == "" {
		t.Fatal("env default must be non-empty")
	}
	t.Setenv(AutoProvisionEnv, "off")
	if ProvisionModeFromEnv() != ProvisionOff {
		t.Fatal("env off")
	}
	t.Setenv(AutoProvisionEnv, "suggest")
	if ProvisionModeFromEnv() != ProvisionSuggest {
		t.Fatal("env suggest")
	}
	t.Setenv(AutoProvisionEnv, "auto")
	if ProvisionModeFromEnv() != ProvisionAuto {
		t.Fatal("env auto")
	}
}

func TestExecToolProvisionReporting(t *testing.T) {
	ResetAptUpdateCache()
	// Suggest mode surfaces the report without running anything.
	t.Setenv(AutoProvisionEnv, "suggest")
	m := NewSessionManager(TerminalDeps{})
	tool := &ExecTool{Sessions: m}
	res, err := tool.Invoke(context.Background(), Invocation{Arguments: []byte(`{"command":"excalibur-test-missing-bin-xyz123 --help"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "command not found") {
		t.Fatalf("exec must show the failure: %q", res.Output)
	}
	if !strings.Contains(res.Output, "provision:") {
		t.Fatalf("suggest must append provisioning: %q", res.Output)
	}
	// Off restores the old bytes (hint only, no provision line).
	t.Setenv(AutoProvisionEnv, "off")
	ResetAptUpdateCache()
	m2 := NewSessionManager(TerminalDeps{})
	tool2 := &ExecTool{Sessions: m2}
	res2, err := tool2.Invoke(context.Background(), Invocation{Arguments: []byte(`{"command":"excalibur-test-missing-bin-xyz123 --help"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res2.Output, "provision:") {
		t.Fatalf("off must not provision: %q", res2.Output)
	}
}

// TestProvisionRealShell is the real container check: the default
// runner executes a trivial command in this container (no docker,
// no network, no apt) and reports its output.
func TestProvisionRealShell(t *testing.T) {
	out, code := defaultRunProvisionCommand(context.Background(), "echo provision-ok", "", 10*1e9)
	if code != 0 {
		t.Fatalf("echo exit=%d out=%q", code, out)
	}
	if !strings.Contains(out, "provision-ok") {
		t.Fatalf("echo output=%q", out)
	}
	if sh := DetectShell(); sh == "" {
		t.Fatal("shell detection must work in the container")
	}
}
