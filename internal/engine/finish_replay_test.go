// End-to-end finish-protocol tests. The first test records a run with the
// eval recorder (Section 23.6), replays it with the eval player and checks
// that the replay makes no live call and reaches the same state; the
// second test changes the instruction and checks that replay stops at the
// first divergence. The third test drives a live scripted run through the
// no-change refusal and a blocked claim that the strict rules refuse.
package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// transcriptBuilder renders the request the way the context compiler does
// for these tests: the instruction, the tool results and the notes.
type transcriptBuilder struct{}

func (transcriptBuilder) BuildRequest(_ context.Context, run *Run, _ bool) (providers.ChatRequest, ctxcompiler.Compiled, error) {
	run.mu.Lock()
	request := run.Request
	run.mu.Unlock()
	msgs := []providers.Message{{Role: providers.RoleUser, Content: request}}
	for _, ev := range run.historyCopy() {
		switch {
		case ev.ToolResult != nil:
			msgs = append(msgs, providers.Message{Role: providers.RoleTool, Content: ev.ToolResult.Output})
		case ev.Content != "":
			msgs = append(msgs, providers.Message{Role: providers.RoleAssistant, Content: ev.Content})
		}
	}
	for _, n := range run.VolatileNotes() {
		msgs = append(msgs, providers.Message{Role: providers.RoleUser, Content: n})
	}
	return providers.ChatRequest{Messages: msgs}, ctxcompiler.Compiled{}, nil
}

// fileRunner writes files for write_file calls so the artifact checks see
// real files; every other tool uses the scripted runner.
type fileRunner struct{ *fakeRunner }

func (f fileRunner) Execute(ctx context.Context, name string, args json.RawMessage, opts tools.ExecuteOptions) (tools.Result, error) {
	if name != "write_file" {
		return f.fakeRunner.Execute(ctx, name, args, opts)
	}
	var a struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	if err := os.WriteFile(a.Path, []byte(a.Content), 0o644); err != nil {
		return tools.Result{Status: tools.StatusToolError, Output: err.Error()}, nil
	}
	return tools.Result{Status: tools.StatusOK, Output: "wrote " + a.Path}, nil
}

// recordingModels serves the scripted turns and records each exchange.
type recordingModels struct {
	inner *fakeEndpoint
	rec   *eval.Recorder
}

func (m *recordingModels) Chat(ctx context.Context, role types.ModelRole, req providers.ChatRequest) (providers.ChatResponse, models.CallMeta, error) {
	resp, meta, err := m.inner.Chat(ctx, role, req)
	if err == nil {
		if rerr := m.rec.Record(replayRequest(req), resp); rerr != nil {
			return resp, meta, rerr
		}
	}
	return resp, meta, err
}

// replayModels answers from a recording and never calls a live model.
type replayModels struct{ player *eval.Player }

func (m *replayModels) Chat(_ context.Context, _ types.ModelRole, req providers.ChatRequest) (providers.ChatResponse, models.CallMeta, error) {
	resp, err := m.player.Replay(replayRequest(req))
	if err != nil {
		return providers.ChatResponse{}, models.CallMeta{}, err
	}
	return resp, models.CallMeta{Provider: "replay", Model: "recorded"}, nil
}

func replayRequest(req providers.ChatRequest) eval.ReplayRequest {
	return eval.ReplayRequest{Model: req.Model, Messages: req.Messages, Tools: req.Tools}
}

// finishE2EEngine wires the engine for an end-to-end run: no verifier
// model calls (refine and strong verification off), the artifact gate and
// the deterministic deliverable checks on, scope guard off.
func finishE2EEngine(t *testing.T, m ModelEndpoint, runner *fakeRunner) (*Engine, *journal.MemStore) {
	t.Helper()
	t.Setenv("EXCALIBUR_SCOPE_GUARD", "off")
	js := journal.NewMemStore()
	e := New(Deps{Models: m, Context: transcriptBuilder{}, Tools: fileRunner{runner}, Gate: passingGate()})
	cfg := tasks.DefaultSelfVerifyConfig()
	cfg.EnableRefine = false
	cfg.EnableStrong = false
	e.deps.SelfVerify = &cfg
	return e, js
}

