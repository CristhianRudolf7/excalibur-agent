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
)

// missingName is a command that no table maps and that apt confirms.
const missingName = "zzqtool"

func online(context.Context) bool { return true }

// missingUntilInstalled returns a command that reports missingName as
// not found until the marker file exists, and the marker path.
func missingUntilInstalled(t *testing.T) (string, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "installed")
	command := fmt.Sprintf(`if [ -f '%s' ]; then echo ready; else echo '%s: command not found'; exit 127; fi`, marker, missingName)
	return command, marker
}

// fakeInstaller confirms missingName in apt, counts installs, creates the
// marker on install, and verifies only when the marker exists.
func fakeInstaller(marker string, installs *int) provisionRunFunc {
	return func(_ context.Context, cmd, dir string) (string, int) {
		switch {
		case strings.HasPrefix(cmd, "apt-cache policy "):
			return missingName + ":\n  Candidate: 1.0\n", 0
		case strings.Contains(cmd, " install "):
			*installs++
			if err := os.WriteFile(marker, nil, 0o644); err != nil {
				return err.Error(), 1
			}
			return "", 0
		case strings.HasPrefix(cmd, "command -v "):
			if _, err := os.Stat(marker); err != nil {
				return "", 1
			}
			return "", 0
		}
		return "", 0
	}
}

func execOutput(t *testing.T, p *Provisioner, command string, timeoutS float64) string {
	t.Helper()
	args := map[string]any{"command": command}
	if timeoutS > 0 {
		args["timeout_s"] = timeoutS
	}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	tool := &ExecTool{Sessions: NewSessionManager(TerminalDeps{}), Provisioner: p}
	res, err := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if err != nil {
		t.Fatal(err)
	}
	return res.Output
}

// A verified install re-runs the original command once. The pre-install
// output shrinks to a first-run line, so no stale not-found text sits next
// to the install.
func TestExecRerunReplacesFirstRun(t *testing.T) {
	requirePOSIXFeature(t, "apt provisioning")
	ResetAptUpdateCache()
	defer ResetAptUpdateCache()
	command, marker := missingUntilInstalled(t)
	installs := 0
	p := &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("apt-get"), Network: online, Run: fakeInstaller(marker, &installs)}
	out := execOutput(t, p, command, 0)
	if installs != 1 {
		t.Fatalf("want one install, got %d; output %q", installs, out)
	}
	if !strings.Contains(out, "provisioning: ") {
		t.Fatalf("timing header missing: %q", out)
	}
	if !strings.Contains(out, "first run: exit 127: "+missingName+": command not found") {
		t.Fatalf("first-run line missing: %q", out)
	}
	if strings.Count(out, missingName+": command not found") != 1 {
		t.Fatalf("pre-install output leaked into the result: %q", out)
	}
	if !strings.Contains(out, "exit: 0") || !strings.Contains(out, "ready") {
		t.Fatalf("result must be the re-run output: %q", out)
	}
	if strings.Contains(out, "verified working command") {
		t.Fatalf("install verification must not appear next to the first run: %q", out)
	}
}

// EXCALIBUR_PROVISION_RERUN=off keeps the first run's output with the
// install note, and never runs the command a second time.
func TestExecRerunSwitchOff(t *testing.T) {
	requirePOSIXFeature(t, "apt provisioning")
	ResetAptUpdateCache()
	defer ResetAptUpdateCache()
	t.Setenv(ProvisionRerunEnv, "off")
	command, marker := missingUntilInstalled(t)
	installs := 0
	p := &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("apt-get"), Network: online, Run: fakeInstaller(marker, &installs)}
	out := execOutput(t, p, command, 0)
	if installs != 1 || !strings.Contains(out, "verified working command") {
		t.Fatalf("install must still run and verify: installs=%d %q", installs, out)
	}
	if strings.Contains(out, "first run:") || strings.Contains(out, "ready") {
		t.Fatalf("switch off must not re-run: %q", out)
	}
}

