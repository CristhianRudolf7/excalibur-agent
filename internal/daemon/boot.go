// Boot recovery after a crash or reboot (Section 17.3).
//
// Boot runs the six steps of the startup algorithm: open the databases
// (quick_check on unclean shutdown, pending migrations, porter start),
// rebuild the RAM indexes, collect unfinished runs ordered by priority
// (master turns first, then user-initiated, then scheduled and
// background), reconcile each run, catch up the agenda (17.4) and emit
// one summary notice. Step 4 reuses the per-run recovery of
// internal/engine (Recover/ResolvePending for 17.2) through the narrow
// RunReconciler interface so this package never duplicates effect
// classification.
//
// Safe mode trips after 3 crashes in 10 minutes (Appendix B): no run
// resumes, every run parks as paused(safe_mode), the agenda records but
// does not launch, and the mode clears by user order or after 30 clean
// minutes. A run that was active at the same step during all 3 crashes
// is quarantined as blocked (possible crash cause) until an explicit
// order releases it.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// RunView is the minimal run row adoption needs.
type RunView struct {
	RunID     string
	State     types.RunState
	UpdatedAt time.Time
	Step      string // engine step label for quarantine matching
}

// Adoption reconciles non-terminal runs at boot (Section 17.3). The
// daemon lists candidates through ListRuns; Decide maps each to its
// disposition; Apply carries it out (resume, pause, block, keep).
type Adoption interface {
	ListRuns() []RunView
	Decide(run RunView, downFor time.Duration) AdoptDecision
	Apply(run RunView, decision AdoptDecision) error
}

// AdoptDecision names the Section 17.3 continuation.
type AdoptDecision string

// Adoption dispositions.
const (
	AdoptResume  AdoptDecision = "resume"  // <6h, reconciled, no uncertainty
	AdoptPause   AdoptDecision = "pause"   // recovery: long outage or low budget
	AdoptBlock   AdoptDecision = "block"   // uncertain effect: urgent notice
	AdoptWaiting AdoptDecision = "waiting" // user wait survives with its ask intact
	AdoptKeep    AdoptDecision = "keep"    // already terminal or safe mode
)

// AdoptionSummary counts one boot pass for the single master notice.
type AdoptionSummary struct {
	Resumed int
	Paused  int
	Blocked int
	Waiting int
	Kept    int
}

// DefaultDecide implements the Section 17.3 table: user waits
// survive, uncertain effects block, long outages pause, the rest
// resumes with a recovery note.
func DefaultDecide(run RunView, downFor time.Duration, uncertain bool, waitingUser bool) AdoptDecision {
	switch {
	case waitingUser:
		return AdoptWaiting
	case uncertain:
		return AdoptBlock
	case run.State == types.RunPaused && false:
		return AdoptKeep
	case downFor > 6*time.Hour:
		return AdoptPause
	case run.State.IsTerminal():
		return AdoptKeep
	default:
		return AdoptResume
	}
}

// adopt runs the Section 17.3 pass: safe mode pauses everything with
// the recovery reason, otherwise each run goes through Decide/Apply.
// It returns the summary the master reports as the single notice.
func (d *Daemon) adopt(_ context.Context, now time.Time) (AdoptionSummary, error) {
	var sum AdoptionSummary
	if d.dep.Adoption == nil {
		return sum, nil
	}
	d.mu.Lock()
	safe := d.safeMode
	d.mu.Unlock()
	downFor := d.downtime(now)
	for _, run := range d.dep.Adoption.ListRuns() {
		var dec AdoptDecision
		if safe {
			dec = AdoptPause
		} else {
			dec = d.dep.Adoption.Decide(run, downFor)
		}
		if err := d.dep.Adoption.Apply(run, dec); err != nil {
			continue
		}
		switch dec {
		case AdoptResume:
			sum.Resumed++
		case AdoptPause:
			sum.Paused++
		case AdoptBlock:
			sum.Blocked++
		case AdoptWaiting:
			sum.Waiting++
		default:
			sum.Kept++
		}
	}
	return sum, nil
}