// programTurns is the scripted run: the agent writes a program, the first
// finish is refused because the program never ran, then it runs the
// program, passes the first finish and ends with the second.
func programTurns(target string) (*fakeRunner, []providers.ChatResponse) {
	runner := scriptedRunner()
	runner.add("write_file", types.EffectWriteLocal, types.IdemIdempotent)
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent,
		tools.Result{Status: tools.StatusOK, Output: "exit: 0  duration: 0.1s  session: sess-1  alive: false\n3"})
	turns := []providers.ChatResponse{
		toolResp(toolCall("w1", "write_file", `{"path":`+strconvQuote(target)+`,"content":"print(3)\n"}`)),
		toolResp(toolCall("f1", "finish", finishArgs("wrote the script", "file written"))),
		toolResp(toolCall("e1", "exec", `{"command":`+strconvQuote("python3 "+target)+`}`)),
		toolResp(toolCall("f2", "finish", finishArgs("ran the script", "exit 0 printed 3"))),
		toolResp(toolCall("f3", "finish", finishArgs("ran the script", "exit 0 printed 3"))),
	}
	return runner, turns
}

// refusalTexts returns the finish tool results of a run, in order.
func refusalTexts(run *Run) []string {
	var out []string
	for _, ev := range run.historyCopy() {
		if ev.ToolResult != nil && ev.ToolResult.Name == FinishToolName {
			out = append(out, ev.ToolResult.Output)
		}
	}
	return out
}

// Record a refused-then-accepted run, then replay it: the replay serves
// every recorded response, makes no live call and reaches the same state.
// The refusal is also visible to the model in its next request.
func TestFinishReplayRecordedRunReachesSameState(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "total.py")
	recPath := filepath.Join(t.TempDir(), "finish.jsonl")

	rec, err := eval.NewRecorder(recPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rec.Close() })
	runner, turns := programTurns(target)
	live := &fakeEndpoint{responses: turns}
	e, js := finishE2EEngine(t, &recordingModels{inner: live, rec: rec}, runner)
	run := NewRun("run_replay", "task_replay", Profile{ExplicitFinish: true, Role: types.RoleExecutor})
	run.Request = "Create " + target + " that prints the total of its input."
	run.Workdir = dir
	startRun(t, e, run, js)
	if err := e.Run(finishTestContext(t), run); err != nil {
		t.Fatal(err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("%v results=%v", err, refusalTexts(run))
	}
	if run.State() != types.RunCompleted {
		t.Fatalf("state=%v results=%v", run.State(), refusalTexts(run))
	}
	mustState(t, run, types.RunCompleted)
	if run.FinishRefusals != 1 || live.calls() != 5 {
		t.Fatalf("recorded run: refusals=%d calls=%d, want 1 and 5", run.FinishRefusals, live.calls())
	}
	// The refusal reached the model: the request after the refused finish
	// carries the numbered refusal as a tool message.
	seen := false
	for _, m := range live.requests[2].Messages {
		if m.Role == providers.RoleTool && strings.HasPrefix(m.Content, "finish refused (attempt 1): 1) ") {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("model did not see the refusal as the finish tool result: %+v", live.requests[2].Messages)
	}

	// Replay: same workdir state (the script is removed), same instruction.
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	player, err := eval.LoadPlayer(recPath)
	if err != nil {
		t.Fatal(err)
	}
	replayRunner, _ := programTurns(target)
	e2, js2 := finishE2EEngine(t, &replayModels{player: player}, replayRunner)
	replayed := NewRun("run_replay", "task_replay", Profile{ExplicitFinish: true, Role: types.RoleExecutor})
	replayed.Request = run.Request
	replayed.Workdir = dir
	startRun(t, e2, replayed, js2)
	if err := e2.Run(context.Background(), replayed); err != nil {
		t.Fatalf("replay must finish without divergence: %v", err)
	}
	mustState(t, replayed, types.RunCompleted)
	if !player.Done() || replayed.FinishRefusals != 1 {
		t.Fatalf("replay: done=%v refusals=%d, want all records served and 1 refusal", player.Done(), replayed.FinishRefusals)
	}
	if got, want := refusalTexts(replayed), refusalTexts(run); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("replayed finish results differ from the recording:\n%v\n%v", got, want)
	}
}

