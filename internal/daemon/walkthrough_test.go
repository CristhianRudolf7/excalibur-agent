package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	orchfake "github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Section 26.11 end to end: two project trees plus a general task,
// three orders from Telegram — a status question, an R4 correction
// with per-run priorities, and a pause of the thesis tree.

type walkTrees struct {
	orch     *orchestrator.Orchestrator
	masterAg types.Agent
	gastos   types.Agent
	lenaRun  types.RunID
	expRun   types.RunID
	thesis   types.Agent
	thesisCh []types.RunID
	lena     *orchfake.Runner
	exporter *orchfake.Runner
}

func mustAgentOf(t *testing.T, store *orchestrator.MemoryStore, run types.RunID) types.AgentID {
	t.Helper()
	rec, err := store.GetRun(run)
	if err != nil {
		t.Fatal(err)
	}
	return rec.Run.AgentID
}

func walkMaster(t *testing.T, wt *walkTrees, store *orchestrator.MemoryStore, classify func(text string, pre master.Preanalysis) master.Triage) *master.Master {
	t.Helper()
	lex := master.NewLexicon()
	lex.AddAgent("Lena", "Lena")
	lex.AddProject("gastos", "", "gastos")
	lex.AddProject("tesis", "thesis", "thesis")
	lex.AddProject("thesis", "", "thesis")
	ctrls := &OrchControl{Orch: wt.orch, Store: store}
	model := &master.FakeModel{Available_: true}
	m := master.New(master.Config{}, master.NewMemStore(), lex,
		&master.FakeDecider{Fn: classify}, ctrls, model, nil)
	return m
}

// Order 1: "What is Lena doing?" — intake persists and confirms,
// the lexicon finds the single active Lena, the decider classifies a
// status query with high confidence, and the fast lane answers from
// the 15.6 snapshot with no model call.
func TestWalkthroughStatusLena(t *testing.T) {
	store := orchestrator.NewMemoryStore()
	wt := seedWalkthroughOn(t, store)
	m := walkMaster(t, wt, store, func(text string, pre master.Preanalysis) master.Triage {
		return master.Triage{Type: master.TriageStatusQuery, Confidence: 0.95}
	})
	id, out, err := m.Receive(context.Background(), "tg", "m1", "telegram", "What is Lena doing?")
	if err != nil {
		t.Fatal(err)
	}
	if id == "" || !out.Handled || out.Snapshot == nil {
		t.Fatalf("status must fast-path: %+v", out)
	}
	if !strings.Contains(out.Reply, "cargo test") || !strings.Contains(out.Reply, "export_pdf") {
		t.Fatalf("reply must name the tool and live line: %q", out.Reply)
	}
	if out.Snapshot.PlanDone != 7 || out.Snapshot.PlanTotal != 9 {
		t.Fatalf("snapshot plan = %d/%d, want 7/9", out.Snapshot.PlanDone, out.Snapshot.PlanTotal)
	}
}

// Order 2: "Change R4: export must be CSV, not PDF." — correction
// with per-run priorities: the exporter editing src/export/pdf.rs
// (inside the impact area) is interrupt; Lena inside graceful
// `cargo test` is normal and her C4.1 evidence goes stale.
func TestWalkthroughCorrectionR4(t *testing.T) {
	store := orchestrator.NewMemoryStore()
	wt := seedWalkthroughOn(t, store)
	_ = wt
	// Priority unit checks first (Section 15.7 point 4).
	exp := orchestrator.PriorityInfo{
		ResourceKeys: []string{"src/export/pdf"},
		ImpactArea:   []string{"src/export"},
		Action:       "edit_file", Instruction: "export CSV",
	}
	if got := orchestrator.DecidePriority(exp, nil); got != orchestrator.PriorityInterrupt {
		t.Fatalf("exporter priority = %s, want interrupt", got)
	}
	lenaInfo := orchestrator.PriorityInfo{
		ResourceKeys: []string{"repo/gastos"},
		ImpactArea:   []string{"src/export"},
		Action:       "cargo test", Instruction: "export CSV",
	}
	if got := orchestrator.DecidePriority(lenaInfo, nil); got != orchestrator.PriorityNormal {
		t.Fatalf("lena priority = %s, want normal", got)
	}
	// Full propagation: the exporter run is redirected with
	// interrupt, Lena keeps running until her suite finishes.
	m := walkMaster(t, wt, store, func(text string, pre master.Preanalysis) master.Triage {
		return master.Triage{Type: master.TriageCorrection, Confidence: 0.9, CorrectionTarget: "gastos"}
	})
	id, out, err := m.Receive(context.Background(), "tg", "m2", "telegram", "Change R4: the export must be CSV, not PDF.")
	if err != nil {
		t.Fatal(err)
	}
	if id == "" || out.Handled {
		t.Fatalf("corrections need the master turn: %+v", out)
	}
	// The master turn runs the correction through the orchestrator.
	results, err := wt.orch.PropagateCorrection(orchestrator.Correction{
		From: wt.masterAg.ID, Text: "R4: export CSV",
		Summary: "CSV instead of PDF", Action: orchestrator.ActionReplan,
		ImpactArea: []string{"src/export"},
	}, types.GeneralScope(), "test")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[types.RunID]orchestrator.DirectivePriority{}
	for _, r := range results {
		seen[r.Run] = r.Priority
		if !r.Delivered {
			t.Fatalf("correction not delivered to %s: %s", r.Run, r.Note)
		}
	}
	// The exporter edits inside the impact area: interrupt. Lena's
	// graceful suite finishes first: normal. Thesis and conversion
	// trees are untouched by the R4 scope (general fan-out still
	// informs them at normal priority).
	if seen[wt.expRun] != orchestrator.PriorityInterrupt {
		t.Fatalf("exporter priority = %s, want interrupt", seen[wt.expRun])
	}
	if seen[wt.lenaRun] != orchestrator.PriorityNormal {
		t.Fatalf("lena priority = %s, want normal", seen[wt.lenaRun])
	}
	if full, _ := wt.exporter.Interrupts(); full != 1 {
		t.Fatalf("exporter interrupts = %d, want 1", full)
	}
	if full, _ := wt.lena.Interrupts(); full != 0 {
		t.Fatalf("lena must not be interrupted mid-suite, got %d", full)
	}
	// Normal directives wait in the mailbox for the next safe point
	// (Section 15.5); interrupt ones reach the engine at once.
	lenaAgent := mustAgentOf(t, store, wt.lenaRun)
	if len(wt.orch.PendingDirectives(lenaAgent)) != 1 {
		t.Fatalf("lena must hold one normal directive, got %d", len(wt.orch.PendingDirectives(lenaAgent)))
	}
	if len(wt.exporter.Delivered()) != 1 {
		t.Fatalf("exporter must receive the interrupt directive, got %d", len(wt.exporter.Delivered()))
	}
}

