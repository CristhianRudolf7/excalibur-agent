package tasks

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultLongHorizonConfig(t *testing.T) {
	cfg := DefaultLongHorizonConfig()
	if !cfg.Enable || !cfg.EnableContinuation || !cfg.EnableMilestones || !cfg.EnablePacing || !cfg.EnableBackgroundNudge {
		t.Fatalf("defaults must be on: %+v", cfg)
	}
	t.Setenv("EXCALIBUR_LONG_HORIZON", "off")
	if got := DefaultLongHorizonConfig(); got.Enable {
		t.Fatalf("master off must disable, got %+v", got)
	}
	t.Setenv("EXCALIBUR_LONG_HORIZON", "on")
	t.Setenv("EXCALIBUR_LONG_HORIZON_CONTINUATION", "0")
	if got := DefaultLongHorizonConfig(); !got.Enable || got.EnableContinuation {
		t.Fatalf("per-mechanism off must stand, got %+v", got)
	}
}

func TestEstimateLarge(t *testing.T) {
	cfg := DefaultLongHorizonConfig()
	if !EstimateLarge(cfg, strings.Repeat("x ", 300), 0) {
		t.Fatal("long request must estimate large")
	}
	if !EstimateLarge(cfg, "short", 3) {
		t.Fatal("enough drafts must estimate large")
	}
	if EstimateLarge(cfg, "short task", 0) {
		t.Fatal("short task must not estimate large")
	}
}

func TestBuildMilestones(t *testing.T) {
	drafts := []DraftCriterion{
		{ID: "D1", Statement: "file out.txt exists", Key: "out.txt"},
		{ID: "D2", Statement: "output is csv format", Key: "csv"},
	}
	plan := BuildMilestones("write out.txt in csv format", drafts)
	if len(plan) < 4 {
		t.Fatalf("plan must carry explore/impl/verify/report, got %d", len(plan))
	}
	if plan[0].Key != "explore" {
		t.Fatalf("first must be explore, got %q", plan[0].Key)
	}
	last := plan[len(plan)-1]
	if last.Key != "report" {
		t.Fatalf("last must be report, got %q", last.Key)
	}
	empty := BuildMilestones("do a thing that is long "+strings.Repeat("x ", 200), nil)
	if len(empty) != 4 {
		t.Fatalf("draft-free plan must be the generic 4 phases, got %d", len(empty))
	}
}

func exercisingHistory() []HistoryTool {
	return []HistoryTool{
		{Name: "exec", Args: `{"command":"ls"}`, Output: "session: sess-1 alive: false\nok"},
		{Name: "exec", Args: `{"command":"python3 prog.py"}`, Output: "result ok"},
	}
}

func TestContinuationGaps(t *testing.T) {
	if gaps := ContinuationGaps(nil, nil, 1); len(gaps) != 0 {
		t.Fatalf("trivial empty history must not block, got %v", gaps)
	}
	if gaps := ContinuationGaps(nil, []DraftCriterion{{ID: "D1", Statement: "x", Key: "x"}}, 1); len(gaps) == 0 {
		t.Fatal("empty history with drafts must report gaps")
	}
	drafts := []DraftCriterion{{ID: "D1", Statement: "file out.txt exists", Key: "out.txt", Method: "command", Check: "test -s out.txt"}}
	hist := []HistoryTool{
		{Name: "write_file", Args: `{"path":"out.txt"}`, Output: "wrote out.txt"},
	}
	if gaps := ContinuationGaps(hist, drafts, 1); len(gaps) == 0 {
		t.Fatal("write without exercising check must report gaps")
	}
	verified := []HistoryTool{
		{Name: "write_file", Args: `{"path":"out.txt"}`, Output: "wrote out.txt"},
		{Name: "exec", Args: `{"command":"python3 check.py out.txt"}`, Output: "out.txt ok"},
	}
	// A single exercising call after the change satisfies freshness for
	// the no-draft path; with a path draft the exercising call must
	// also name the artifact.
	if gaps := ContinuationGaps(verified, nil, 1); len(gaps) != 0 {
		t.Fatalf("exercised history must have no gaps, got %v", gaps)
	}
}

func TestSignificantBudgetLeft(t *testing.T) {
	cfg := DefaultLongHorizonConfig()
	if !SignificantBudgetLeft(cfg, 3600, 10*time.Minute, 2, 25) {
		t.Fatal("early in a capped run must count as significant")
	}
	if SignificantBudgetLeft(cfg, 3600, 59*time.Minute, 24, 25) {
		t.Fatal("last minute must not count as significant")
	}
	if !SignificantBudgetLeft(cfg, 0, 0, 2, 25) {
		t.Fatal("steps remaining must count when uncapped")
	}
	if SignificantBudgetLeft(cfg, 0, 0, 24, 25) {
		t.Fatal("one step left must not count as significant")
	}
}

func TestFormatPacing(t *testing.T) {
	line := FormatPacing(150*time.Second, 3600, 8, 25, 2, 5)
	for _, want := range []string{"pacing:", "elapsed", "steps 8/25", "time", "milestones 2/5"} {
		if !strings.Contains(line, want) {
			t.Fatalf("pacing must contain %q, got %q", want, line)
		}
	}
	bare := FormatPacing(5*time.Second, 0, 0, 0, 0, 0)
	if !strings.Contains(bare, "elapsed 5s") {
		t.Fatalf("uncapped pacing must still show elapsed, got %q", bare)
	}
}

func TestIsLongCommand(t *testing.T) {
	if !IsLongCommand("make -j4", `{"command":"make -j4"}`, false) {
		t.Fatal("make must count as long")
	}
	if IsLongCommand("make -j4", `{"command":"make -j4"}`, true) {
		t.Fatal("backgrounded commands must not nudge")
	}
	if IsLongCommand("echo hi", `{"command":"echo hi"}`, false) {
		t.Fatal("echo must not count as long")
	}
	if got := BackgroundHint("make -j4"); !strings.Contains(got, "background:true") || !strings.Contains(got, "exec_io") {
		t.Fatalf("hint must name background+poll, got %q", got)
	}
}

func TestUnprovenNote(t *testing.T) {
	note := UnprovenNote([]string{"gap one", "gap two"})
	if !strings.Contains(note, "long-horizon continuation") || !strings.Contains(note, "gap one") {
		t.Fatalf("note must name the gaps, got %q", note)
	}
}

func TestMilestoneProgress(t *testing.T) {
	plan := []LHMilestone{
		{ID: "M1", Title: "explore", Key: "explore"},
		{ID: "M2", Title: "verify", Key: "verify"},
		{ID: "M3", Title: "report", Key: "report"},
	}
	done, total := MilestoneProgress(plan, nil)
	if done != 0 || total != 2 {
		t.Fatalf("empty history: done/total = %d/%d, want 0/2", done, total)
	}
	done, total = MilestoneProgress(plan, exercisingHistory())
	if done != 2 || total != 2 {
		t.Fatalf("exercised history: done/total = %d/%d, want 2/2", done, total)
	}
	if next := NextPending(plan, nil); next == nil || next.ID != "M1" {
		t.Fatalf("first pending must be M1, got %+v", next)
	}
	if note := NextMilestoneNote(plan, nil); !strings.Contains(note, "M1") {
		t.Fatalf("nudge must name M1, got %q", note)
	}
}