// A command still running when the call yields cannot be re-run: the
// output is labelled as predating the install instead.
func TestExecNoRerunWhileCommandRuns(t *testing.T) {
	requirePOSIXFeature(t, "apt provisioning")
	ResetAptUpdateCache()
	defer ResetAptUpdateCache()
	marker := filepath.Join(t.TempDir(), "installed")
	command := fmt.Sprintf(`echo '%s: command not found'; sleep 1; if [ -f '%s' ]; then echo ready; fi`, missingName, marker)
	installs := 0
	p := &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("apt-get"), Network: online, Run: fakeInstaller(marker, &installs)}
	out := execOutput(t, p, command, 0.25)
	if installs != 1 {
		t.Fatalf("want one install, got %d: %q", installs, out)
	}
	if strings.Contains(out, "first run:") || !strings.Contains(out, "alive: true") {
		t.Fatalf("a live session must not be re-run: %q", out)
	}
	if !strings.Contains(out, "predates the install") {
		t.Fatalf("stale output must be labelled: %q", out)
	}
}

func TestProvisionBudgetFollowsTimeout(t *testing.T) {
	if got := ProvisionBudget(300); got != 300*time.Second {
		t.Fatalf("timeout_s 300: budget %v", got)
	}
	// Short timeouts are raised to the floor so apt is never cut halfway.
	if got := ProvisionBudget(5); got != MinProvisionBudget {
		t.Fatalf("timeout_s 5: budget %v, want %v", got, MinProvisionBudget)
	}
	if got := ProvisionBudget(0); got != 120*time.Second {
		t.Fatalf("no timeout_s: budget %v, want 120s", got)
	}
}

// The whole provisioning step stops at its budget and says so; the
// caller gets the original output plus the instruction to install by hand.
func TestProvisionBudgetBoundsSteps(t *testing.T) {
	requirePOSIXFeature(t, "apt provisioning")
	ResetAptUpdateCache()
	defer ResetAptUpdateCache()
	p := &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("apt-get"), Network: online,
		Run: func(ctx context.Context, cmd, dir string) (string, int) {
			<-ctx.Done()
			return "", -1
		}}
	start := time.Now()
	res := p.Provision(context.Background(), "file x", "sh: 1: file: not found", "", 150*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("provisioning ignored its budget: %v", elapsed)
	}
	if !strings.Contains(res.Report, "provisioning did not finish in") || !strings.Contains(res.Report, "install it yourself in a background session") {
		t.Fatalf("time bound not reported: %q", res.Report)
	}
	if res.Rerun || res.Timing == "" {
		t.Fatalf("a cut-off install must not re-run and must show its time: %+v", res)
	}
}

// apt waits for another apt or dpkg process of the run instead of failing
// on the lock.
func TestAptWaitsForOwnPackageManager(t *testing.T) {
	requirePOSIXFeature(t, "apt provisioning")
	ResetAptUpdateCache()
	defer ResetAptUpdateCache()
	polls := 0
	var commands []string
	p := &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("apt-get"), Network: online, Poll: time.Millisecond,
		AptBusy: func(context.Context) bool {
			polls++
			return polls <= 2
		},
		Run: func(_ context.Context, cmd, dir string) (string, int) {
			commands = append(commands, cmd)
			return "", 0
		}}
	res := p.Provision(context.Background(), "file x", "sh: 1: file: not found", "", 0)
	if !res.Rerun {
		t.Fatalf("install after the wait must verify: %+v", res)
	}
	if polls < 3 {
		t.Fatalf("apt ran without waiting for the busy package manager (polls=%d)", polls)
	}
	if len(commands) == 0 {
		t.Fatal("no install ran after the wait")
	}
}

// When another package manager stays busy until the budget ends, nothing
// runs and the result says the install did not finish.
func TestAptBusyForeverIsBounded(t *testing.T) {
	requirePOSIXFeature(t, "apt provisioning")
	ResetAptUpdateCache()
	defer ResetAptUpdateCache()
	var commands []string
	p := &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("apt-get"), Network: online, Poll: time.Millisecond,
		AptBusy: func(context.Context) bool { return true },
		Run: func(_ context.Context, cmd, dir string) (string, int) {
			commands = append(commands, cmd)
			return "", 0
		}}
	res := p.Provision(context.Background(), "file x", "sh: 1: file: not found", "", 100*time.Millisecond)
	if len(commands) != 0 {
		t.Fatalf("apt ran while another package manager was busy: %v", commands)
	}
	if res.Rerun || !strings.Contains(res.Report, "did not finish in") {
		t.Fatalf("busy wait must be reported: %+v", res)
	}
}

