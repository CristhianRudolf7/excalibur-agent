package telemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Bus is the live event bus of Section 22.1: every action emits one
// typed event, subscribers resume from a cursor without gaps or
// duplicates, and every event is also appended to the telemetry sink
// so a cut trial keeps partial data.
type Bus struct {
	mu     sync.Mutex
	events []types.Event
	seq    uint64
	subs   []chan types.Event
	sink   *Sink
}

// NewBus returns an empty bus. Sink may be nil (memory only).
func NewBus(sink *Sink) *Bus {
	return &Bus{sink: sink}
}

// Publish emits one typed event. Payload must be JSON-marshalable; it
// is stored as raw JSON on the envelope.
func (b *Bus) Publish(typ types.EventType, runID *types.RunID, taskID *types.TaskID, payload any) (types.Event, error) {
	var raw json.RawMessage
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return types.Event{}, fmt.Errorf("telemetry: marshal payload: %w", err)
		}
		raw = buf
	}
	b.mu.Lock()
	b.seq++
	ev := types.Event{
		Seq:     b.seq,
		ID:      fmt.Sprintf("evt_%d", b.seq),
		Type:    typ,
		At:      time.Now().UTC(),
		RunID:   runID,
		TaskID:  taskID,
		Payload: raw,
	}
	b.events = append(b.events, ev)
	subs := append([]chan types.Event(nil), b.subs...)
	sink := b.sink
	b.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
		}
	}
	if sink != nil {
		if err := sink.AppendEvent(ev); err != nil {
			return ev, err
		}
	}
	return ev, nil
}

// Replay returns events with Seq greater than cursor, in order. An
// empty cursor replays from the start. It never skips or duplicates:
// the cursor is the last delivered sequence number (Section 3.5).
func (b *Bus) Replay(cursor types.Cursor) []types.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []types.Event
	for _, ev := range b.events {
		if ev.Seq > cursor.Seq {
			out = append(out, ev)
		}
	}
	return out
}

// Len returns the number of buffered events.
func (b *Bus) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.events)
}

// Subscribe opens a live channel buffered with size. The caller must
// first Replay from its cursor, then drain the channel to avoid gaps.
func (b *Bus) Subscribe(size int) <-chan types.Event {
	if size <= 0 {
		size = 64
	}
	ch := make(chan types.Event, size)
	b.mu.Lock()
	b.subs = append(b.subs, ch)
	b.mu.Unlock()
	return ch
}

// Sink is an incremental append-only JSONL sink for events, metrics
// and ATIF steps. Every write is flushed and synced so a trial cut by
// timeout, OOM or crash keeps valid partial data (Section 23.2).
type Sink struct {
	mu   sync.Mutex
	dir  string
	evF  *os.File
	metF *os.File
}

// OpenSink creates dir (usually /logs/agent/excalibur) and opens the
// append-only files inside it.
func OpenSink(dir string) (*Sink, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("telemetry: mkdir sink: %w", err)
	}
	evF, err := os.OpenFile(dir+"/events.jsonl", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("telemetry: open events sink: %w", err)
	}
	metF, err := os.OpenFile(dir+"/metrics.jsonl", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		_ = evF.Close()
		return nil, fmt.Errorf("telemetry: open metrics sink: %w", err)
	}
	return &Sink{dir: dir, evF: evF, metF: metF}, nil
}

// Dir returns the sink directory.
func (s *Sink) Dir() string { return s.dir }

// AppendEvent writes one event as a single JSON line and syncs.
func (s *Sink) AppendEvent(ev types.Event) error {
	return s.appendJSON(s.evF, ev)
}

// AppendMetrics writes one metrics snapshot as a single JSON line.
func (s *Sink) AppendMetrics(v any) error {
	return s.appendJSON(s.metF, v)
}

func (s *Sink) appendJSON(f *os.File, v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("telemetry: marshal sink record: %w", err)
	}
	buf = append(buf, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := f.Write(buf); err != nil {
		return fmt.Errorf("telemetry: write sink: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("telemetry: sync sink: %w", err)
	}
	return nil
}

// Close closes the sink files.
func (s *Sink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var first error
	if s.evF != nil {
		if err := s.evF.Close(); err != nil && first == nil {
			first = err
		}
		s.evF = nil
	}
	if s.metF != nil {
		if err := s.metF.Close(); err != nil && first == nil {
			first = err
		}
		s.metF = nil
	}
	return first
}
