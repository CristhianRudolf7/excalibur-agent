package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func probeRun(t *testing.T) (*Engine, *Run, string, string) {
	t.Helper()
	e, r, path := earlyOutputRun(t)
	writeTrialFile(t, path, "alpha\nbeta\n")
	r.Request = "Edit the required file `" + path + "` in place."
	return e, r, path, filepath.Join(r.Workdir, "probe", "candidate.csv")
}
func writeTrialFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func trialCall(name string, args map[string]string) providers.ToolCall {
	body, _ := json.Marshal(args)
	return toolCall("trial", name, string(body))
}
func observeTrial(t *testing.T, e *Engine, r *Run, tc providers.ToolCall, output string, action func()) {
	t.Helper()
	before := e.captureTrialObservation(r, []providers.ToolCall{tc})
	if action != nil {
		action()
	}
	e.observeProbeCopies(r, []providers.ToolCall{tc}, []tools.Result{{Status: tools.StatusOK, Output: output}}, before)
	e.probeSyncCheck(r, r.StartedAt)
}
func createTrialCopy(t *testing.T, e *Engine, r *Run, original, copy string) {
	t.Helper()
	observeTrial(t, e, r, trialCall("exec", map[string]string{"command": "cp " + original + " " + copy}), "exit: 0\n", func() { writeTrialFile(t, copy, "alpha\nbeta\n") })
}
func editTrialCopy(t *testing.T, e *Engine, r *Run, copy, content string) {
	t.Helper()
	observeTrial(t, e, r, trialCall("write_file", map[string]string{"path": copy, "content": content}), "ok", func() { writeTrialFile(t, copy, content) })
}
func probeNotes(r *Run) string { return strings.Join(r.VolatileNotes(), "\n") }

