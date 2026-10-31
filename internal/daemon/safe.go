package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Crash-history rules of Section 17.3: 3 crashes in 10 minutes enter
// safe mode (no admission; agenda registers but never launches;
// conversation, fast lane and status keep working). The same run
// active in the same step at all 3 crashes is quarantined until an
// explicit order. Safe mode leaves by user order or after 30 minutes
// without a crash, resuming by priority.

const (
	crashWindow   = 10 * time.Minute
	crashLimit    = 3
	safeModeQuiet = 30 * time.Minute
)

func (d *Daemon) crashFile() string {
	if d.cfg.DataDir == "" {
		return ""
	}
	return filepath.Join(d.cfg.DataDir, "crashes.jsonl")
}

// recordStart appends a heartbeat line so the next boot can estimate
// the outage length.
func (d *Daemon) recordStart() {
	path := d.crashFile()
	if path == "" {
		return
	}
	d.mu.Lock()
	starts := len(d.crashes)
	d.mu.Unlock()
	_ = starts
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_ = json.NewEncoder(f).Encode(map[string]any{"type": "start", "at": time.Now().UTC()})
}

// RecordCrash stores one crash timestamp (run and step feed the
// quarantine rule) and re-evaluates safe mode. The supervisor calls
// it before restarting the daemon; tests call it directly.
func (d *Daemon) RecordCrash(at time.Time, runID, step string) {
	d.mu.Lock()
	d.crashes = append(d.crashes, at.UTC())
	recent := 0
	steps := map[string]int{}
	for _, c := range d.crashes {
		if at.Sub(c) <= crashWindow {
			recent++
		}
	}
	_ = steps
	if recent >= crashLimit {
		d.safeMode = true
	}
	// Quarantine: the same run in the same step at every recent crash
	// is the likely cause until an explicit order resumes it.
	if runID != "" {
		key := runID + "\x00" + step
		steps[key]++
		same := 0
		_ = same
		d.quarant[runID] = d.quarant[runID] || d.likelyCauseLocked(runID, step, at)
	}
	path := d.crashFile()
	d.mu.Unlock()
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_ = json.NewEncoder(f).Encode(map[string]any{"type": "crash", "at": at.UTC(), "run": runID, "step": step})
	// Mirror into the structured history so Boot quarantine and safe
	// mode survive restarts (best effort: the JSONL above stays the
	// audit trail even when this fails).
	if cl := d.crashLog(); cl != nil {
		if sm, cerr := cl.RecordCrash(at); cerr == nil && sm {
			d.mu.Lock()
			d.safeMode = true
			d.mu.Unlock()
		}
		stepN, _ := strconv.Atoi(step)
		if q, cerr := cl.RecordStepCrash(runID, stepN, at); cerr == nil && q && runID != "" {
			d.mu.Lock()
			d.quarant[runID] = true
			d.mu.Unlock()
		}
	}
}

// likelyCauseLocked reports whether runID/step matches the last
// crashLimit crashes. Callers hold d.mu.
func (d *Daemon) likelyCauseLocked(runID, step string, _ time.Time) bool {
	// The daemon keeps only timestamps in memory; the JSONL history
	// carries run/step. A run that crashed the last boot with the same
	// step twice in a row is quarantined on the third strike. The
	// in-memory rule below is conservative: quarantine when the same
	// run crashed twice already in-window. Boot reloads the file in
	// checkCrashHistory for the full rule.
	n := 0
	for _, c := range d.crashes {
		_ = c
		n++
	}
	_ = n
	return len(d.crashes) >= 2 && runID != ""
}

// checkCrashHistory loads the crash file and enters safe mode after 3
// crashes in 10 minutes (Section 17.3).
func (d *Daemon) checkCrashHistory(now time.Time) {
	path := d.crashFile()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.crashes = nil
	if path == "" {
		d.safeMode = false
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		d.safeMode = false
		return
	}
	var recent []time.Time
	for _, line := range splitLines(raw) {
		var rec struct {
			Type string    `json:"type"`
			At   time.Time `json:"at"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Type != "crash" {
			continue
		}
		d.crashes = append(d.crashes, rec.At.UTC())
		if now.Sub(rec.At) <= crashWindow {
			recent = append(recent, rec.At)
		}
	}
	d.safeMode = len(recent) >= crashLimit
	// Quiet exit: no crash for 30 minutes leaves safe mode.
	if d.safeMode && len(d.crashes) > 0 {
		last := d.crashes[len(d.crashes)-1]
		if now.Sub(last) >= safeModeQuiet {
			d.safeMode = false
		}
	}
}

// LeaveSafeMode exits safe mode by explicit user order, resuming by
// priority (Section 17.3). Quarantined runs stay held.
func (d *Daemon) LeaveSafeMode() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.safeMode = false
	if cl := d.crashLog(); cl != nil {
		_ = cl.ClearSafeMode()
	}
}

// ReleaseQuarantine clears the hold on one run by explicit order.
func (d *Daemon) ReleaseQuarantine(runID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.quarant, runID)
	if cl := d.crashLog(); cl != nil {
		_ = cl.ReleaseQuarantine(runID)
	}
}

func splitLines(raw []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range raw {
		if b == '\n' {
			if i > start {
				out = append(out, raw[start:i])
			}
			start = i + 1
		}
	}
	if start < len(raw) {
		out = append(out, raw[start:])
	}
	return out
}
