package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func init() { InlineBackground = true }

func testProject(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// TestTaskRoundTrip covers the Section 3.6 CLI family end to end:
// start returns immediately with an id, status snapshots, wait maps
// to the exec codes, result carries the 7.6 delivery report, and
// list/logs expose the task.
func TestTaskRoundTrip(t *testing.T) {
	proj := testProject(t)
	started, err := Start(StartOptions{Request: "implement it", ProjectDir: proj, Inline: true})
	if err != nil {
		t.Fatal(err)
	}
	if started.Short != "T1" {
		t.Fatalf("first short = %q, want T1", started.Short)
	}
	st, err := Status(proj, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if st.TaskID != started.TaskID || st.State != types.TaskCompleted {
		t.Fatalf("status = %+v", st)
	}
	w, err := Wait(proj, string(started.TaskID), 5)
	if err != nil {
		t.Fatal(err)
	}
	if w.State != types.TaskCompleted {
		t.Fatalf("wait = %+v", w)
	}
	if code := CodeForTaskState(w.State); code != ExitOK {
		t.Fatalf("wait exit = %d", code)
	}
	res, err := Result(proj, string(started.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary == "" {
		t.Fatal("result needs the delivery summary")
	}
	items, err := ListTasks(proj)
	if err != nil || len(items) != 1 {
		t.Fatalf("list = %v, %v", items, err)
	}
	events, err := TaskLogs(proj, "T1")
	if err != nil || len(events) == 0 {
		t.Fatalf("logs = %v, %v", events, err)
	}
	// Artifacts sit with the task: ATIF, metrics, result.
	for _, name := range []string{"trajectory.json", "metrics.json", "result.json", "events.jsonl"} {
		store, _ := ResolveStore(proj)
		if _, err := os.Stat(filepath.Join(store.TaskPath(started.TaskID), name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}

// TestTaskQuestionReply covers the Section 3.7 delegation row: wait
// returns the question, reply answers it, and the task continues
// with the answer.
func TestTaskQuestionReply(t *testing.T) {
	proj := testProject(t)
	started, err := Start(StartOptions{
		Request: "deploy it {{ask: which host?}}", ProjectDir: proj, Inline: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w, err := Wait(proj, string(started.TaskID), 5)
	if err != nil {
		t.Fatal(err)
	}
	if w.State != types.TaskInProgress || w.Question != "which host?" {
		t.Fatalf("wait = %+v, want the pending question", w)
	}
	st, err := Reply(proj, string(started.TaskID), "edge-01")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != types.TaskCompleted {
		t.Fatalf("after reply = %+v", st)
	}
	if st.Question != "" {
		t.Fatal("question must clear after the answer")
	}
	res, err := Result(proj, string(started.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary == "" {
		t.Fatal("resumed task needs a delivery summary")
	}
}

// TestTaskNeverAsk covers the never_ask branch: the question becomes
// a recorded assumption and the task completes without parking.
func TestTaskNeverAsk(t *testing.T) {
	proj := testProject(t)
	started, err := Start(StartOptions{
		Request: "deploy it {{ask: which host?}}", ProjectDir: proj,
		NeverAsk: true, Inline: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := Status(proj, string(started.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	if st.State != types.TaskCompleted || st.Question != "" {
		t.Fatalf("never_ask = %+v", st)
	}
	res, err := Result(proj, string(started.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Assumptions) != 1 {
		t.Fatalf("assumptions = %v", res.Assumptions)
	}
}

// TestTaskControl covers pause/resume/cancel and the unknown-task
// errors of Section 15.8 over the delegation family.
func TestTaskControl(t *testing.T) {
	proj := testProject(t)
	started, err := Start(StartOptions{
		Request: "waiting {{ask: proceed?}}", ProjectDir: proj, Inline: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := Pause(proj, string(started.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	if p.State != types.TaskPaused {
		t.Fatalf("pause = %+v", p)
	}
	// A paused task resumes and parks on its question again.
	r, err := Resume(proj, string(started.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	if r.State != types.TaskInProgress || r.Question == "" {
		t.Fatalf("resume = %+v", r)
	}
	c, err := Cancel(proj, string(started.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	if c.State != types.TaskCanceled {
		t.Fatalf("cancel = %+v", c)
	}
	if _, err := Cancel(proj, string(started.TaskID)); err == nil {
		t.Fatal("double cancel must fail")
	}
	if _, err := Resume(proj, string(started.TaskID)); err == nil {
		t.Fatal("resume of a canceled task must fail")
	}
	if _, err := Status(proj, "T999"); err == nil {
		t.Fatal("unknown task must fail")
	}
	if _, err := Reply(proj, string(started.TaskID), "late"); err == nil {
		t.Fatal("reply to a terminal task must fail")
	}
}

// TestTaskShortCodes covers T-number addressing across tasks.
func TestTaskShortCodes(t *testing.T) {
	proj := testProject(t)
	for i := 0; i < 3; i++ {
		if _, err := Start(StartOptions{Request: "work", ProjectDir: proj, Inline: true}); err != nil {
			t.Fatal(err)
		}
	}
	items, err := ListTasks(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].Short != "T1" || items[2].Short != "T3" {
		t.Fatalf("shorts = %v", items)
	}
	if _, err := Status(proj, "T2"); err != nil {
		t.Fatalf("short lookup: %v", err)
	}
}

// TestTaskWorktree covers the Section 3.6 worktree rule: inside a git
// repository the task works on its own branch and the delegating
// tree stays clean.
func TestTaskWorktree(t *testing.T) {
	if _, err := lookPathGit(); err != nil {
		t.Skip("git not installed")
	}
	proj := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		if err := runGit(proj, args...); err != nil {
			t.Skipf("git unavailable: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(proj, "base.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runGit(proj, "add", "."); err != nil {
		t.Fatal(err)
	}
	if err := runGit(proj, "commit", "-m", "base"); err != nil {
		t.Fatal(err)
	}
	started, err := Start(StartOptions{Request: "work in tree", ProjectDir: proj, Inline: true})
	if err != nil {
		t.Fatal(err)
	}
	st, err := Status(proj, string(started.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch == "" {
		t.Fatal("worktree task must report its branch")
	}
	clean, err := gitClean(proj)
	if err != nil {
		t.Fatal(err)
	}
	if !clean {
		t.Fatal("the delegating tree must stay unchanged")
	}
}

// TestTaskKillAndAdopt covers the Section 3.7 row: killing the
// background process mid-run leaves the interruption visible in
// status, and the record files are adoptable (the daemon adopts the
// same directory with the 17.3 algorithm).
func TestTaskKillAndAdopt(t *testing.T) {
	if _, err := execLookPath("sleep"); err != nil {
		t.Skip("no sleep binary")
	}
	script := testSleepScript(t)
	proj := testProject(t)
	prev := InlineBackground
	InlineBackground = false
	defer func() { InlineBackground = prev }()
	started, err := Start(StartOptions{
		Request: "long work", ProjectDir: proj, Binary: script,
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := ResolveStore(proj)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := store.Load(string(started.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	if rec.PID <= 0 {
		t.Fatal("background child must record its pid")
	}
	// Kill the child like an operator would; the record keeps the
	// interruption for status and for daemon adoption.
	proc, err := osFindProcess(rec.PID)
	if err != nil {
		t.Fatal(err)
	}
	if err := procKill(proc); err != nil {
		t.Fatal(err)
	}
	st, err := Status(proj, string(started.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	if st.State != types.TaskInProgress {
		t.Fatalf("killed child still shows progress until control: %q", st.State)
	}
	canceled, err := Cancel(proj, string(started.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	if canceled.State != types.TaskCanceled {
		t.Fatalf("cancel = %q", canceled.State)
	}
	// Adoption: a fresh store over the same directory reads the full
	// record, events and artifacts without the original handle.
	adopted, err := ResolveStore(proj)
	if err != nil {
		t.Fatal(err)
	}
	arec, err := adopted.Load(string(started.TaskID))
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if arec.State != types.TaskCanceled {
		t.Fatalf("adopted state = %q", arec.State)
	}
	events, err := adopted.ReadEvents(started.TaskID)
	if err != nil || len(events) == 0 {
		t.Fatalf("adopted log = %v, %v", events, err)
	}
}

// TestRunTaskFile covers the background child entry point: the
// `__run-task` argv resolves the store three levels up from
// <base>/tasks/<id>/task.json and drives the record to terminal.
func TestRunTaskFile(t *testing.T) {
	proj := testProject(t)
	store, err := ResolveStore(proj)
	if err != nil {
		t.Fatal(err)
	}
	rec := &TaskRecord{
		TaskID: types.NewTaskID(), Short: "T1", Request: "file entry",
		Profile: ProfileDelegate, ProjectDir: proj,
		State: types.TaskOpen, RunID: types.NewRunID(),
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := store.Save(rec); err != nil {
		t.Fatal(err)
	}
	taskFile := store.TaskPath(rec.TaskID) + "/task.json"
	if err := RunTaskFile(taskFile); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Load(string(rec.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != types.TaskCompleted {
		t.Fatalf("child entry = %q", updated.State)
	}
	if err := RunTaskFile(taskFile + ".missing"); err == nil {
		t.Fatal("missing task file must fail")
	}
}