// downtime estimates the outage from the crash-history file; without
// history it reports zero (clean first start).
func (d *Daemon) downtime(now time.Time) time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.crashes) == 0 {
		return 0
	}
	last := d.crashes[len(d.crashes)-1]
	if now.Before(last) {
		return 0
	}
	return now.Sub(last)
}

// Appendix B boot parameters.
const (
	// AutoResumeWithin bounds automatic requeueing after an
	// interruption: shorter than 6h and reconciled without
	// uncertainty resumes; anything else parks (17.3 step 4).
	AutoResumeWithin = 6 * time.Hour
	// SafeModeCrashes trips safe mode (17.3): this many crashes ...
	SafeModeCrashes = 3
	// SafeModeWindow is the window they must fall in (10 minutes).
	SafeModeWindow = 10 * time.Minute
	// SafeModeCooldown clears safe mode after this long without
	// crashes (30 minutes) when no explicit order arrives.
	SafeModeCooldown = 30 * time.Minute
)

// RunKind orders unfinished runs at boot (17.3 step 3): master turns
// first, then user-initiated runs, then scheduled and background work.
type RunKind int

// Run kinds in boot priority order.
const (
	KindMasterTurn RunKind = iota
	KindUser
	KindScheduled
	KindBackground
)

// String reports the kind for notices and tests.
func (k RunKind) String() string {
	switch k {
	case KindMasterTurn:
		return "master_turn"
	case KindUser:
		return "user"
	case KindScheduled:
		return "scheduled"
	default:
		return "background"
	}
}

// Outcome is the boot decision for one run (17.3 step 4).
type Outcome int

// Boot outcomes.
const (
	// OutcomeResume requeues the run with a recovery note (L5):
	// it was running, reconciled without uncertainty and the
	// interruption was short.
	OutcomeResume Outcome = iota
	// OutcomePaused parks the run as paused(recovery): long
	// interruption or budget nearly gone.
	OutcomePaused
	// OutcomeBlocked parks the run as blocked(uncertain_effect)
	// with an urgent notice: an effect may have happened and must
	// never repeat automatically.
	OutcomeBlocked
	// OutcomeWaiting restores waiting(user) with the question or
	// plan approval intact.
	OutcomeWaiting
	// OutcomeQuarantined parks the run as blocked (possible crash
	// cause): it needs an explicit order to resume.
	OutcomeQuarantined
	// OutcomeSafeMode parks the run as paused(safe_mode): the
	// daemon is in safe mode and resumes nothing.
	OutcomeSafeMode
)

// String reports the outcome for notices and tests.
func (o Outcome) String() string {
	switch o {
	case OutcomeResume:
		return "resume"
	case OutcomePaused:
		return "paused"
	case OutcomeBlocked:
		return "blocked"
	case OutcomeWaiting:
		return "waiting"
	case OutcomeQuarantined:
		return "quarantined"
	default:
		return "safe_mode"
	}
}

// DecideInput is everything step 4 needs for one run, already
// reconciled: file fingerprints compared, terminal sessions marked
// lost, capability tasks reconsulted, and the pending tool leg
// classified per 17.2.
type DecideInput struct {
	Kind          RunKind
	WasRunning    bool
	WaitingUser   bool
	Uncertain     bool
	Quarantined   bool
	SafeMode      bool
	InterruptedBy time.Duration
	BudgetGone    bool
}

// Decide applies 17.3 step 4 in priority order: safe mode,
// quarantine, waiting user, uncertain effect, long interruption or
// spent budget, else resume. It is pure and table-tested.
func Decide(in DecideInput) Outcome {
	switch {
	case in.SafeMode:
		return OutcomeSafeMode
	case in.Quarantined:
		return OutcomeQuarantined
	case in.WaitingUser:
		return OutcomeWaiting
	case in.Uncertain:
		return OutcomeBlocked
	case in.InterruptedBy > AutoResumeWithin:
		return OutcomePaused
	case in.BudgetGone:
		return OutcomePaused
	default:
		return OutcomeResume
	}
}

