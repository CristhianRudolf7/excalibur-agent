package robotlab

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// eventSubscriber keeps 64 events in memory. Frames evict the oldest frame;
// reliable overflow is spooled to an unlinked temporary file, never frames.
// This keeps callbacks off the protocol reader without losing control events.
type eventSubscriber struct {
	mu          sync.Mutex
	queue       []Event
	wake        chan struct{}
	done        chan struct{}
	closed      bool
	spool       *os.File
	read, write int64
}

func newEventSubscriber(fn func(Event)) *eventSubscriber {
	s := &eventSubscriber{wake: make(chan struct{}, 1), done: make(chan struct{})}
	safego.Go("robotlab-subscriber", nil, func() {
		defer s.close()
		for {
			select {
			case <-s.done:
				return
			case <-s.wake:
				for {
					e, ok := s.pop()
					if !ok {
						break
					}
					fn(e)
				}
			}
		}
	})
	return s
}
func (s *eventSubscriber) enqueue(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if len(s.queue) >= 64 || s.write > s.read {
		for i, v := range s.queue {
			if v.Name == "frame" {
				s.queue = append(s.queue[:i], s.queue[i+1:]...)
				break
			}
		}
		if e.Name == "frame" && (len(s.queue) >= 64 || s.write > s.read) {
			return
		}
	}
	if len(s.queue) < 64 && s.write == s.read {
		s.queue = append(s.queue, e)
	} else {
		if s.spool == nil {
			f, err := os.CreateTemp("", "excalibur-robotlab-events-*")
			if err != nil {
				slog.Error("Robot Lab event spool unavailable", "error", err)
				return
			}
			s.spool = f
			_ = os.Remove(f.Name())
		}
		b, _ := json.Marshal(e)
		header := make([]byte, 4)
		binary.BigEndian.PutUint32(header, uint32(len(b)))
		record := append(header, b...)
		if _, err := s.spool.WriteAt(record, s.write); err != nil {
			slog.Error("Robot Lab event spool write failed", "error", err)
			return
		}
		s.write += int64(len(record))
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *eventSubscriber) pop() (Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Event{}, false
	}
	if len(s.queue) > 0 {
		e := s.queue[0]
		s.queue[0] = Event{}
		s.queue = s.queue[1:]
		return e, true
	}
	if s.write > s.read {
		var header [4]byte
		if _, err := s.spool.ReadAt(header[:], s.read); err != nil {
			return Event{}, false
		}
		b := make([]byte, binary.BigEndian.Uint32(header[:]))
		if _, err := s.spool.ReadAt(b, s.read+4); err != nil && err != io.EOF {
			return Event{}, false
		}
		s.read += 4 + int64(len(b))
		var e Event
		_ = json.Unmarshal(b, &e)
		if s.read == s.write {
			_ = s.spool.Truncate(0)
			s.read = 0
			s.write = 0
		}
		return e, true
	}
	return Event{}, false
}
func (s *eventSubscriber) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.done)
		if s.spool != nil {
			_ = s.spool.Close()
			_ = os.Remove(s.spool.Name())
		}
	}
}