// Changing the instruction makes the replay diverge at its first request:
// the run fails with the divergence and no later response is served.
func TestFinishReplayStopsAtFirstDivergence(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "total.py")
	recPath := filepath.Join(t.TempDir(), "finish.jsonl")
	rec, err := eval.NewRecorder(recPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rec.Close() })
	runner, turns := programTurns(target)
	e, js := finishE2EEngine(t, &recordingModels{inner: &fakeEndpoint{responses: turns}, rec: rec}, runner)
	run := NewRun("run_div", "task_div", Profile{ExplicitFinish: true, Role: types.RoleExecutor})
	run.Request = "Create " + target + " that prints the total of its input."
	run.Workdir = dir
	startRun(t, e, run, js)
	if err := e.Run(finishTestContext(t), run); err != nil {
		t.Fatal(err)
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
	player, err := eval.LoadPlayer(recPath)
	if err != nil {
		t.Fatal(err)
	}
	e2, js2 := finishE2EEngine(t, &replayModels{player: player}, scriptedRunner())
	changed := NewRun("run_div", "task_div", Profile{ExplicitFinish: true, Role: types.RoleExecutor})
	changed.Request = "Create " + target + " that prints the sum of its input."
	changed.Workdir = dir
	startRun(t, e2, changed, js2)
	err = e2.Run(context.Background(), changed)
	if err == nil || !strings.Contains(err.Error(), "diverged at call 0") {
		t.Fatalf("changed instruction must diverge at the first call, got %v", err)
	}
	mustState(t, changed, types.RunFailed)
	if player.Done() {
		t.Fatal("no recorded response may be served after a divergence")
	}
}

// A live run: a refused finish, a repeated finish with no tool work (the
// no-change refusal, answered without the verifier), a blocked claim with
// no edit attempted (refused by the strict rules), then the work and an
// accepted finish. Refusals never end the run.
func TestFinishRefusalsNeverEndTheRun(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "report.txt")
	runner := scriptedRunner()
	runner.add("write_file", types.EffectWriteLocal, types.IdemIdempotent)
	runner.add("exec", types.EffectExec, types.IdemNonIdempotent, tools.Result{Status: tools.StatusOK, Output: "exit: 0\n"})
	live := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("f1", "finish", finishArgs("done", "none yet"))),
		toolResp(toolCall("f2", "finish", finishArgs("done", "again"))),
		toolResp(toolCall("c1", "exec", `{"command":`+strconvQuote("ls "+dir)+`}`)),
		toolResp(toolCall("f3", "finish", finishBlockedArgs("stuck", "tried", "the directory is read only"))),
		toolResp(toolCall("w1", "write_file", `{"path":`+strconvQuote(target)+`,"content":"summary\n"}`)),
		toolResp(toolCall("c2", "exec", `{"command":`+strconvQuote("cat "+target)+`}`)),
		toolResp(toolCall("f4", "finish", finishArgs("written", "file exists"))),
		toolResp(toolCall("f5", "finish", finishArgs("written", "file exists"))),
	}}
	e, js := finishE2EEngine(t, live, runner)
	run := NewRun("run_live", "task_live", Profile{ExplicitFinish: true, Role: types.RoleExecutor})
	run.Request = "Create " + target + " with the summary."
	run.Workdir = dir
	startRun(t, e, run, js)
	if err := e.Run(finishTestContext(t), run); err != nil {
		t.Fatalf("%v results=%v", err, refusalTexts(run))
	}
	if run.State() != types.RunCompleted {
		t.Fatalf("state=%v results=%v", run.State(), refusalTexts(run))
	}
	mustState(t, run, types.RunCompleted)
	if live.calls() != 8 {
		t.Fatalf("model calls = %d, want 8: a refusal must not end the run", live.calls())
	}
	texts := refusalTexts(run)
	if len(texts) < 4 || !strings.Contains(texts[0], "required artifact missing") {
		t.Fatalf("first refusal must name the missing artifact: %v", texts)
	}
	if !strings.HasPrefix(texts[1], "finish refused: nothing changed since the previous refusal (same reasons)") {
		t.Fatalf("repeated finish with no tool work must be answered without the verifier: %q", texts[1])
	}
	if !strings.Contains(texts[2], "strict blocked") && !strings.Contains(texts[2], "named deliverable") {
		t.Fatalf("blocked claim with no attempt must be refused by the strict rules: %q", texts[2])
	}
	if run.FinishRefusals != 3 {
		t.Fatalf("refusals = %d, want 3", run.FinishRefusals)
	}
}

func finishTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}
