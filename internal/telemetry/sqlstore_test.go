// SQL telemetry persistence: events, per-task metric snapshots and
// exported spans over a real temporary telemetry database, plus
// crash/restart survival (Sections 22.1-22.3).
package telemetry

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func openTelemetryDB(t *testing.T, path string) *store.DB {
	t.Helper()
	ctx := context.Background()
	opts := store.TelemetryOptions()
	opts.BlobDir = filepath.Join(t.TempDir(), "blobs")
	db, err := store.Open(ctx, path, opts, migrations.Telemetry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSQLTelemetryStore(t *testing.T) {
	db := openTelemetryDB(t, filepath.Join(t.TempDir(), "tele.db"))
	s := OpenTelemetryStore(db)
	now := time.Now().UTC().Truncate(time.Millisecond)

	seq1, err := s.AppendEvent("run_1", "tsk_1", "run.progress", json.RawMessage(`{"p":50}`), now)
	if err != nil || seq1 == 0 {
		t.Fatalf("append: %d %v", seq1, err)
	}
	seq2, err := s.AppendEvent("run_1", "tsk_1", "tool.called", json.RawMessage(`{"tool":"exec"}`), now)
	if err != nil || seq2 != seq1+1 {
		t.Fatalf("append order: %d %d %v", seq1, seq2, err)
	}
	events, err := s.EventsSince(0, 0)
	if err != nil || len(events) != 2 || events[1].Seq != uint64(seq2) {
		t.Fatalf("events since: %+v %v", events, err)
	}
	if events[0].Type != "run.progress" || events[0].RunID == nil || string(*events[0].RunID) != "run_1" {
		t.Fatalf("event envelope: %+v", events[0])
	}
	events, err = s.EventsSince(uint64(seq1), 10)
	if err != nil || len(events) != 1 || events[0].ID != "evt_2" && events[0].Seq != uint64(seq2) {
		t.Fatalf("cursor resume: %+v %v", events, err)
	}
	byRun, err := s.EventsForRun("run_1")
	if err != nil || len(byRun) != 2 {
		t.Fatalf("events for run: %+v %v", byRun, err)
	}

	snap := Snapshot{TaskID: "tsk_1", RunID: "run_1", Model: "m", CostUSD: 0.42,
		FinalState: "completed", CallsByTool: map[string]int64{"exec": 3}}
	if err := s.SaveMetrics(snap); err != nil {
		t.Fatal(err)
	}
	back, ok := s.Metrics("tsk_1")
	if !ok || back.CostUSD != 0.42 || back.CallsByTool["exec"] != 3 || back.Model != "m" {
		t.Fatalf("metrics: %+v %v", back, ok)
	}
	if _, ok := s.Metrics("tsk_nope"); ok {
		t.Fatal("unexpected metrics for unknown task")
	}

	tr := NewTracer(NewTraceID(), types.RunID("run_1"))
	root := tr.StartAgent("m", "sys")
	if err := s.AppendSpan(root, "run_1"); err != nil {
		t.Fatal(err)
	}
	tool := tr.StartTool(root, "exec", "call_1")
	tool.EndedAt = time.Now().UTC()
	tool.Status = "ok"
	if err := s.AppendSpan(tool, "run_1"); err != nil {
		t.Fatal(err)
	}
	spans, err := s.SpansForTrace(tr.TraceID)
	if err != nil || len(spans) != 2 || spans[0].Name != SpanInvokeAgent {
		t.Fatalf("spans: %+v %v", spans, err)
	}
	if spans[1].ParentID != root.SpanID {
		t.Fatalf("span parent: %+v", spans[1])
	}
}

func TestSQLTelemetrySurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tele-crash.db")
	db := openTelemetryDB(t, path)
	s := OpenTelemetryStore(db)
	now := time.Now().UTC().Truncate(time.Millisecond)

	if _, err := s.AppendEvent("run_c", "tsk_c", "run.progress", json.RawMessage(`{"p":1}`), now); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMetrics(Snapshot{TaskID: "tsk_c", RunID: "run_c", CostUSD: 1.5}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2 := openTelemetryDB(t, path)
	s2 := OpenTelemetryStore(db2)
	events, err := s2.EventsForRun("run_c")
	if err != nil || len(events) != 1 {
		t.Fatalf("events lost: %+v %v", events, err)
	}
	m, ok := s2.Metrics("tsk_c")
	if !ok || m.CostUSD != 1.5 {
		t.Fatalf("metrics lost: %+v %v", m, ok)
	}
	// New rows keep the sequence moving forward after the crash.
	seq, err := s2.AppendEvent("run_c", "tsk_c", "tool.called", json.RawMessage(`{}`), now)
	if err != nil || seq != 2 {
		t.Fatalf("sequence after reopen: %d %v", seq, err)
	}
}
