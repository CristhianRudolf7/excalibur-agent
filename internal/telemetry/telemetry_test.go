package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestBusCursorNoGaps(t *testing.T) {
	b := NewBus(nil)
	mkRun := types.RunID("run_01testcursor0000000000")
	mkTask := types.TaskID("tsk_01testcursor0000000000")
	for i := 0; i < 5; i++ {
		if _, err := b.Publish(types.EventToolCalled, &mkRun, &mkTask, map[string]string{"i": "x"}); err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			if _, err := b.Publish(types.EventFileEdited, &mkRun, &mkTask,
				api.FileEditEvent{RunID: mkRun, Path: "a.go", Diff: "+x"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Client disconnects after seq 2, resumes from cursor.
	var cursor types.Cursor
	got := b.Replay(cursor)
	if len(got) != 6 {
		t.Fatalf("replay from zero: %d events", len(got))
	}
	cursor = types.Cursor{Seq: 2}
	rest := b.Replay(cursor)
	if len(rest) != 4 {
		t.Fatalf("resume from 2: %d events", len(rest))
	}
	for i, ev := range rest {
		if ev.Seq != uint64(3+i) {
			t.Fatalf("gap or reorder at %d: seq %d", i, ev.Seq)
		}
	}
	// Cursor roundtrip.
	s := cursor.String()
	back, err := types.ParseCursor(s)
	if err != nil || back.Seq != 2 {
		t.Fatalf("cursor roundtrip: %v %v", back, err)
	}
	// Typed payload with diff survives.
	var fe api.FileEditEvent
	for _, ev := range got {
		if ev.Type == types.EventFileEdited {
			if err := json.Unmarshal(ev.Payload, &fe); err != nil {
				t.Fatal(err)
			}
		}
	}
	if fe.Diff != "+x" {
		t.Fatalf("file edit diff lost: %+v", fe)
	}
}

func TestMetricsTokensCostCalls(t *testing.T) {
	prices := PriceTable{Version: "2026-10-28", Prices: map[string]ModelPrice{
		"m": {Input: 2.0, CachedRead: 0.2, Output: 8.0},
	}}
	m := NewTaskMetrics("tsk_01testmetrics000000000", "run_01testmetrics000000000", prices.Version)
	m.ObserveModel(types.RoleExecutor, "m",
		providers.Usage{Input: 1000, CacheRead: 400, CacheWrite: 100, Output: 200, Reasoning: 50}, prices, 2*time.Second)
	m.ObserveModel(types.RoleExecutor, "m",
		providers.Usage{Input: 500, CacheRead: 100, Output: 100}, prices, time.Second)
	m.ObserveTool("exec", time.Second, false)
	m.ObserveTool("exec", time.Second, true)
	m.ObserveWait(30 * time.Second)
	m.ObserveResources(1<<20, 2<<20, 512<<20, 1, 7, 3.5)
	m.ObserveEfficiency(true, true, true, true, true, 2)
	m.Finish("completed")
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	snap := m.Snapshot()
	if snap.Usage.Input != 1500 || snap.Usage.Output != 300 {
		t.Fatalf("usage not accumulated: input=%d output=%d", snap.Usage.Input, snap.Usage.Output)
	}
	// Expected cost: uncached 900*2 + cached 600*0.2 + out 350*8 per 1M.
	want := 900.0 / 1e6 * 2.0
	extra := 600.0 / 1e6 * 0.2
	wantExtra := 350.0 / 1e6 * 8.0
	if snap.CostUSD != want+extra+wantExtra {
		t.Fatalf("cost %.8f want %.8f", snap.CostUSD, want+extra+wantExtra)
	}
	if snap.CallsByRole["executor"] != 2 || snap.CallsByTool["exec"] != 2 {
		t.Fatalf("call counts: roles=%v tools=%v", snap.CallsByRole, snap.CallsByTool)
	}
	if snap.ToolRetries != 1 || snap.ModelRetries != 1 || snap.Compactions != 1 || snap.MaskBatches != 1 || snap.LoopsCaught != 1 {
		t.Fatalf("efficiency counters: retries=%d/%d comp=%d mask=%d loops=%d",
			snap.ToolRetries, snap.ModelRetries, snap.Compactions, snap.MaskBatches, snap.LoopsCaught)
	}
	if snap.ChildPeakBytes != 512<<20 || snap.ChildOOMKills != 1 || snap.ChildPeakPIDs != 7 {
		t.Fatalf("resources: peak=%d oom=%d pids=%d", snap.ChildPeakBytes, snap.ChildOOMKills, snap.ChildPeakPIDs)
	}
	if got := snap.CacheHitRate(); got != 600.0/1500.0 {
		t.Fatalf("hit rate %v", got)
	}
	// Cached subset violation is rejected.
	bad := NewTaskMetrics("tsk_01bad00000000000000000", "run_01bad00000000000000000", "v")
	bad.Usage = providers.Usage{Input: 10, CacheRead: 11}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected cached-exceeds-input error")
	}
}

func TestATIFIncrementalAndValidate(t *testing.T) {
	dir := t.TempDir()
	meta := ATIFMetadata{TaskID: "t1", ExcaliburCommit: "abc", Profile: "eval@1", ProfileHash: "h", Model: "m", Provider: "p"}
	w, err := NewWriter(dir, ATIFv18, meta)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := w.Append(ATIFStep{Role: "assistant", Content: "step"}); err != nil {
			t.Fatal(err)
		}
		// Partial file must stay valid after every write.
		if err := ValidateFile(w.Path()); err != nil {
			t.Fatalf("partial ATIF invalid after step %d: %v", i, err)
		}
	}
	if w.Version() != ATIFv18 {
		t.Fatal("version not kept")
	}
	bad, err := NewWriter(filepath.Join(dir, "bad"), "v9.9", meta)
	if err == nil {
		_ = bad
		t.Fatal("expected version rejection")
	}
	// v1.8 requires provider + profile hash.
	traj := Trajectory{Version: ATIFv18, Metadata: ATIFMetadata{TaskID: "t", ExcaliburCommit: "c"}}
	if err := Validate(&traj); err == nil {
		t.Fatal("expected v1.8 metadata rejection")
	}
	// Bad seq rejected.
	traj2 := Trajectory{Version: ATIFv17, Metadata: ATIFMetadata{TaskID: "t", ExcaliburCommit: "c"},
		Steps: []ATIFStep{{Seq: 2, Role: "assistant"}}}
	if err := Validate(&traj2); err == nil {
		t.Fatal("expected seq rejection")
	}
}

func TestSpansValidate(t *testing.T) {
	tr := NewTracer(NewTraceID(), "run_01spans000000000000000")
	if err := ValidateIDShapes(tr.TraceID, NewSpanID()); err != nil {
		t.Fatal(err)
	}
	root := tr.StartAgent("m", "sys")
	if err := ValidateSpan(root); err != nil {
		t.Fatal(err)
	}
	chat := tr.StartModel(root, "m", "executor", 100)
	if err := ValidateSpan(chat); err != nil {
		t.Fatal(err)
	}
	tool := tr.StartTool(root, "exec", "call_1")
	if err := ValidateSpan(tool); err != nil {
		t.Fatal(err)
	}
	Finish(&tool, "ok", map[string]any{"gen_ai.tool.result": "done"})
	if tool.Status != "ok" || tool.EndedAt.IsZero() {
		t.Fatal("finish did not close span")
	}
	broken := tool
	broken.Attributes = map[string]any{}
	if err := ValidateSpan(broken); err == nil {
		t.Fatal("expected missing-attribute rejection")
	}
}

func TestProvenanceChain(t *testing.T) {
	p := Provenance{ArtifactKind: "edit", ArtifactID: "e1", RunID: "run_01prov000000000000000",
		Step: 3, ToolCallID: "call_1", RequestID: "req_1", InstructionRev: 4, EvidenceID: "evd_1"}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	s := p.Chain()
	for _, want := range []string{"run_01prov", "req_1", "rev 4", "call_1"} {
		if !contains(s, want) {
			t.Fatalf("chain missing %q: %s", want, s)
		}
	}
	bad := Provenance{}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected provenance rejection")
	}
}