// EXCALIBUR_PROVISION_APT_LOCK=off restores the immediate apt run.
func TestAptLockSwitchOff(t *testing.T) {
	requirePOSIXFeature(t, "apt provisioning")
	ResetAptUpdateCache()
	defer ResetAptUpdateCache()
	t.Setenv(ProvisionAptLockEnv, "off")
	polls := 0
	var commands []string
	p := &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("apt-get"), Network: online, Poll: time.Millisecond,
		AptBusy: func(context.Context) bool {
			polls++
			return true
		},
		Run: func(_ context.Context, cmd, dir string) (string, int) {
			commands = append(commands, cmd)
			return "", 0
		}}
	res := p.Provision(context.Background(), "file x", "sh: 1: file: not found", "", 0)
	if polls != 0 || !res.Rerun || len(commands) == 0 {
		t.Fatalf("switch off must not wait: polls=%d rerun=%v commands=%v", polls, res.Rerun, commands)
	}
	if got := AwaitPackageManagers(context.Background(), time.Second); !got {
		t.Fatal("AwaitPackageManagers must not wait when the switch is off")
	}
}

// apt-get update runs at most once per aptUpdateInterval.
func TestAptUpdateAtMostOncePerInterval(t *testing.T) {
	ResetAptUpdateCache()
	defer ResetAptUpdateCache()
	oldNow := aptNow
	defer func() { aptNow = oldNow }()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	aptNow = func() time.Time { return now }
	if !ShouldAptUpdate() {
		t.Fatal("first plan must update")
	}
	MarkAptUpdated()
	now = now.Add(9 * time.Minute)
	if ShouldAptUpdate() {
		t.Fatal("updated again within 10 minutes")
	}
	now = now.Add(2 * time.Minute)
	if !ShouldAptUpdate() {
		t.Fatal("index older than 10 minutes must be refreshed")
	}
}

// Harness tool names, prose tokens and names apt does not know never reach
// a package manager. Only a name apt confirms is installed.
func TestUnconfirmedNamesNeverInstall(t *testing.T) {
	requirePOSIXFeature(t, "apt provisioning")
	ResetAptUpdateCache()
	defer ResetAptUpdateCache()
	installed := map[string]bool{}
	p := &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("apt-get"), Network: online,
		Run: func(_ context.Context, cmd, dir string) (string, int) {
			if name, ok := strings.CutPrefix(cmd, "apt-cache policy "); ok {
				if name == missingName {
					return name + ":\n  Candidate: 1.0\n", 0
				}
				return name + ":\n  Candidate: (none)\n", 0
			}
			if strings.Contains(cmd, " install ") {
				for _, name := range []string{"read_file", "status", "finish", "verifier", "my_tool", "the", "notapkgxyz", missingName} {
					if strings.Contains(cmd, " "+name) {
						installed[name] = true
					}
				}
			}
			return "", 0
		}}
	for _, name := range []string{"read_file", "status", "finish", "verifier", "my_tool", "Error", "the", "notapkgxyz", missingName} {
		delete(installed, name)
		p.Provision(context.Background(), name, name+": command not found", "", 0)
		want := name == missingName
		if installed[name] != want {
			t.Errorf("%s: installed=%v want %v", name, installed[name], want)
		}
	}
}

// Missing commands that are not in the curated table are reported as
// suggestions when no apt is present, and never run.
func TestUnmappedCommandWithoutAptIsNotRun(t *testing.T) {
	var commands []string
	p := &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("dnf"), Network: online,
		Run: func(_ context.Context, cmd, dir string) (string, int) {
			commands = append(commands, cmd)
			return "", 0
		}}
	res := p.Provision(context.Background(), "zzqtool", "zzqtool: command not found", "", 0)
	if len(commands) != 0 || res.Rerun {
		t.Fatalf("unmapped name must not run on dnf: %v %+v", commands, res)
	}
}