// Order 3: "Stop the thesis research." — "stop" a concrete tree means
// pause with immediate interrupt (Section 15.8): the parent and both
// children pause, contexts and leases survive, gastos keeps working.
func TestWalkthroughPauseThesis(t *testing.T) {
	store := orchestrator.NewMemoryStore()
	wt := seedWalkthroughOn(t, store)
	m := walkMaster(t, wt, store, func(text string, pre master.Preanalysis) master.Triage {
		return master.Triage{Type: master.TriageQuickAction, Confidence: 0.95}
	})
	_, out, err := m.Receive(context.Background(), "tg", "m3", "telegram", "pause thesis-coord")
	if err != nil {
		t.Fatal(err)
	}
	if !out.Handled {
		t.Fatalf("pause must fast-path: %+v", out)
	}
	for _, rid := range append([]types.RunID{}, wt.thesisCh...) {
		st, err := wt.orch.RunState(rid)
		if err != nil {
			t.Fatal(err)
		}
		if st != types.RunPaused {
			t.Fatalf("thesis child %s = %s, want paused", rid, st)
		}
	}
	for _, rid := range []types.RunID{wt.lenaRun, wt.expRun} {
		st, err := wt.orch.RunState(rid)
		if err != nil {
			t.Fatal(err)
		}
		if st == types.RunPaused {
			t.Fatalf("gastos run %s must keep working", rid)
		}
	}
}

// seedWalkthroughOn seeds onto a caller-owned store so OrchControl
// and the orchestrator share it.
func seedWalkthroughOn(t *testing.T, store *orchestrator.MemoryStore) *walkTrees {
	t.Helper()
	orch := orchestrator.New(orchestrator.DefaultConfig(), store, orchestrator.NoopJournal{})
	now := time.Now()
	mk := func(parent types.AgentID, name, desc string) (types.Agent, types.RunID) {
		card := orchestrator.DelegationCard{
			Name: name, Description: desc, Goal: "walkthrough work",
			Deliverable: "done", Scope: types.GeneralScope(),
			Done: []string{"done"}, Report: "summary",
		}
		child, rec, err := orch.Spawn(parent, card)
		if err != nil {
			t.Fatalf("spawn %s: %v", name, err)
		}
		return child, rec.Run.ID
	}
	mag, err := orch.RegisterMaster("master", types.GeneralScope())
	if err != nil {
		t.Fatal(err)
	}
	gastos, _ := mk(mag.ID, "gastos-coord", "coordinates gastos")
	lenaAg, lenaRun := mk(gastos.ID, "Lena", "runs the gastos tests")
	expAg, expRun := mk(gastos.ID, "exporter", "implements the exporter")
	_, _ = expAg, expRun
	_, _ = mk(gastos.ID, "migrator", "handles migrations")
	thesis, _ := mk(mag.ID, "thesis-coord", "researches the thesis")
	_, r1 := mk(thesis.ID, "scholar-a", "researches sources")
	_, r2 := mk(thesis.ID, "scholar-b", "researches archives")
	_, _ = mk(mag.ID, "converter", "converts documents")

	lena := orchfake.NewRunner(lenaRun, lenaAg.ID)
	lena.SetTool("cargo test", "--all", types.CancelGraceful, []string{"repo/gastos"}, 8*time.Minute, now.Add(-3*time.Minute))
	lena.SetLiveTail("test export_pdf ... ok")
	lena.SetProgress("export tests running", 7, 9, "M2")
	orch.AttachRunner(lena)

	exporter := orchfake.NewRunner(expRun, mustAgentOf(t, store, expRun))
	exporter.SetTool("edit_file", "src/export/pdf.rs", types.CancelImmediate, []string{"src/export/pdf"}, 0, now.Add(-time.Minute))
	orch.AttachRunner(exporter)

	for _, rid := range []types.RunID{r1, r2} {
		rr := orchfake.NewRunner(rid, mustAgentOf(t, store, rid))
		tool, cancel := "web_read", types.CancelImmediate
		if rid == r2 {
			tool, cancel = "browser_wait_ready", types.CancelCooperative
		}
		rr.SetTool(tool, "https://archive.example", cancel, []string{"web:archive"}, 0, now.Add(-time.Minute))
		orch.AttachRunner(rr)
	}
	_ = telemetry.NewBus(nil)
	return &walkTrees{orch: orch, masterAg: mag, gastos: gastos, lenaRun: lenaRun, expRun: expRun, thesis: thesis, thesisCh: []types.RunID{r1, r2}, lena: lena, exporter: exporter}
}