// PauseReasonFor maps an outcome to the stored pause reason.
func PauseReasonFor(o Outcome) types.PauseReason {
	switch o {
	case OutcomeSafeMode:
		return types.PauseSafeMode
	default:
		return types.PauseRecovery
	}
}

// BlockReasonFor maps an outcome to the stored block reason.
func BlockReasonFor(o Outcome) types.BlockReason {
	switch o {
	case OutcomeQuarantined:
		return types.BlockMissingData
	default:
		return types.BlockUncertainEffect
	}
}

// RecoveryNote builds the L5 note attached to a resumed run: it names
// the restart, the lost terminal sessions and the reconciled tree
// state (17.3 step 4).
func RecoveryNote(lostSessions []string, treeState string) string {
	note := "system restarted; terminal sessions lost: "
	if len(lostSessions) == 0 {
		note += "none"
	} else {
		for i, s := range lostSessions {
			if i > 0 {
				note += ", "
			}
			note += s
		}
	}
	note += "; reconciled tree state: " + treeState
	return note
}

// CrashRecord is one crash marker for safe-mode accounting.
type CrashRecord struct {
	At time.Time `json:"at"`
}

// StepCrash records that a crash happened while a run was active at a
// step, for quarantine detection.
type StepCrash struct {
	RunID string    `json:"run_id"`
	Step  int       `json:"step"`
	At    time.Time `json:"at"`
}

// crashFile is the persisted crash history beside the databases.
type crashFile struct {
	Crashes    []CrashRecord `json:"crashes"`
	StepCrash  []StepCrash   `json:"step_crashes"`
	SafeMode   bool          `json:"safe_mode"`
	EnteredAt  time.Time     `json:"safe_mode_entered_at,omitempty"`
	Quarantine []string      `json:"quarantine,omitempty"`
}

// CrashLog persists crash markers across restarts so three quick
// crashes trip safe mode even though each boot is a fresh process.
type CrashLog struct {
	path string
}

// NewCrashLog returns a log backed by dir/crashes.json.
func NewCrashLog(dir string) *CrashLog {
	return &CrashLog{path: filepath.Join(dir, "crashes.json")}
}

// load reads the file; a missing file means a clean history.
func (l *CrashLog) load() crashFile {
	var cf crashFile
	raw, err := os.ReadFile(l.path)
	if err != nil || len(raw) == 0 {
		return cf
	}
	_ = json.Unmarshal(raw, &cf)
	return cf
}

// save writes the file atomically (temp + rename) with 0600 perms so
// a crash mid-write never corrupts the history.
func (l *CrashLog) save(cf crashFile) error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	raw, err := json.Marshal(cf)
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

// RecordCrash appends a crash marker at now and reports whether this
// boot must enter safe mode (3 crashes in 10 minutes).
func (l *CrashLog) RecordCrash(now time.Time) (safeMode bool, err error) {
	cf := l.load()
	cf.Crashes = append(cf.Crashes, CrashRecord{At: now.UTC()})
	cutoff := now.Add(-SafeModeWindow)
	kept := cf.Crashes[:0]
	for _, c := range cf.Crashes {
		if !c.At.Before(cutoff) {
			kept = append(kept, c)
		}
	}
	cf.Crashes = kept
	if len(cf.Crashes) >= SafeModeCrashes {
		cf.SafeMode = true
		cf.EnteredAt = now.UTC()
		safeMode = true
	}
	if serr := l.save(cf); serr != nil {
		return false, serr
	}
	return safeMode, nil
}

// RecordStepCrash notes that a crash found runID active at step; three
// crashes with the same run at the same step quarantines it.
func (l *CrashLog) RecordStepCrash(runID string, step int, now time.Time) (quarantined bool, err error) {
	cf := l.load()
	cf.StepCrash = append(cf.StepCrash, StepCrash{RunID: runID, Step: step, At: now.UTC()})
	cutoff := now.Add(-SafeModeWindow)
	kept := cf.StepCrash[:0]
	same := 0
	for _, s := range cf.StepCrash {
		if s.At.Before(cutoff) {
			continue
		}
		kept = append(kept, s)
		if s.RunID == runID && s.Step == step {
			same++
		}
	}
	cf.StepCrash = kept
	if same >= SafeModeCrashes {
		quarantined = true
		found := false
		for _, q := range cf.Quarantine {
			if q == runID {
				found = true
			}
		}
		if !found {
			cf.Quarantine = append(cf.Quarantine, runID)
		}
	}
	if serr := l.save(cf); serr != nil {
		return false, serr
	}
	return quarantined, nil
}