func TestStatusRenders(t *testing.T) {
	s := api.RunStatusResult{RunID: "run_1", State: types.RunRunning,
		CurrentStep: "run tests", ToolInFlight: "exec", LiveOutput: "ok", PlanDone: 7, PlanTotal: 9, BudgetUsedPct: 0.5}
	out := RenderStatus(s)
	for _, want := range []string{"run_1", "running", "run tests", "exec", "50%"} {
		if !contains(out, want) {
			t.Fatalf("status missing %q: %s", want, out)
		}
	}
	m := NewTaskMetrics("tsk_01status000000000000000", "run_01status00000000000000", "v")
	m.ObserveModel(types.RoleExecutor, "unknown-model", providers.Usage{Input: 100, Output: 10}, PriceTable{}, time.Second)
	line := RenderMetricsLine(m)
	if !contains(line, "cost") || !contains(line, "100 input") {
		t.Fatalf("metrics line: %s", line)
	}
}

func TestWatchdogs(t *testing.T) {
	th := DefaultThresholds()
	now := time.Now().UTC()
	if a := CheckStuck(th, "r", now.Add(-time.Hour), now); a == nil || a.Severity != "urgent" {
		t.Fatal("stuck should fire")
	}
	if a := CheckStuck(th, "r", now.Add(-time.Minute), now); a != nil {
		t.Fatal("fresh run should not fire")
	}
	if a := CheckWAL(th, th.WALAlertBytes+1, now); a == nil {
		t.Fatal("WAL should fire")
	}
	if a := CheckRAM(th, th.RAMHighBytes+1, now); a == nil {
		t.Fatal("RAM should fire")
	}
	if a := CheckDisk(th, th.DiskFreeLowBytes-1, now); a == nil {
		t.Fatal("disk should fire")
	}
	if a := CheckProvider(th, 5, 0, "openai", now); a == nil {
		t.Fatal("provider failures should fire")
	}
	if a := CheckProvider(th, 0, 0.9, "openai", now); a == nil {
		t.Fatal("provider rate should fire")
	}
	if a := CheckCacheDrift(th, 0.8, 0.5, now); a == nil {
		t.Fatal("cache drift should fire")
	}
	if a := CheckCacheDrift(th, 0.8, 0.75, now); a != nil {
		t.Fatal("small drift should not fire")
	}
	if Summarize(nil) == "" {
		t.Fatal("empty summary")
	}
}

func TestSinkIncremental(t *testing.T) {
	dir := t.TempDir()
	sink, err := OpenSink(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sink.Close() }()
	b := NewBus(sink)
	if _, err := b.Publish(types.EventToolCalled, nil, nil, map[string]string{"a": "b"}); err != nil {
		t.Fatal(err)
	}
	if err := sink.AppendMetrics(map[string]int{"n": 1}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"events.jsonl", "metrics.jsonl"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || len(raw) == 0 {
			t.Fatalf("sink %s missing: %v", name, err)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
