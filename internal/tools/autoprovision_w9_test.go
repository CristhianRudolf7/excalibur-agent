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
	provisionText(context.Background(), p, "python3", out, "")
	if !ShouldAptUpdate() {
		t.Fatal("failed update was cached")
	}
	failUpdate = false
	report := provisionText(context.Background(), p, "python3; file x", out+"\n/usr/bin/bash: line 2: file: command not found", "")
	if !strings.Contains(report, "verified working command") || ShouldAptUpdate() {
		t.Fatal(report)
	}
	joined := strings.Join(commands, "\n")
	// python3 maps to the full interpreter package, never the minimal one.
	if strings.Contains(joined, "python3-minimal") || !strings.Contains(joined, "--no-install-recommends python3") {
		t.Fatalf("python3 must install the full package: %v", commands)
	}
	if !strings.Contains(joined, "Lock::Timeout=60") {
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
	report := provisionText(context.Background(), p, "cd /tmp; /opt/venv/bin/python - <<'PY'\nimport yaml\nPY", "ModuleNotFoundError: No module named 'yaml'", "/tmp")
	if !strings.Contains(strings.Join(cmds, "\n"), "--python /opt/venv/bin/python") || !strings.Contains(report, "/opt/venv/bin/python -c") {
		t.Fatal(report, cmds)
	}
}

func TestProvisionSessionOutputs(t *testing.T) {
	m := NewSessionManager(TerminalDeps{})
	s, _, _ := m.Start("default", "sleep 0.3; printf 'sh: 1: file: not found\n'", t.TempDir(), false, 250, nil)
	// Auto mode, and any command the read path runs is counted: read
	// paths only report, so the count must stay zero.
	ran := 0
	p := &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("apt-get"), Run: func(_ context.Context, cmd, dir string) (string, int) {
		ran++
		return "", 1
	}}
	io := &ExecIOTool{Sessions: m, Provisioner: p}
	for _, action := range []string{"wait_for", "read", "list"} {
		args := fmt.Sprintf(`{"action":%q,"session":%q,"pattern":"not found","timeout_s":2}`, action, s.ID)
		result, err := io.Invoke(context.Background(), Invocation{Arguments: []byte(args)})
		if err != nil || !strings.Contains(result.Output, "install needed: command 'file' (run exec to provision)") || strings.Contains(result.Output, "provision:") {
			t.Fatalf("%s: %v %s", action, err, result.Output)
		}
	}
	if ran != 0 {
		t.Fatalf("read paths ran %d command(s)", ran)
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
	report := provisionText(context.Background(), p, "/opt/env/bin/python x.py", "ModuleNotFoundError: No module named 'yaml'", "/tmp")
	if !strings.Contains(report, "pip bootstrap exit 0") || len(commands) != 4 || commands[1] != "/opt/env/bin/python -m ensurepip" {
		t.Fatal(report, commands)
	}
}

func TestProvisionSuggestionDoesNotConsumeAptUpdate(t *testing.T) {
	ResetAptUpdateCache()
	defer ResetAptUpdateCache()
	p := &Provisioner{Mode: ProvisionSuggest, LookPath: fakeLookPath("apt-get")}
	report := provisionText(context.Background(), p, "file x", "sh: 1: file: not found", "")
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
	report := provisionText(context.Background(), p, "python3 x.py", "No module named 'yaml'", "")
	if !strings.Contains(report, "verification failed") || strings.Contains(report, "verified working command") {
		t.Fatal(report)
	}
}

func TestProvisionStatusSessionTail(t *testing.T) {
	// A status read is passive: it reports the session's own tail and
	// never runs or suggests the provisioner, in any provisioning mode.
	m := NewSessionManager(TerminalDeps{})
	m.Start("default", "printf 'sh: 1: file: not found\n'", t.TempDir(), false, 1000, nil)
	ran := 0
	tool := &StatusTool{Sessions: m, Provisioner: &Provisioner{Mode: ProvisionAuto, LookPath: fakeLookPath("apt-get"), Run: func(_ context.Context, cmd, dir string) (string, int) {
		ran++
		return "", 1
	}}}
	result, err := tool.Invoke(context.Background(), Invocation{})
	if err != nil || !strings.Contains(result.Output, "install needed: command 'file' (run exec to provision)") || strings.Contains(result.Output, "provision:") || !strings.Contains(result.Output, "tail: sh: 1: file: not found") {
		t.Fatalf("%v %s", err, result.Output)
	}
	if ran != 0 {
		t.Fatalf("status ran %d command(s)", ran)
	}
	// Status is read-only and fast in every mode: it never installs.
	for _, mode := range []ProvisionMode{ProvisionAuto, ProvisionSuggest, ProvisionOff} {
		tool.Provisioner.Mode = mode
		if def := tool.Definition(); !def.Fast || def.Effect != "read_only" {
			t.Fatalf("status must stay read-only in %s mode: %+v", mode, def)
		}
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
