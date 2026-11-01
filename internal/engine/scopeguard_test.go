package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScopeGuardFinishPaths(t *testing.T) {
	t.Setenv("EXCALIBUR_SCOPE_GUARD", "on")
	root := t.TempDir()
	p := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(p, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	run := explicitRun()
	run.Workdir = root
	run.Request = "Do not change keep.txt"
	e.startScopeGuard(run)
	if run.scopeGuard == nil {
		t.Fatal("guard inactive")
	}
	if err := os.WriteFile(p, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if reason := e.validateFinish(context.Background(), run, FinishArgs{Status: "completed"}, "verified"); !strings.Contains(reason, p) {
		t.Fatal(reason)
	}
	startRun(t, e, run, js)
	if stop, err := e.finishTurn(context.Background(), run, "done"); err != nil || stop {
		t.Fatalf("text finish accepted: %v %v", stop, err)
	}
	// A later step must not silently replace the original snapshot.
	e.startScopeGuard(run)
	if reason := scopeRefusal(run); !strings.Contains(reason, p) {
		t.Fatal(reason)
	}
}

func TestScopeGuardSwitchOff(t *testing.T) {
	t.Setenv("EXCALIBUR_SCOPE_GUARD", "off")
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	run := explicitRun()
	run.Workdir = t.TempDir()
	run.Request = "Do not change input.txt"
	e.startScopeGuard(run)
	if run.scopeGuard != nil || run.scopeGuardStarted {
		t.Fatal("off switch ignored")
	}
}
