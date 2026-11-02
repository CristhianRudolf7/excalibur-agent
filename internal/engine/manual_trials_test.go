package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func manualTrial(t *testing.T, e *Engine, r *Run, tc providers.ToolCall, output string, action func()) {
	t.Helper()
	before := e.captureTrialObservation(r, []providers.ToolCall{tc})
	if action != nil {
		action()
	}
	e.observeManualTrials(r, []providers.ToolCall{tc}, []tools.Result{{Status: tools.StatusOK, Output: output}}, before)
}
func manualEdit(t *testing.T, e *Engine, r *Run, path, body string) {
	t.Helper()
	manualTrial(t, e, r, trialCall("edit_file", map[string]string{"path": path}), "ok", func() { writeTrialFile(t, path, body) })
}
func TestManualTrialsHintOnceAfterFiveChecks(t *testing.T) {
	for _, output := range []string{"exit: 1\n3 warnings\n", "exit: 1\nassertion failed\n"} {
		t.Run(output, func(t *testing.T) {
			e, r, path, _ := probeRun(t)
			t.Setenv(probeSyncSwitch, "0") // The detector is independent of copy tracking.
			check := trialCall("exec", map[string]string{"command": "python3 validate.py " + path})
			for i := 0; i < 7; i++ {
				if i > 0 {
					manualEdit(t, e, r, path, fmt.Sprintf("choice=%d\n", i))
				}
				manualTrial(t, e, r, check, output, nil)
				if got := strings.Contains(probeNotes(r), manualTrialsNote); got != (i == 4) {
					t.Fatalf("check %d note=%v: %s", i+1, got, probeNotes(r))
				}
				if i == 4 {
					r.InjectControlNote("loop.manual_trials", "")
				}
				if i > 4 && probeNotes(r) != "" {
					t.Fatal("hint repeated")
				}
			}
		})
	}
}
func TestManualTrialsProgressAndNegativeCases(t *testing.T) {
	for _, scenario := range []string{"disabled", "assistant", "no-edit", "two-edits", "large-edit", "changing-check", "improving", "passing", "new-information", "read-only-probes", "unknown-write", "running"} {
		t.Run(scenario, func(t *testing.T) {
			e, r, path, _ := probeRun(t)
			if scenario == "disabled" {
				t.Setenv(manualTrialsSwitch, "off")
			}
			if scenario == "assistant" {
				r.Profile.ExplicitFinish = false
			}
			for i := 0; i < 7; i++ {
				if i > 0 && scenario != "no-edit" && scenario != "read-only-probes" {
					body := fmt.Sprintf("choice=%d\n", i)
					if scenario == "large-edit" {
						body = strings.Repeat(fmt.Sprint(i), 2048)
					}
					manualEdit(t, e, r, path, body)
					if scenario == "two-edits" {
						manualEdit(t, e, r, path, body+"extra")
					}
					if scenario == "unknown-write" {
						manualTrial(t, e, r, trialCall("apply_patch", map[string]string{"patch": "dynamic"}), "ok", nil)
					}
				}
				command := "python3 validate.py " + path
				if scenario == "changing-check" {
					command += fmt.Sprintf(" --mode=%d", i)
				}
				if scenario == "read-only-probes" {
					command = "cat " + path
				}
				output := "exit: 1\n8 warnings\n"
				if scenario == "improving" {
					output = fmt.Sprintf("exit: 1\n%d warnings\n", 8-i)
				}
				if scenario == "passing" {
					output = "exit: 0\n0 warnings\n"
				}
				if scenario == "new-information" {
					output = fmt.Sprintf("exit: 1\ndifferent finding %d\n", i)
				}
				if scenario == "running" {
					output = "exit: running  alive: true\n8 warnings\n"
				}
				manualTrial(t, e, r, trialCall("exec", map[string]string{"command": command}), output, nil)
			}
			if strings.Contains(probeNotes(r), manualTrialsNote) {
				t.Fatal(probeNotes(r))
			}
		})
	}
}
func TestManualTrialsImprovementRestartsWindow(t *testing.T) {
	e, r, path, _ := probeRun(t)
	check := trialCall("exec", map[string]string{"command": "python3 validate.py " + path})
	for i, n := range []int{9, 9, 8, 8, 8, 8, 8} {
		if i > 0 {
			manualEdit(t, e, r, path, fmt.Sprintf("choice=%d", i))
		}
		manualTrial(t, e, r, check, fmt.Sprintf("exit: 1\n%d warnings\n", n), nil)
		if got := strings.Contains(probeNotes(r), manualTrialsNote); got != (i == 6) {
			t.Fatal("wrong stagnant window", i, probeNotes(r))
		}
	}
}
func TestManualTrialsIgnoresReadBetweenEditAndCheck(t *testing.T) {
	e, r, path, _ := probeRun(t)
	for i := 0; i < 5; i++ {
		if i > 0 {
			manualEdit(t, e, r, path, fmt.Sprintf("choice=%d", i))
		}
		manualTrial(t, e, r, trialCall("read_file", map[string]string{"path": path}), "contents", nil)
		manualTrial(t, e, r, trialCall("exec", map[string]string{"command": "python3 validate.py " + path}), fmt.Sprintf("exit: 1  duration: %d.0s  session: sess-%d\n3 warnings", i, i), nil)
	}
	if !strings.Contains(probeNotes(r), manualTrialsNote) {
		t.Fatal(probeNotes(r))
	}
}
func TestSmallTrialEdit(t *testing.T) {
	cases := []struct {
		name           string
		a, b           trialFiles
		changed, small bool
	}{
		{"one", trialFiles{"a": "aaa"}, trialFiles{"a": "aba"}, true, true},
		{"unchanged", trialFiles{"a": "aaa"}, trialFiles{"a": "aaa"}, false, false},
		{"new", trialFiles{}, trialFiles{"a": "a"}, true, false},
		{"deleted", trialFiles{"a": "a"}, trialFiles{}, true, false},
		{"two", trialFiles{"a": "a", "b": "b"}, trialFiles{"a": "c", "b": "d"}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed, small := smallTrialEdit(tc.a, tc.b)
			if changed != tc.changed || small != tc.small {
				t.Fatal(changed, small)
			}
		})
	}
}
func TestManualTrialsBatchHook(t *testing.T) {
	e, r, path, _ := probeRun(t)
	runner := scriptedRunner()
	runner.add("edit_file", types.EffectWriteLocal, types.IdemNonIdempotent)
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent)
	_, js := testEngine(&fakeEndpoint{}, runner, passingGate())
	startRun(t, e, r, js)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for i := 0; i < 5; i++ {
		if i > 0 {
			body := fmt.Sprintf("choice=%d", i)
			e.deps.Tools = probeFileRunner{runner, func() { writeTrialFile(t, path, body) }}
			if _, err := e.execBatch(ctx, r, []providers.ToolCall{trialCall("edit_file", map[string]string{"path": path})}); err != nil {
				t.Fatal(err)
			}
		}
		runner.results["exec"] = []tools.Result{{Status: tools.StatusOK, Output: "exit: 1\n3 warnings\n"}}
		e.deps.Tools = runner
		if _, err := e.execBatch(ctx, r, []providers.ToolCall{trialCall("exec", map[string]string{"command": "python3 validate.py " + path})}); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(probeNotes(r), manualTrialsNote) {
		t.Fatal("batch hook missing", probeNotes(r))
	}
}