// InSafeMode reports whether the persisted flag is set.
func (l *CrashLog) InSafeMode() bool { return l.load().SafeMode }

// SafeModeSince reports when safe mode was entered.
func (l *CrashLog) SafeModeSince() time.Time { return l.load().EnteredAt }

// Quarantined lists runs needing an explicit order to resume.
func (l *CrashLog) Quarantined() []string { return append([]string(nil), l.load().Quarantine...) }

// IsQuarantined reports whether runID is quarantined.
func (l *CrashLog) IsQuarantined(runID string) bool {
	for _, q := range l.load().Quarantine {
		if q == runID {
			return true
		}
	}
	return false
}

// ClearSafeMode lifts the flag by user order.
func (l *CrashLog) ClearSafeMode() error {
	cf := l.load()
	cf.SafeMode = false
	return l.save(cf)
}

// ReleaseQuarantine removes runID after an explicit resume order.
func (l *CrashLog) ReleaseQuarantine(runID string) error {
	cf := l.load()
	kept := cf.Quarantine[:0]
	for _, q := range cf.Quarantine {
		if q != runID {
			kept = append(kept, q)
		}
	}
	cf.Quarantine = kept
	return l.save(cf)
}

// MaybeExitSafeMode clears safe mode after 30 clean minutes without
// crashes; it reports whether the mode is still active.
func (l *CrashLog) MaybeExitSafeMode(now time.Time) (active bool, err error) {
	cf := l.load()
	if !cf.SafeMode {
		return false, nil
	}
	if len(cf.Crashes) > 0 {
		last := cf.Crashes[0].At
		for _, c := range cf.Crashes[1:] {
			if c.At.After(last) {
				last = c.At
			}
		}
		if now.Sub(last) < SafeModeCooldown {
			return true, nil
		}
	} else if now.Sub(cf.EnteredAt) < SafeModeCooldown {
		return true, nil
	}
	cf.SafeMode = false
	if serr := l.save(cf); serr != nil {
		return true, serr
	}
	return false, nil
}

// RunSnapshot is the boot-time view of one unfinished run (17.3 step
// 3). It carries only what the decision needs; per-run journal replay
// stays behind RunReconciler.
type RunSnapshot struct {
	ID            string
	Kind          RunKind
	State         types.RunState
	LastStep      int
	LastJournalAt time.Time
	WaitingUser   bool
	BudgetGone    bool
	// Reconciled is filled by the reconciler: file diffs applied,
	// sessions marked lost, capability tasks reconsulted.
	Reconciled bool
	// Uncertain mirrors the 17.2 pending-leg classification: true
	// when a non-idempotent intent has no result and no probe
	// confirms it.
	Uncertain bool
	// LostSessions names terminal sessions that died with the old
	// process (17.3: processes die with the daemon via Pdeathsig
	// and the process group).
	LostSessions []string
	TreeState    string
}

// RunReconciler rebuilds one run from its checkpoint plus trailing
// entries, reconciles the world and classifies the pending leg (17.3
// step 4 i-iii). Implementations wrap internal/engine recovery.
type RunReconciler interface {
	Reconcile(ctx context.Context, snap *RunSnapshot) error
}

// RunStore persists boot transitions without importing the
// orchestrator store (H8 owns it): the daemon adapts it.
type RunStore interface {
	Unfinished() ([]RunSnapshot, error)
	SetState(id string, state types.RunState, pause *types.PauseReason, block *types.BlockReason, note string) error
}

// AgendaCatchUp recalculates firings lost during the downtime (17.4)
// and, in safe mode, records without launching.
type AgendaCatchUp interface {
	CatchUp(downSince, now time.Time, deferLaunch bool) (fired, deferred int, err error)
}

