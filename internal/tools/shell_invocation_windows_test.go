package tools

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestWindowsShellArgs(t *testing.T) {
	for _, tt := range []struct {
		shell string
		args  []string
	}{
		{`C:\Git\bin\bash.exe`, []string{"-c", "script"}},
		{"zsh", []string{"-c", "script"}},
		{"pwsh.exe", []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "script"}},
		{"powershell.exe", []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "script"}},
		{"cmd.exe", []string{"/d", "/s", "/c", "script"}},
	} {
		if got := shellArgs(tt.shell, "script"); !reflect.DeepEqual(got, tt.args) {
			t.Errorf("%s: %v", tt.shell, got)
		}
	}
}

func TestWindowsShellSelection(t *testing.T) {
	env := map[string]string{"SystemRoot": `C:\Windows`, "ProgramFiles": `C:\Program Files`, "ComSpec": `C:\Windows\System32\cmd.exe`}
	paths := map[string]string{"bash.exe": `C:\Windows\System32\bash.exe`, `C:\Program Files\Git\bin\bash.exe`: `C:\Program Files\Git\bin\bash.exe`, "pwsh.exe": "pwsh.exe"}
	lookup := func(s string) (string, error) {
		if p, ok := paths[s]; ok {
			return p, nil
		}
		return "", exec.ErrNotFound
	}
	get := func(s string) string { return env[s] }
	if got := selectWindowsShell(get, lookup); got != paths[`C:\Program Files\Git\bin\bash.exe`] {
		t.Fatal(got)
	}
	delete(paths, `C:\Program Files\Git\bin\bash.exe`)
	if got := selectWindowsShell(get, lookup); got != "pwsh.exe" {
		t.Fatal(got)
	}
	env["SHELL"] = "sh.exe"
	paths["sh.exe"] = "sh.exe"
	if got := selectWindowsShell(get, lookup); got != "sh.exe" {
		t.Fatal(got)
	}
	delete(paths, "sh.exe")
	delete(paths, "pwsh.exe")
	if got := selectWindowsShell(get, lookup); got != env["ComSpec"] {
		t.Fatal(got)
	}
}

func TestWindowsCmdVerbatimScript(t *testing.T) {
	shell := defaultShell()
	script := `echo "a b" & echo c`
	c := exec.Command(shell, shellArgs(shell, script)...)
	prepareShellCommand(c, shell, script)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got := strings.ReplaceAll(string(out), "\r\n", "\n"); got != "\"a b\" \nc\n" {
		t.Fatalf("unexpected cmd output %q", got)
	}
}
