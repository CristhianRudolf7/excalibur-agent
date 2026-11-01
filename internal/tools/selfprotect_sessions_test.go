package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestPgrepPipedToKillRefusesHarnessPattern(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux process inspection")
	}
	// The test binary's own command line carries its base name, so this
	// pattern matches the harness itself and the pipeline must be refused.
	base := regexp.QuoteMeta(filepath.Base(os.Args[0]))
	for _, cmd := range []string{
		fmt.Sprintf("pgrep -f '%s' | xargs kill -9", base),
		fmt.Sprintf("pgrep -f '%s' | xargs -r kill", base),
		fmt.Sprintf("pgrep -f '%s' | xargs pkill -f x", base),
	} {
		if err := guardSignals(cmd); err == nil {
			t.Errorf("allowed %s", cmd)
		}
	}
}

// unrelatedPattern is unique to this test process, so it cannot match an
// ancestor's command line (the calling shell may carry test text).
func unrelatedPattern() string {
	return fmt.Sprintf("w22-unrelated-%d-zz", os.Getpid())
}

func TestPgrepAloneAndUnrelatedKillsAllowed(t *testing.T) {
	u := unrelatedPattern()
	for _, cmd := range []string{
		"pgrep -f " + u,
		"pgrep -l -f " + u,
		"pgrep -f " + u + " | xargs kill -9",
		"pgrep -f " + u + " | xargs -r kill",
		"echo 4242 | xargs kill",
		"kill -9 9999",
	} {
		if err := guardSignals(cmd); err != nil {
			t.Errorf("%s: %v", cmd, err)
		}
	}
	// Literal PIDs behind xargs are still checked against the harness.
	if err := guardSignals(fmt.Sprintf("echo 1 | xargs kill -9 %d", os.Getpid())); err == nil {
		t.Error("xargs kill of the harness PID accepted")
	}
	// A kill with no operands at all is still refused.
	if err := guardSignals("kill -9"); err == nil {
		t.Error("bare kill accepted")
	}
}

func TestSessionWarnsWhenPatternMatchesOwnSession(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux process inspection")
	}
	m := NewSessionManager(TerminalDeps{})
	marker := fmt.Sprintf("w22-marker-%d", os.Getpid())
	// The shell stays alive (two commands), so its argv keeps the marker.
	active, _, _ := m.Start("test", fmt.Sprintf("sleep 30; : %s", marker), "", false, 250, nil)
	defer m.Close(active.ID)
	_, out, _ := m.Start("test", "pkill -f "+marker, "", false, 2000, nil)
	want := fmt.Sprintf("matches active session %s", active.ID)
	if !strings.Contains(out, want) {
		t.Fatalf("warning missing (want %q): %s", want, out)
	}
	if strings.Contains(out, "refused") {
		t.Fatalf("pattern refused instead of warned: %s", out)
	}
}

func TestSessionWarningSkipsUnrelatedPattern(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux process inspection")
	}
	m := NewSessionManager(TerminalDeps{})
	active, _, _ := m.Start("test", "sleep 30", "", false, 250, nil)
	defer m.Close(active.ID)
	warnings, err := signalPreflight(m, "pkill -f "+unrelatedPattern(), "")
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	// Only the command's own shell may be named, never the unrelated session.
	for _, w := range warnings {
		if strings.Contains(w, "active session") {
			t.Fatalf("unrelated session named: %v", warnings)
		}
	}
}

func TestSelectorParsing(t *testing.T) {
	if _, err := parseSelector("pgrep", []string{"-l", "-a", "-f", "x"}); err != nil {
		t.Fatalf("pgrep flags refused: %v", err)
	}
	if _, err := parseSelector("pkill", []string{"-l", "x"}); err == nil {
		t.Fatal("pkill unknown flag accepted")
	}
	if _, err := parseSelector("killall", []string{"a/b"}); err == nil {
		t.Fatal("killall path selector accepted")
	}
	if _, err := parseSelector("pkill", []string{"a", "b"}); err == nil {
		t.Fatal("pkill with two patterns accepted")
	}
}

func TestRefusalTextIsStable(t *testing.T) {
	// The refusal prefix is the model-visible contract for self-kill
	// attempts; keep it stable.
	err := refusalError(fmt.Errorf("process pattern matches protected PID 1"))
	if !strings.Contains(err.Error(), "self-kill guard refused command") {
		t.Fatalf("refusal text: %v", err)
	}
}
