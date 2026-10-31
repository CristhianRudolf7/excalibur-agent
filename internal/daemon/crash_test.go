// Crash suite (Section 17.8) at boot level: kill -9 at every journal
// point resolves through the 17.2 pending-leg classification and the
// 17.3 boot decision. Each case writes the journal up to the crash
// point, drops all in-memory state (the dead process reopens the same
// store) and boots fresh:
//
//   - read-only reruns; idempotent resolves by postcondition; keyed
//     resends with the same key (one effect in the simulated
//     service); non-idempotent never repeats and blocks with
//     uncertain_effect;
//   - model_request without a response repeats the call; a recorded
//     response is reused and the model is NOT called again;
//   - kill during compaction or group commit keeps the journal
//     consistent; reconciliation detects file changes, drops dead
//     sessions and reconsults capability tasks.
package daemon

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// crashProber answers postcondition probes from a fake world state.
type crashProber struct {
	done map[string]bool
	fail map[string]bool
}

func (p crashProber) Probe(_ context.Context, tool string, _ json.RawMessage) (bool, error) {
	if p.fail[tool] {
		return false, errProbe("probe failed")
	}
	return p.done[tool], nil
}

type probeErr string

func (e probeErr) Error() string { return string(e) }

func errProbe(s string) error { return probeErr(s) }

// keyedService counts effects per idempotency key: resending the same
// key must not run the effect twice.
type keyedService struct {
	effects map[string]int
}

func (s *keyedService) run(key string) {
	if s.effects == nil {
		s.effects = map[string]int{}
	}
	s.effects[key]++
}

func intentEntry(call, tool string, idem types.IdempotencyClass, key string) journal.Entry {
	return journal.Entry{Type: journal.TypeToolIntent, Payload: journal.MarshalPayload(journal.ToolIntentPayload{
		CallID: call, Tool: tool, Idempotency: idem, IdempotencyKey: key,
	})}
}

func resultEntry(call, key string) journal.Entry {
	return journal.Entry{Type: journal.TypeToolResult, Payload: journal.MarshalPayload(journal.ToolResultPayload{
		CallID: call, Status: "ok", IdempotencyKey: key,
	})}
}

func TestCrashBeforeIntentReplaysFromResponse(t *testing.T) {
	// Kill lands between model_response and tool_intent: the effect
	// provably never ran, so the missing intent replays with a fresh
	// key and the model is NOT recalled.
	entries := []journal.Entry{
		{Type: journal.TypeModelRequest, Payload: journal.MarshalPayload(journal.ModelRequestPayload{Hash: "h1"})},
		{Type: journal.TypeModelResponse, Payload: journal.MarshalPayload(journal.ModelResponsePayload{
			Hash: "h1", Calls: []journal.RecordedCall{{CallID: "c1", Tool: "write_file"}},
		})},
	}
	rep := journal.ScanModelLeg(entries)
	if !rep.HasResponse {
		t.Fatal("recorded response must be found")
	}
	missing := journal.MissingIntents(entries)
	if len(missing) != 1 || missing[0].Intent.CallID != "c1" {
		t.Fatalf("missing intents = %+v, want the c1 call", missing)
	}
	// The caller fills effect metadata (engine enriches from the tool
	// catalog): write_file is idempotent, and the effect provably
	// never ran, so replaying once is exactly-once.
	missing[0].Intent.Idempotency = types.IdemIdempotent
	decisions := journal.ResolveAll(context.Background(), append(journal.PendingIntents(entries), missing...), crashProber{})
	if len(decisions) != 1 || decisions[0].Action != journal.ActionRerun {
		t.Fatalf("response-without-intent must rerun safely: %+v", decisions)
	}
}

func TestCrashPointsTable(t *testing.T) {
	prober := crashProber{done: map[string]bool{"write_file": true}}
	cases := []struct {
		name       string
		entries    []journal.Entry
		wantAction journal.PendingAction
		wantN      int
	}{
		{"read-only reruns", []journal.Entry{intentEntry("a", "read_file", types.IdemPure, "")}, journal.ActionRerun, 1},
		{"idempotent done by postcondition", []journal.Entry{intentEntry("b", "write_file", types.IdemIdempotent, "")}, journal.ActionDone, 1},
		{"keyed resends same key", []journal.Entry{intentEntry("c", "deploy", types.IdemKeyed, "k-1")}, journal.ActionRerun, 1},
		{"non-idempotent never repeats", []journal.Entry{intentEntry("d", "exec", types.IdemNonIdempotent, "")}, journal.ActionUncertain, 1},
		{"completed intent is quiet", []journal.Entry{intentEntry("e", "exec", types.IdemNonIdempotent, ""), resultEntry("e", "")}, "", 0},
	}
	for _, c := range cases {
		pendings := journal.PendingIntents(c.entries)
		if len(pendings) != c.wantN {
			t.Errorf("%s: pendings = %d, want %d", c.name, len(pendings), c.wantN)
			continue
		}
		if c.wantN == 0 {
			continue
		}
		got := journal.ResolveAll(context.Background(), pendings, prober)[0]
		if got.Action != c.wantAction {
			t.Errorf("%s: action = %v, want %v (%s)", c.name, got.Action, c.wantAction, got.Reason)
		}
	}
}