// The curated table covers the tools images lack. Each maps to the package
// that provides it; python3 never maps to the minimal interpreter.
func TestCuratedPackageTable(t *testing.T) {
	requirePOSIXFeature(t, "apt provisioning")
	ResetAptUpdateCache()
	defer ResetAptUpdateCache()
	apt := fakeLookPath("apt-get")
	cases := map[string]string{
		"ps": "procps", "pgrep": "procps", "curl": "curl", "wget": "wget", "git": "git",
		"file": "file", "xxd": "xxd", "unzip": "unzip", "zip": "zip", "make": "make",
		"gcc": "build-essential", "cc": "build-essential", "g++": "build-essential",
		"sqlite3": "sqlite3", "jq": "jq", "bc": "bc", "netstat": "net-tools",
		"ss": "iproute2", "ffmpeg": "ffmpeg", "tesseract": "tesseract-ocr", "python3": "python3",
	}
	for bin, want := range cases {
		plan := PlanInstall(&MissingDep{Kind: KindCommand, Name: bin}, apt)
		if plan == nil || plan.Package != want || plan.ConfirmPackage != "" {
			t.Errorf("%s: got %+v, want package %q without confirmation", bin, plan, want)
		}
	}
}

// Bootstrapping pip on a system interpreter installs pip with venv, which
// ensurepip needs on Debian and Ubuntu.
func TestPipBootstrapInstallsPipAndVenv(t *testing.T) {
	ResetAptUpdateCache()
	defer ResetAptUpdateCache()
	var commands []string
	p := &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("apt-get", "python3"), Network: online,
		Run: func(_ context.Context, cmd, dir string) (string, int) {
			commands = append(commands, cmd)
			if strings.Contains(cmd, "-m ensurepip") {
				return "ensurepip is not available", 1
			}
			if len(commands) == 1 {
				return "/opt/env/bin/python: No module named pip", 1
			}
			return "", 0
		}}
	provisionText(context.Background(), p, "/opt/env/bin/python x.py", "ModuleNotFoundError: No module named 'yaml'", "/tmp")
	joined := strings.Join(commands, "\n")
	if !strings.Contains(joined, "install -y --no-install-recommends python3-pip python3-venv") {
		t.Fatalf("pip bootstrap must install pip and venv: %v", commands)
	}
}

// Every builtin tool name is a harness tool name, so a shell typo of one
// can never provision a package.
func TestHarnessToolNamesCoverBuiltins(t *testing.T) {
	reg := NewMapRegistry()
	if err := RegisterBuiltins(reg, DefaultBuiltinDeps()); err != nil {
		t.Fatal(err)
	}
	for _, name := range reg.Names() {
		if !harnessToolNames[name] {
			t.Errorf("builtin %q missing from harnessToolNames", name)
		}
	}
}

// Read paths report only, and a name that is not a package gets no line.
func TestReportOnlyNamesMissingCommands(t *testing.T) {
	requirePOSIXFeature(t, "apt provisioning")
	p := &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("apt-get")}
	if got := p.ReportOnly("read_file", "read_file: command not found"); got != "" {
		t.Fatalf("tool names are not packages: %q", got)
	}
	if got := p.ReportOnly("bc 1+1", "bash: bc: command not found"); !strings.Contains(got, "install needed: command 'bc' (run exec to provision)") {
		t.Fatalf("missing command not named: %q", got)
	}
	resolved := &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("apt-get", "bc")}
	if got := resolved.ReportOnly("bc 1+1", "bash: bc: command not found"); got != "" {
		t.Fatalf("a command that now resolves must not be reported: %q", got)
	}
	if got := (&Provisioner{Mode: ProvisionOff}).ReportOnly("bc", "bc: command not found"); got != "" {
		t.Fatalf("off must be silent: %q", got)
	}
}