// BootReport is the outcome of one boot: per-run outcomes plus the
// agenda tally and the single master notice (17.3 steps 5-6).
type BootReport struct {
	Outcomes   map[string]Outcome
	Order      []string
	SafeMode   bool
	Fired      int
	Deferred   int
	Notice     string
	Quarantine []string
}

// Boot runs the startup algorithm. now anchors interruption lengths;
// downSince anchors agenda catch-up (17.4).
func Boot(ctx context.Context, store RunStore, rec RunReconciler, agenda AgendaCatchUp, crashes *CrashLog, now, downSince time.Time) (*BootReport, error) {
	safe := false
	if crashes != nil {
		safe = crashes.InSafeMode()
	}
	snaps, err := store.Unfinished()
	if err != nil {
		return nil, fmt.Errorf("daemon: boot list unfinished: %w", err)
	}
	sort.Slice(snaps, func(i, j int) bool {
		if snaps[i].Kind != snaps[j].Kind {
			return snaps[i].Kind < snaps[j].Kind
		}
		return snaps[i].LastJournalAt.Before(snaps[j].LastJournalAt)
	})
	rep := &BootReport{Outcomes: map[string]Outcome{}, Order: []string{}, SafeMode: safe}
	var resumed, paused, blocked, waiting int
	for i := range snaps {
		s := &snaps[i]
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		in := DecideInput{
			Kind:          s.Kind,
			WasRunning:    s.State == types.RunRunning || s.State == types.RunRecovering,
			WaitingUser:   s.WaitingUser,
			SafeMode:      safe,
			InterruptedBy: now.Sub(s.LastJournalAt),
			BudgetGone:    s.BudgetGone,
		}
		if crashes != nil && crashes.IsQuarantined(s.ID) {
			in.Quarantined = true
		}
		if !safe && !in.Quarantined {
			if rec != nil {
				if rerr := rec.Reconcile(ctx, s); rerr != nil {
					return nil, fmt.Errorf("daemon: boot reconcile %q: %w", s.ID, rerr)
				}
			}
			in.Uncertain = s.Uncertain
		}
		out := Decide(in)
		rep.Outcomes[s.ID] = out
		rep.Order = append(rep.Order, s.ID)
		var st types.RunState
		var pause *types.PauseReason
		var block *types.BlockReason
		note := ""
		switch out {
		case OutcomeResume:
			st = types.RunQueued
			note = RecoveryNote(s.LostSessions, s.TreeState)
			resumed++
		case OutcomeWaiting:
			st = types.RunWaiting
			note = "restored waiting for the user; the question is intact"
			waiting++
		case OutcomeBlocked:
			st = types.RunBlocked
			b := BlockReasonFor(out)
			block = &b
			note = "uncertain effect: never repeats automatically; needs information or a question"
			blocked++
		case OutcomeQuarantined:
			st = types.RunBlocked
			b := BlockReasonFor(out)
			block = &b
			note = "quarantined: possible crash cause; resumes only by explicit order"
			blocked++
		default: // OutcomePaused, OutcomeSafeMode
			st = types.RunPaused
			p := PauseReasonFor(out)
			pause = &p
			if out == OutcomeSafeMode {
				note = "paused: daemon in safe mode"
			} else {
				note = "paused for review after restart"
			}
			paused++
		}
		if serr := store.SetState(s.ID, st, pause, block, note); serr != nil {
			return nil, fmt.Errorf("daemon: boot park %q: %w", s.ID, serr)
		}
	}
	if agenda != nil {
		fired, deferred, aerr := agenda.CatchUp(downSince, now, safe)
		if aerr != nil {
			return nil, fmt.Errorf("daemon: boot agenda catch-up: %w", aerr)
		}
		rep.Fired, rep.Deferred = fired, deferred
	}
	if crashes != nil {
		rep.Quarantine = crashes.Quarantined()
	}
	rep.Notice = fmt.Sprintf("restart summary: %d resumed, %d paused, %d blocked, %d waiting; agenda fired %d, deferred %d; safe mode: %v",
		resumed, paused, blocked, waiting, rep.Fired, rep.Deferred, safe)
	return rep, nil
}