func TestProbeSyncImprovedOrPassingCopy(t *testing.T) {
	for _, output := range []string{"exit: 0\n0 warnings\n", "exit: 1\n2 warnings\n"} {
		t.Run(output, func(t *testing.T) {
			e, r, original, copy := probeRun(t)
			createTrialCopy(t, e, r, original, copy)
			check := trialCall("exec", map[string]string{"command": "python3 validate.py " + copy})
			observeTrial(t, e, r, check, "exit: 1\n3 warnings\n", nil)
			editTrialCopy(t, e, r, copy, "alpha\nfixed\n")
			observeTrial(t, e, r, check, output, nil)
			if !strings.Contains(probeNotes(r), "the deliverable at "+original+" is unchanged — copy your best version back now") {
				t.Fatal(probeNotes(r))
			}
			body, _ := os.ReadFile(original)
			if string(body) != "alpha\nbeta\n" {
				t.Fatal("advice must never copy files")
			}
		})
	}
}
func TestProbeSyncHalfwayAndReserveOnce(t *testing.T) {
	e, r, original, copy := probeRun(t)
	createTrialCopy(t, e, r, original, copy)
	editTrialCopy(t, e, r, copy, "alpha\nchanged\n")
	e.probeSyncCheck(r, r.StartedAt.Add(49*time.Second))
	if probeNotes(r) != "" {
		t.Fatal(probeNotes(r))
	}
	e.probeSyncCheck(r, r.StartedAt.Add(50*time.Second))
	if !strings.Contains(probeNotes(r), "you are editing a copy") {
		t.Fatal(probeNotes(r))
	}
	r.InjectControlNote("budget.probe_sync."+trialDigest(original), "")
	e.probeSyncCheck(r, r.StartedAt.Add(69*time.Second))
	if probeNotes(r) != "" {
		t.Fatal("first reminder repeated")
	}
	e.probeSyncCheck(r, r.StartedAt.Add(70*time.Second))
	if !strings.Contains(probeNotes(r), "you are editing a copy") {
		t.Fatal("reserve reminder missing")
	}
	r.InjectControlNote("budget.probe_sync."+trialDigest(original), "")
	e.probeSyncCheck(r, r.StartedAt.Add(90*time.Second))
	if probeNotes(r) != "" {
		t.Fatal("reserve reminder repeated")
	}
}
func TestProbeSyncNoFalseReminder(t *testing.T) {
	for _, scenario := range []string{"disabled", "assistant", "unchanged-copy", "synced", "deliverable-edited", "not-required", "assumed-budget", "unrelated-check", "no-improvement", "existing-duplicate", "request-changed"} {
		t.Run(scenario, func(t *testing.T) {
			e, r, original, copy := probeRun(t)
			switch scenario {
			case "disabled":
				t.Setenv(probeSyncSwitch, "0")
			case "assistant":
				r.Profile.ExplicitFinish = false
			case "not-required":
				r.Request = "Read the file `" + original + "`."
			case "existing-duplicate":
				writeTrialFile(t, copy, "alpha\nbeta\n")
			}
			createTrialCopy(t, e, r, original, copy)
			if scenario != "unchanged-copy" {
				editTrialCopy(t, e, r, copy, "alpha\nchanged\n")
			}
			check := trialCall("exec", map[string]string{"command": "python3 validate.py " + copy})
			switch scenario {
			case "synced":
				writeTrialFile(t, original, "alpha\nchanged\n")
			case "deliverable-edited":
				writeTrialFile(t, original, "other\n")
			case "request-changed":
				r.Request = "Read documentation."
			case "assumed-budget":
				r.Profile.WallSource = "default"
			case "unrelated-check":
				check = trialCall("exec", map[string]string{"command": "python3 validate.py " + filepath.Join(r.Workdir, "other.csv")})
			}
			if scenario == "unrelated-check" || scenario == "no-improvement" {
				observeTrial(t, e, r, check, "exit: 1\n4 warnings", nil)
			}
			now := r.StartedAt.Add(80 * time.Second)
			if scenario == "unrelated-check" || scenario == "no-improvement" {
				now = r.StartedAt.Add(10 * time.Second)
			}
			e.probeSyncCheck(r, now)
			if strings.Contains(probeNotes(r), "you are editing a copy") {
				t.Fatal(probeNotes(r))
			}
		})
	}
}
func TestProbeSyncCreationForms(t *testing.T) {
	for _, kind := range []string{"cp-directory", "rsync", "write-identical", "inline-write", "copy-and-edit", "directory-copy", "quoted-path"} {
		t.Run(kind, func(t *testing.T) {
			e, r, original, copy := probeRun(t)
			command := "cp " + original + " " + copy
			content := "alpha\nbeta\n"
			var tc providers.ToolCall
			switch kind {
			case "cp-directory":
				copy = filepath.Join(r.Workdir, "probe", filepath.Base(original))
				_ = os.MkdirAll(filepath.Dir(copy), 0700)
				command = "cp " + original + " " + filepath.Dir(copy)
			case "rsync":
				command = "rsync -a " + original + " " + copy
			case "write-identical":
				tc = trialCall("write_file", map[string]string{"path": copy, "content": content})
			case "inline-write":
				command = "python3 -c 'import shutil; shutil.copy(\"" + original + "\", \"" + copy + "\")'"
			case "copy-and-edit":
				command += " && sed -i s/beta/fixed/ " + copy
				content = "alpha\nfixed\n"
			case "directory-copy":
				source := filepath.Join(r.Workdir, "source")
				original = filepath.Join(source, "file.dat")
				writeTrialFile(t, original, content)
				r.Request = "Edit the required file `" + original + "` in place."
				copy = filepath.Join(r.Workdir, "probe", "file.dat")
				command = "cp -r " + source + " " + filepath.Dir(copy)
			case "quoted-path":
				copy = filepath.Join(r.Workdir, "probe", "some candidate.csv")
				command = "cp '" + original + "' '" + copy + "'"
			}
			if tc.Name == "" {
				tc = trialCall("exec", map[string]string{"command": command})
			}
			observeTrial(t, e, r, tc, "exit: 0\n", func() { writeTrialFile(t, copy, content) })
			editTrialCopy(t, e, r, copy, "alpha\nbest\n")
			e.probeSyncCheck(r, r.StartedAt.Add(50*time.Second))
			if !strings.Contains(probeNotes(r), "you are editing a copy") {
				t.Fatal("missing copy observation", kind, r.probes)
			}
		})
	}
}
func TestProbeSyncRetiresAfterSync(t *testing.T) {
	e, r, original, copy := probeRun(t)
	createTrialCopy(t, e, r, original, copy)
	editTrialCopy(t, e, r, copy, "alpha\nbest\n")
	e.probeSyncCheck(r, r.StartedAt.Add(50*time.Second))
	writeTrialFile(t, original, "alpha\nbest\n")
	e.probeSyncCheck(r, r.StartedAt.Add(70*time.Second))
	if strings.Contains(probeNotes(r), "you are editing a copy") {
		t.Fatal("stale copy note", probeNotes(r))
	}
}
func TestTrialFilesBoundsAndSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	writeTrialFile(t, filepath.Join(dir, "large.dat"), strings.Repeat("a", trialSingleFileLimit+1))
	writeTrialFile(t, filepath.Join(dir, "small.dat"), "small")
	_ = os.Symlink(filepath.Join(dir, "small.dat"), filepath.Join(dir, "link.dat"))
	files := readTrialFiles([]string{dir})
	if len(files) != 1 || files[filepath.Join(dir, "small.dat")] != "small" {
		t.Fatal("bounds/symlink", files)
	}
}

