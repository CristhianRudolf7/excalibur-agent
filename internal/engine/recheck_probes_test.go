package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestRecheckVaryingProbesDuringConfirmationAndRefusal(t *testing.T) {
	for _, pending := range []bool{false, true} {
		r := explicitRun()
		r.Workdir = t.TempDir()
		if pending {
			r.armConfirm(FinishArgs{})
		} else {
			r.LastRefusal = "verify output"
		}
		if r.recheckSnapshot() == "" {
			t.Fatal("missing snapshot")
		}
		for i := 1; i <= 16; i++ {
			tc := toolCall(fmt.Sprint(i), "exec", fmt.Sprintf(`{"command":"echo phase%d; ls ."}`, i))
			note := r.observeRecheck(tools.Result{Output: fmt.Sprintf("phase%d", i)}, true, tc)
			if (note != "") != (i%8 == 0) {
				t.Fatalf("pending=%v call=%d: %s", pending, i, note)
			}
			if pending && note != "" && !strings.Contains(note, confirmationReminder) {
				t.Fatal(note)
			}
		}
		r.observeRecheck(tools.Result{Output: "changed"}, false, toolCall("edit", "exec", `{"command":"echo edit"}`))
		if r.rechecks.consecutive != 0 {
			t.Fatal("edit failed to reset")
		}
	}
}

func TestDisplayProbeCommands(t *testing.T) {
	for _, cmd := range []string{"echo one; ls .", `printf 'apt-get; make'; true`, "cat a | head; tail a; wc a; test -f a", "grep -c pattern file", "git status; git diff --stat", "python3 -m py_compile source.py"} {
		if !displayProbeCommand(cmd) {
			t.Errorf("not probe: %s", cmd)
		}
	}
	for _, cmd := range []string{"echo one; make", "python3 run.py", "git diff", "grep pattern file", ""} {
		if displayProbeCommand(cmd) {
			t.Errorf("false probe: %s", cmd)
		}
	}
	t.Setenv("EXCALIBUR_RECHECK_PROBES", "off")
	r := &Run{finishConfirmPending: true, Workdir: t.TempDir()}
	t.Setenv("EXCALIBUR_FINISH_CONFIRM_PROGRESS", "off")
	if r.recheckSnapshot() != "" {
		t.Fatal("switch ignored")
	}
}
