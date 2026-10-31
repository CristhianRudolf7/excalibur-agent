package orchestrator

import (
	"context"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Journal event types the orchestrator records (subset of the Section
// 17.1 journal table relevant to hierarchy and control).
const (
	JournalRunStarted        = "run_started"
	JournalStateChange       = "state_change"
	JournalDirectiveReceived = "directive_received"
	JournalDirectiveApplied  = "directive_applied"
	JournalCompletion        = "completion"
)

// Journal is the durable record the orchestrator appends to: run
// creation, every state transition, directive delivery and
// application, and delivery reports. The journal package (H3/H5
// territory) will back this with SQLite; embedded mode uses a
// RecordingJournal or a NoopJournal.
type Journal interface {
	Append(ctx context.Context, run types.RunID, typ, payload string) error
}

// NoopJournal discards every entry. It is for callers that only need
// in-memory behavior.
type NoopJournal struct{}

// Append implements Journal.
func (NoopJournal) Append(_ context.Context, _ types.RunID, _, _ string) error {
	return nil
}

// JournalEntry is one recorded journal row.
type JournalEntry struct {
	Run     types.RunID
	Type    string
	Payload string
	At      time.Time
}

// RecordingJournal keeps every entry in RAM for tests.
type RecordingJournal struct {
	mu      sync.Mutex
	entries []JournalEntry
}

// Append implements Journal.
func (j *RecordingJournal) Append(_ context.Context, run types.RunID, typ, payload string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entries = append(j.entries, JournalEntry{Run: run, Type: typ, Payload: payload, At: time.Now().UTC()})
	return nil
}

// Entries returns a copy of the recorded rows in append order.
func (j *RecordingJournal) Entries() []JournalEntry {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]JournalEntry(nil), j.entries...)
}

// Types returns the recorded event types in append order.
func (j *RecordingJournal) Types() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]string, 0, len(j.entries))
	for _, e := range j.entries {
		out = append(out, e.Type)
	}
	return out
}