// Exercise the real batch hook with fake tool execution and a bounded step.
type probeFileRunner struct {
	*fakeRunner
	action func()
}

func (r probeFileRunner) Execute(ctx context.Context, name string, args json.RawMessage, opts tools.ExecuteOptions) (tools.Result, error) {
	r.action()
	return r.fakeRunner.Execute(ctx, name, args, opts)
}
func TestProbeSyncBatchHook(t *testing.T) {
	e, r, original, copy := probeRun(t)
	runner := scriptedRunner()
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent, tools.Result{Status: tools.StatusOK, Output: "exit: 0\n"})
	e.deps.Tools = probeFileRunner{runner, func() { writeTrialFile(t, copy, "alpha\nbeta\n") }}
	_, js := testEngine(&fakeEndpoint{}, runner, passingGate())
	startRun(t, e, r, js)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := e.execBatch(ctx, r, []providers.ToolCall{trialCall("exec", map[string]string{"command": "cp " + original + " " + copy})}); err != nil {
		t.Fatal(err)
	}
	if len(r.probes.copies) != 1 {
		t.Fatal("batch failed to observe copy", r.probes)
	}
}

func TestProbeSyncMultipleCopiesShareReminder(t *testing.T) {
	e, r, original, copy := probeRun(t)
	createTrialCopy(t, e, r, original, copy)
	other := filepath.Join(r.Workdir, "probe", "other.csv")
	createTrialCopy(t, e, r, original, other)
	editTrialCopy(t, e, r, copy, "alpha\nbest\n")
	e.probeSyncCheck(r, r.StartedAt.Add(50*time.Second))
	if !strings.Contains(probeNotes(r), "you are editing a copy") {
		t.Fatal("unchanged sibling erased reminder", probeNotes(r))
	}
	r.InjectControlNote("budget.probe_sync."+trialDigest(original), "")
	editTrialCopy(t, e, r, other, "alpha\nother\n")
	e.probeSyncCheck(r, r.StartedAt.Add(60*time.Second))
	if probeNotes(r) != "" {
		t.Fatal("another copy repeated the first reminder", probeNotes(r))
	}
	e.probeSyncCheck(r, r.StartedAt.Add(70*time.Second))
	if !strings.Contains(probeNotes(r), "you are editing a copy") {
		t.Fatal("reserve reminder missing")
	}
}
func TestProbeSyncRetiresLegacyAndChangedRequest(t *testing.T) {
	for _, scenario := range []string{"legacy", "request"} {
		t.Run(scenario, func(t *testing.T) {
			if scenario == "legacy" {
				t.Setenv("EXCALIBUR_CONTROL_TAIL", "off")
			}
			e, r, original, copy := probeRun(t)
			createTrialCopy(t, e, r, original, copy)
			editTrialCopy(t, e, r, copy, "alpha\nbest\n")
			e.probeSyncCheck(r, r.StartedAt.Add(50*time.Second))
			if scenario == "request" {
				r.Request = "Read documentation."
			} else {
				writeTrialFile(t, original, "alpha\nbest\n")
			}
			e.probeSyncCheck(r, r.StartedAt.Add(60*time.Second))
			if strings.Contains(probeNotes(r), "you are editing a copy") {
				t.Fatal("stale reminder", probeNotes(r))
			}
		})
	}
}
func TestProbeSyncCombinedEditAndFailedCheckImprovement(t *testing.T) {
	e, r, original, copy := probeRun(t)
	createTrialCopy(t, e, r, original, copy)
	for i, n := range []int{4, 3} {
		tc := trialCall("exec", map[string]string{"command": fmt.Sprintf("sed -i s/choice/choice%d/ %s && python3 validate.py %s", i, copy, copy)})
		observeTrial(t, e, r, tc, fmt.Sprintf("exit: 1\n%d warnings\n", n), func() { writeTrialFile(t, copy, fmt.Sprintf("choice%d\n", i)) })
	}
	if !strings.Contains(probeNotes(r), "you are editing a copy") {
		t.Fatal("combined check lost improvement", probeNotes(r))
	}
}
func TestProbeSyncStepHook(t *testing.T) {
	e, r, original, copy := probeRun(t)
	createTrialCopy(t, e, r, original, copy)
	editTrialCopy(t, e, r, copy, "alpha\nbest\n")
	runner := scriptedRunner()
	runner.add("read_file", types.EffectReadOnly, types.IdemPure)
	e.deps.Tools = runner
	e.deps.Models = &fakeEndpoint{responses: []providers.ChatResponse{toolResp(trialCall("read_file", map[string]string{"path": original}))}}
	_, js := testEngine(&fakeEndpoint{}, runner, passingGate())
	startRun(t, e, r, js)
	e.deps.Clock = func() time.Time { return r.StartedAt.Add(50 * time.Second) }
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := e.step(ctx, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(probeNotes(r), "you are editing a copy") {
		t.Fatal("step hook missing", probeNotes(r))
	}
}

func TestProbeSyncUnrelatedFinalCheckDoesNotVerifyCopy(t *testing.T) {
	e, r, original, copy := probeRun(t)
	createTrialCopy(t, e, r, original, copy)
	tc := trialCall("exec", map[string]string{"command": "sed -i s/alpha/best/ " + copy + " && python3 validate.py unrelated.csv"})
	observeTrial(t, e, r, tc, "exit: 0\n0 warnings\n", func() { writeTrialFile(t, copy, "best\nbeta\n") })
	if strings.Contains(probeNotes(r), "you are editing a copy") {
		t.Fatal("unrelated check verified copy", probeNotes(r))
	}
}
func TestProbeSyncCheckerDirectoryContext(t *testing.T) {
	e, r, original, copy := probeRun(t)
	createTrialCopy(t, e, r, original, copy)
	editTrialCopy(t, e, r, copy, "alpha\nbest\n")
	tc := trialCall("exec", map[string]string{"command": "cd '" + filepath.Dir(copy) + "' && python3 ../validate.py candidate.csv"})
	observeTrial(t, e, r, tc, "exit: 0\n0 warnings\n", nil)
	if !strings.Contains(probeNotes(r), "you are editing a copy") {
		t.Fatal("checker directory context lost copy", probeNotes(r))
	}
}
