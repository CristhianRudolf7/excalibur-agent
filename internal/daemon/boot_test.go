// Boot tests: 17.3 decisions, safe mode, quarantine, adoption.
package daemon

import (
	"context"
	"errors"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestDecideTable(t *testing.T) {
	cases := []struct {
		name string
		in   DecideInput
		want Outcome
	}{
		{"short clean interruption resumes", DecideInput{Kind: KindUser, WasRunning: true}, OutcomeResume},
		{"long interruption parks", DecideInput{Kind: KindUser, WasRunning: true, InterruptedBy: 7 * time.Hour}, OutcomePaused},
		{"exactly 6h resumes", DecideInput{Kind: KindUser, InterruptedBy: 6 * time.Hour}, OutcomeResume},
		{"uncertain effect blocks", DecideInput{Kind: KindUser, Uncertain: true}, OutcomeBlocked},
		{"uncertain beats long interruption", DecideInput{Kind: KindUser, Uncertain: true, InterruptedBy: 24 * time.Hour}, OutcomeBlocked},
		{"waiting user restores", DecideInput{Kind: KindUser, WaitingUser: true}, OutcomeWaiting},
		{"waiting beats uncertainty", DecideInput{Kind: KindUser, WaitingUser: true, Uncertain: true}, OutcomeWaiting},
		{"budget gone parks", DecideInput{Kind: KindUser, BudgetGone: true}, OutcomePaused},
		{"safe mode parks everything", DecideInput{Kind: KindMasterTurn, SafeMode: true}, OutcomeSafeMode},
		{"quarantine beats uncertainty", DecideInput{Kind: KindUser, Quarantined: true, Uncertain: true}, OutcomeQuarantined},
		{"safe mode beats quarantine", DecideInput{Kind: KindUser, SafeMode: true, Quarantined: true}, OutcomeSafeMode},
	}
	for _, c := range cases {
		if got := Decide(c.in); got != c.want {
			t.Errorf("%s: Decide = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPauseBlockReasonMapping(t *testing.T) {
	if PauseReasonFor(OutcomeSafeMode) != types.PauseSafeMode {
		t.Error("safe mode must store paused(safe_mode)")
	}
	if PauseReasonFor(OutcomePaused) != types.PauseRecovery {
		t.Error("recovery pause must store paused(recovery)")
	}
	if BlockReasonFor(OutcomeBlocked) != types.BlockUncertainEffect {
		t.Errorf("uncertain must store blocked(%q)", types.BlockUncertainEffect)
	}
	if BlockReasonFor(OutcomeQuarantined) == types.BlockUncertainEffect {
		t.Error("quarantine must use a distinct block reason (possible crash cause)")
	}
}

func TestRecoveryNoteNamesSessionsAndTree(t *testing.T) {
	note := RecoveryNote([]string{"npm run dev", "tail -f log"}, "tree clean at step 41")
	for _, want := range []string{"npm run dev", "tail -f log", "step 41"} {
		if !containsStr(note, want) {
			t.Errorf("note %q lacks %q", note, want)
		}
	}
	if got := RecoveryNote(nil, "clean"); !containsStr(got, "none") {
		t.Errorf("empty sessions must read none, got %q", got)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// fakeStore is a minimal RunStore.
type fakeStore struct {
	snaps  []RunSnapshot
	set    map[string]types.RunState
	pauses map[string]*types.PauseReason
	blocks map[string]*types.BlockReason
	notes  map[string]string
	err    error
}

func (f *fakeStore) Unfinished() ([]RunSnapshot, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]RunSnapshot(nil), f.snaps...), nil
}

func (f *fakeStore) SetState(id string, st types.RunState, pause *types.PauseReason, block *types.BlockReason, note string) error {
	if f.set == nil {
		f.set = map[string]types.RunState{}
		f.pauses = map[string]*types.PauseReason{}
		f.blocks = map[string]*types.BlockReason{}
		f.notes = map[string]string{}
	}
	f.set[id] = st
	f.pauses[id] = pause
	f.blocks[id] = block
	f.notes[id] = note
	return nil
}

type fakeReconciler struct {
	losers map[string][]string
}

func (f *fakeReconciler) Reconcile(_ context.Context, s *RunSnapshot) error {
	s.Reconciled = true
	if ls, ok := f.losers[s.ID]; ok {
		s.LostSessions = ls
	}
	s.TreeState = "clean"
	return nil
}

type fakeAgenda struct {
	fired, deferred int
	deferLaunch     bool
	err             error
}

func (f *fakeAgenda) CatchUp(_, _ time.Time, deferLaunch bool) (int, int, error) {
	f.deferLaunch = deferLaunch
	if f.err != nil {
		return 0, 0, f.err
	}
	return f.fired, f.deferred, nil
}

func TestBootOrdersAndOutcomes(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeStore{snaps: []RunSnapshot{
		{ID: "bg", Kind: KindBackground, State: types.RunRunning, LastJournalAt: now.Add(-time.Hour)},
		{ID: "master", Kind: KindMasterTurn, State: types.RunRunning, LastJournalAt: now.Add(-time.Minute)},
		{ID: "user", Kind: KindUser, State: types.RunRunning, LastJournalAt: now.Add(-2 * time.Hour), Uncertain: true},
		{ID: "sched", Kind: KindScheduled, State: types.RunWaiting, LastJournalAt: now.Add(-time.Hour), WaitingUser: true},
	}}
	rec := &fakeReconciler{losers: map[string][]string{"master": {"dev server"}}}
	ag := &fakeAgenda{fired: 2, deferred: 1}
	rep, err := Boot(context.Background(), store, rec, ag, nil, now, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Boot: %v", err)
	}
	wantOrder := []string{"master", "user", "sched", "bg"}
	if len(rep.Order) != len(wantOrder) {
		t.Fatalf("order = %v, want %v", rep.Order, wantOrder)
	}
	for i, id := range wantOrder {
		if rep.Order[i] != id {
			t.Fatalf("order = %v, want %v", rep.Order, wantOrder)
		}
	}
	if rep.Outcomes["master"] != OutcomeResume || rep.Outcomes["bg"] != OutcomeResume {
		t.Errorf("clean short runs must resume: %v", rep.Outcomes)
	}
	if rep.Outcomes["user"] != OutcomeBlocked {
		t.Errorf("uncertain run must block: %v", rep.Outcomes)
	}
	if rep.Outcomes["sched"] != OutcomeWaiting {
		t.Errorf("waiting run must restore: %v", rep.Outcomes)
	}
	if store.set["master"] != types.RunQueued {
		t.Errorf("resumed run must requeue, have %q", store.set["master"])
	}
	if store.set["user"] != types.RunBlocked {
		t.Errorf("uncertain run must block, have %q", store.set["user"])
	}
	if store.blocks["user"] == nil || *store.blocks["user"] != types.BlockUncertainEffect {
		t.Errorf("uncertain run needs the uncertain_effect reason: %+v", store.blocks["user"])
	}
	if store.set["sched"] != types.RunWaiting {
		t.Errorf("waiting run must restore, have %q", store.set["sched"])
	}
	if ag.deferLaunch {
		t.Error("agenda must launch when not in safe mode")
	}
	if rep.Fired != 2 || rep.Deferred != 1 {
		t.Errorf("agenda tally = %d/%d, want 2/1", rep.Fired, rep.Deferred)
	}
	if rep.Notice == "" || !containsStr(rep.Notice, "1 blocked") {
		t.Errorf("single summary notice required, got %q", rep.Notice)
	}
}

func TestBootSafeModeParksAll(t *testing.T) {
	dir := t.TempDir()
	log := NewCrashLog(dir)
	now := time.Now().UTC()
	if _, err := log.RecordCrash(now.Add(-9 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := log.RecordCrash(now.Add(-4 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	safe, err := log.RecordCrash(now)
	if err != nil || !safe {
		t.Fatalf("third crash in 10m must trip safe mode: %v %v", safe, err)
	}
	store := &fakeStore{snaps: []RunSnapshot{
		{ID: "a", Kind: KindMasterTurn, State: types.RunRunning, LastJournalAt: now.Add(-time.Minute)},
		{ID: "b", Kind: KindUser, State: types.RunWaiting, LastJournalAt: now.Add(-time.Minute), WaitingUser: true},
	}}
	ag := &fakeAgenda{}
	rep, err := Boot(context.Background(), store, &fakeReconciler{}, ag, log, now, now)
	if err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if !rep.SafeMode {
		t.Error("report must flag safe mode")
	}
	for _, id := range []string{"a", "b"} {
		if rep.Outcomes[id] != OutcomeSafeMode {
			t.Errorf("%s outcome = %v, want safe_mode", id, rep.Outcomes[id])
		}
		if store.set[id] != types.RunPaused || store.pauses[id] == nil || *store.pauses[id] != types.PauseSafeMode {
			t.Errorf("%s must park paused(safe_mode): %+v", id, store.set[id])
		}
	}
	if !ag.deferLaunch {
		t.Error("agenda must record without launching in safe mode")
	}
}

func TestSafeModeCooldownAndOrder(t *testing.T) {
	dir := t.TempDir()
	log := NewCrashLog(dir)
	now := time.Now().UTC()
	for _, at := range []time.Time{now.Add(-9 * time.Minute), now.Add(-4 * time.Minute), now} {
		if _, err := log.RecordCrash(at); err != nil {
			t.Fatal(err)
		}
	}
	if !log.InSafeMode() {
		t.Fatal("three crashes in 10 minutes must latch safe mode")
	}
	active, err := log.MaybeExitSafeMode(now.Add(25 * time.Minute))
	if err != nil || !active {
		t.Fatalf("last crash 25m ago: still active = %v, %v", active, err)
	}
	active, err = log.MaybeExitSafeMode(now.Add(31 * time.Minute))
	if err != nil || active {
		t.Fatalf("30 clean minutes must clear safe mode: %v %v", active, err)
	}
	if log.InSafeMode() {
		t.Error("flag must clear after cooldown")
	}
	// Explicit order clears immediately.
	if err := log.ClearSafeMode(); err != nil || log.InSafeMode() {
		t.Errorf("user order must clear safe mode: %v", err)
	}
}

func TestQuarantineSameRunSameStep(t *testing.T) {
	dir := t.TempDir()
	log := NewCrashLog(dir)
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		q, err := log.RecordStepCrash("run-9", 41, now.Add(time.Duration(i)*time.Minute))
		if err != nil || q {
			t.Fatalf("attempt %d: quarantined = %v, %v (want false until third)", i, q, err)
		}
	}
	q, err := log.RecordStepCrash("run-9", 41, now.Add(3*time.Minute))
	if err != nil || !q {
		t.Fatalf("third same-run same-step crash must quarantine: %v %v", q, err)
	}
	if !log.IsQuarantined("run-9") {
		t.Error("run-9 must be quarantined")
	}
	// Different step does not count.
	if q, _ := log.RecordStepCrash("run-9", 42, now.Add(4*time.Minute)); q {
		t.Error("different step must not quarantine")
	}
	store := &fakeStore{snaps: []RunSnapshot{
		{ID: "run-9", Kind: KindUser, State: types.RunRunning, LastJournalAt: now},
	}}
	rep, err := Boot(context.Background(), store, &fakeReconciler{}, &fakeAgenda{}, log, now.Add(5*time.Minute), now)
	if err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if rep.Outcomes["run-9"] != OutcomeQuarantined {
		t.Errorf("outcome = %v, want quarantined", rep.Outcomes["run-9"])
	}
	if err := log.ReleaseQuarantine("run-9"); err != nil || log.IsQuarantined("run-9") {
		t.Errorf("explicit resume order must release quarantine: %v", err)
	}
}

func TestAdoptAllKeepsGoing(t *testing.T) {
	tasks := []TaskFile{{DBPath: "a.db"}, {DBPath: "b.db"}, {DBPath: "c.db"}}
	imp := &fakeImporter{runs: map[string]int{"a.db": 2, "c.db": 1}, fail: map[string]bool{"b.db": true}}
	rep := AdoptAll(tasks, imp)
	if len(rep.Adopted) != 2 || len(rep.Failed) != 1 {
		t.Errorf("adopt = %+v, want 2 adopted 1 failed", rep)
	}
	if _, ok := rep.Failed["b.db"]; !ok {
		t.Errorf("failing file must be reported, not fatal: %+v", rep)
	}
	empty := AdoptAll(nil, imp)
	if len(empty.Adopted) != 0 {
		t.Errorf("no tasks must adopt nothing: %+v", empty)
	}
	skipped := AdoptAll(tasks, nil)
	if len(skipped.Skipped) != 3 {
		t.Errorf("nil importer must skip all: %+v", skipped)
	}
}

type fakeImporter struct {
	runs map[string]int
	fail map[string]bool
}

func (f *fakeImporter) Import(t TaskFile) (int, error) {
	if f.fail[t.DBPath] {
		return 0, errors.New("corrupt task database")
	}
	return f.runs[t.DBPath], nil
}

func TestScanTaskFiles(t *testing.T) {
	root := t.TempDir()
	proj := root + "/proj"
	if err := mkTaskDB(proj, "t1.db"); err != nil {
		t.Fatal(err)
	}
	if err := mkTaskDB(proj, "notes.txt"); err != nil {
		t.Fatal(err)
	}
	found, err := ScanTaskFiles([]string{proj, root + "/missing"})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(found) != 1 || !containsStr(found[0].DBPath, "t1.db") {
		t.Errorf("scan = %+v, want only t1.db", found)
	}
	if found[0].ProjectDir != proj {
		t.Errorf("project dir = %q, want %q", found[0].ProjectDir, proj)
	}
}

func mkTaskDB(proj, name string) error {
	if err := os.MkdirAll(proj+"/.excalibur", 0o755); err != nil {
		return err
	}
	return os.WriteFile(proj+"/.excalibur/"+name, []byte("x"), 0o600)
}

func TestBootSortsByKindThenAge(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeStore{snaps: []RunSnapshot{
		{ID: "bg-old", Kind: KindBackground, State: types.RunRunning, LastJournalAt: now.Add(-3 * time.Hour)},
		{ID: "bg-new", Kind: KindBackground, State: types.RunRunning, LastJournalAt: now.Add(-time.Hour)},
		{ID: "m", Kind: KindMasterTurn, State: types.RunRunning, LastJournalAt: now.Add(-3 * time.Hour)},
	}}
	rep, err := Boot(context.Background(), store, &fakeReconciler{}, nil, nil, now, now)
	if err != nil {
		t.Fatalf("Boot: %v", err)
	}
	want := []string{"m", "bg-old", "bg-new"}
	for i, id := range want {
		if rep.Order[i] != id {
			t.Fatalf("order = %v, want %v", rep.Order, want)
		}
	}
	var _ = sort.Strings
}
