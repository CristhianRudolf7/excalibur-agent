package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// Diagnostics retain only general shell errors from evaluation records.
func TestProvisionCompoundDiagnostics(t *testing.T) {
	for _, command := range []string{"cat > script.py <<'PY'\n# pip install example\nPY\npython3 script.py", "set -e; mkdir -p work; python3 - <<'PY'\nprint(1)\nPY"} {
		dep := DetectMissing(command, "/usr/bin/bash: line 1: python3: command not found")
		if dep == nil || dep.Name != "python3" {
			t.Fatalf("diagnostic lost: %+v", dep)
		}
	}
	for _, command := range []string{"rm x", "mkdir work", "timeout 180 bash x", "set -e"} {
		if dep := DetectMissing(command, "ordinary application: item not found"); dep != nil {
			t.Fatalf("false executable: %+v", dep)
		}
	}
}

func TestProvisionMultipleAndUpdateSuccess(t *testing.T) {
	ResetAptUpdateCache()
	defer ResetAptUpdateCache()
	var commands []string
	failUpdate := true
	p := &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("apt-get"), Network: func(context.Context) bool { return true },
		Run: func(_ context.Context, cmd, dir string) (string, int) {
			commands = append(commands, cmd)
			if strings.Contains(cmd, " update ") && failUpdate {
				return "mirror unavailable", 1
			}
			return "", 0
		}}
	out := "/usr/bin/bash: line 1: python3: command not found"
	p.MaybeProvision(context.Background(), "python3", out, "")
	if !ShouldAptUpdate() {
		t.Fatal("failed update was cached")
	}
	failUpdate = false
	report := p.MaybeProvision(context.Background(), "python3; file x", out+"\n/usr/bin/bash: line 2: file: command not found", "")
	if !strings.Contains(report, "python3-minimal") || !strings.Contains(report, "verified working command") || ShouldAptUpdate() {
		t.Fatal(report)
	}
	if !strings.Contains(strings.Join(commands, "\n"), "Lock::Timeout=60") {
		t.Fatal(commands)
	}
}

func TestProvisionInterpreterAndDirectory(t *testing.T) {
	var cmds []string
	p := &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("python3", "uv"), Network: func(context.Context) bool { return true },
		Run: func(_ context.Context, cmd, dir string) (string, int) {
			if dir != "/tmp" {
				t.Fatalf("wrong directory %q", dir)
			}
			cmds = append(cmds, cmd)
			return "", 0
		}}
	report := p.MaybeProvision(context.Background(), "cd /tmp; /opt/venv/bin/python - <<'PY'\nimport yaml\nPY", "ModuleNotFoundError: No module named 'yaml'", "/tmp")
	if !strings.Contains(strings.Join(cmds, "\n"), "--python /opt/venv/bin/python") || !strings.Contains(report, "/opt/venv/bin/python -c") {
		t.Fatal(report, cmds)
	}
}

func TestProvisionSessionOutputs(t *testing.T) {
	t.Setenv(AutoProvisionEnv, "off")
	m := NewSessionManager(TerminalDeps{})
	s, _, _ := m.Start("default", "sleep 0.3; printf 'sh: 1: file: not found\n'", t.TempDir(), false, 250, nil)
	p := &Provisioner{Mode: ProvisionSuggest, LookPath: fakeLookPath("apt-get")}
	io := &ExecIOTool{Sessions: m, Provisioner: p}
	for _, action := range []string{"wait_for", "read", "list"} {
		args := fmt.Sprintf(`{"action":%q,"session":%q,"pattern":"not found","timeout_s":2}`, action, s.ID)
		result, err := io.Invoke(context.Background(), Invocation{Arguments: []byte(args)})
		if err != nil || !strings.Contains(result.Output, "provision: suggest:") {
			t.Fatalf("%s: %v %s", action, err, result.Output)
		}
	}
}

func TestProvisionPipBootstrap(t *testing.T) {
	var commands []string
	p := &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("python3"), Run: func(_ context.Context, cmd, dir string) (string, int) {
		commands = append(commands, cmd)
		if len(commands) == 1 {
			return "/opt/env/bin/python: No module named pip", 1
		}
		return "", 0
	}}
	report := p.MaybeProvision(context.Background(), "/opt/env/bin/python x.py", "ModuleNotFoundError: No module named 'yaml'", "/tmp")
	if !strings.Contains(report, "pip bootstrap exit 0") || len(commands) != 4 || commands[1] != "/opt/env/bin/python -m ensurepip" {
		t.Fatal(report, commands)
	}
}

func TestProvisionSuggestionDoesNotConsumeAptUpdate(t *testing.T) {
	ResetAptUpdateCache()
	defer ResetAptUpdateCache()
	p := &Provisioner{Mode: ProvisionSuggest, LookPath: fakeLookPath("apt-get")}
	report := p.MaybeProvision(context.Background(), "file x", "sh: 1: file: not found", "")
	if !strings.Contains(report, "suggest") || !ShouldAptUpdate() {
		t.Fatal(report)
	}
}

func TestProvisionVerificationFailure(t *testing.T) {
	p := &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("python3"),
		Run: func(_ context.Context, cmd, dir string) (string, int) {
			if strings.Contains(cmd, " -c ") {
				return "import still fails", 1
			}
			return "", 0
		}}
	report := p.MaybeProvision(context.Background(), "python3 x.py", "No module named 'yaml'", "")
	if !strings.Contains(report, "verification failed") || strings.Contains(report, "verified working command") {
		t.Fatal(report)
	}
}

func TestProvisionStatusSessionTail(t *testing.T) {
	m := NewSessionManager(TerminalDeps{})
	m.Start("default", "printf 'sh: 1: file: not found\n'", t.TempDir(), false, 1000, nil)
	tool := &StatusTool{Sessions: m, Provisioner: &Provisioner{Mode: ProvisionSuggest, LookPath: fakeLookPath("apt-get")}}
	result, err := tool.Invoke(context.Background(), Invocation{})
	if err != nil || !strings.Contains(result.Output, "provision: suggest:") {
		t.Fatalf("%v %s", err, result.Output)
	}
	tool.Provisioner.Mode = ProvisionAuto
	if def := tool.Definition(); def.Fast || def.Effect != "exec" {
		t.Fatalf("auto status must declare side effects: %+v", def)
	}
	tool.Provisioner.Mode = ProvisionOff
	if def := tool.Definition(); !def.Fast || def.Effect != "read_only" {
		t.Fatalf("off status must stay fast: %+v", def)
	}
}

func TestProvisionReportPrecedesLargeOutput(t *testing.T) {
	tool := &ExecTool{Sessions: NewSessionManager(TerminalDeps{}), Provisioner: &Provisioner{Mode: ProvisionSuggest, LookPath: fakeLookPath("apt-get")}}
	result, err := tool.Invoke(context.Background(), Invocation{Arguments: []byte(`{"command":"printf 'sh: 1: file: not found\n'; printf '%10000s' x"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if pos := strings.Index(result.Output, "provision:"); pos < 0 || pos > 200 {
		t.Fatalf("report hidden by command output (offset %d)", pos)
	}
}

func TestProvisionIgnoresPrintedNodeFixture(t *testing.T) {
	if dep := DetectMissing("cat example_test.go", `output: "Error: Cannot find module 'express'"`); dep != nil {
		t.Fatalf("source literal is not a diagnostic: %+v", dep)
	}
}