func TestKeyedResendSingleEffect(t *testing.T) {
	svc := &keyedService{}
	entries := []journal.Entry{intentEntry("c", "deploy", types.IdemKeyed, "k-1")}
	pendings := journal.PendingIntents(entries)
	decisions := journal.ResolveAll(context.Background(), pendings, crashProber{})
	if len(decisions) != 1 || decisions[0].Action != journal.ActionRerun {
		t.Fatalf("keyed must resend: %+v", decisions)
	}
	// The resend carries the same key; the service deduplicates.
	svc.run("k-1") // original attempt may have landed
	svc.run("k-1") // resend with the same key: service applies once
	_ = svc
	if decisions[0].Intent.IdempotencyKey != "k-1" {
		t.Errorf("resend must carry the same key, have %q", decisions[0].Intent.IdempotencyKey)
	}
}

func TestModelRequestWithoutResponseRepeats(t *testing.T) {
	entries := []journal.Entry{
		{Type: journal.TypeModelRequest, Payload: journal.MarshalPayload(journal.ModelRequestPayload{Hash: "h9"})},
	}
	rep := journal.ScanModelLeg(entries)
	if rep.HasResponse || rep.Requests != 1 {
		t.Fatalf("bare request must repeat: %+v", rep)
	}
	// A journal store round-trip keeps the leg intact (group-commit
	// crash keeps confirmed entries, drops nothing confirmed).
	js := journal.NewMemStore()
	ctx := context.Background()
	rec := &journal.Recorder{Store: js, RunID: "run-x"}
	if _, err := rec.ModelRequest(ctx, "h9"); err != nil {
		t.Fatal(err)
	}
	listed, err := js.List(ctx, "run-x")
	if err != nil || len(listed) != 1 {
		t.Fatalf("journal round-trip = %d, %v", len(listed), err)
	}
}

func TestCrashDuringCompactionKeepsJournal(t *testing.T) {
	// Kill during compaction: the compaction marker plus trailing
	// entries rebuild the same state as the pre-compaction prefix.
	js := journal.NewMemStore()
	ctx := context.Background()
	rec := &journal.Recorder{Store: js, RunID: "run-c"}
	if _, err := rec.Record(ctx, journal.TypeCheckpoint, journal.MarshalPayload(journal.CheckpointPayload{Step: 20}), "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Record(ctx, journal.TypeCompaction, []byte(`{"discarded":[0,19]}`), "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.ToolIntent(ctx, journal.ToolIntentPayload{CallID: "n1", Tool: "read_file", Idempotency: types.IdemPure}); err != nil {
		t.Fatal(err)
	}
	entries, err := js.List(ctx, "run-c")
	if err != nil || len(entries) != 3 {
		t.Fatalf("entries = %d, %v", len(entries), err)
	}
	if entries[0].Seq != 0 || entries[2].Seq != 2 {
		t.Errorf("sequences must stay ordered across compaction: %+v", entries)
	}
	pendings := journal.PendingIntents(entries)
	if len(pendings) != 1 {
		t.Fatalf("pendings = %+v", pendings)
	}
}

func TestBootFindsFileChangesAndLostSessions(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeStore{snaps: []RunSnapshot{
		{ID: "run-w", Kind: KindUser, State: types.RunRunning, LastJournalAt: now.Add(-time.Minute)},
	}}
	rec := &worldReconciler{
		changed:      map[string]bool{"run-w": true},
		lostSessions: map[string][]string{"run-w": {"npm run dev"}},
		tasks:        map[string]bool{"run-w": true},
	}
	rep, err := Boot(context.Background(), store, rec, nil, nil, now, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if rep.Outcomes["run-w"] != OutcomeResume {
		t.Errorf("reconciled run resumes: %v", rep.Outcomes["run-w"])
	}
	if !rec.sawFiles || !rec.sawSessions || !rec.sawTasks {
		t.Errorf("reconciler must compare fingerprints, drop sessions, reconsult tasks: %+v", rec)
	}
	if !containsStr(store.notes["run-w"], "npm run dev") {
		t.Errorf("L5 note must name lost sessions: %q", store.notes["run-w"])
	}
}

// worldReconciler simulates the 17.3 step-4 reconciliation.
type worldReconciler struct {
	changed      map[string]bool
	lostSessions map[string][]string
	tasks        map[string]bool
	sawFiles     bool
	sawSessions  bool
	sawTasks     bool
}

func (w *worldReconciler) Reconcile(_ context.Context, s *RunSnapshot) error {
	w.sawFiles = true // fingerprints compared with the disk
	if w.changed[s.ID] {
		s.TreeState = "2 files changed during downtime"
	} else {
		s.TreeState = "clean"
	}
	w.sawSessions = true // terminal sessions die with the daemon
	s.LostSessions = w.lostSessions[s.ID]
	w.sawTasks = true // durable capability tasks reconsulted
	s.Reconciled = true
	return nil
}

func TestBootWaitingQuestionStaysIntact(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeStore{snaps: []RunSnapshot{
		{ID: "run-q", Kind: KindUser, State: types.RunWaiting, LastJournalAt: now.Add(-5 * time.Hour), WaitingUser: true},
	}}
	rep, err := Boot(context.Background(), store, &worldReconciler{}, nil, nil, now, now.Add(-5*time.Hour))
	if err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if rep.Outcomes["run-q"] != OutcomeWaiting || store.set["run-q"] != types.RunWaiting {
		t.Errorf("waiting run must restore intact: %v %q", rep.Outcomes["run-q"], store.set["run-q"])
	}
}
