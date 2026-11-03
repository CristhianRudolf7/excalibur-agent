package telemetry

import (
	"encoding/json"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// PublishEphemeral sends live-only data to explicitly registered consumers.
// It allocates no sequence, replay record, sink entry or ordinary subscriber
// channel. Callbacks must return immediately (drop when the consumer is busy).
func (b *Bus) PublishEphemeral(typ types.EventType, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ev := types.Event{Type: typ, At: time.Now().UTC(), Payload: raw}
	b.mu.Lock()
	callbacks := make([]func(types.Event), 0, len(b.ephemeral))
	for _, fn := range b.ephemeral {
		callbacks = append(callbacks, fn)
	}
	b.mu.Unlock()
	for _, fn := range callbacks {
		fn(ev)
	}
	return nil
}

// SubscribeEphemeral registers a nonblocking live consumer. Unsubscribe releases
// it on disconnect; ephemeral events are never delivered via Subscribe/Replay.
func (b *Bus) SubscribeEphemeral(fn func(types.Event)) func() {
	b.mu.Lock()
	if b.ephemeral == nil {
		b.ephemeral = map[uint64]func(types.Event){}
	}
	b.ephemeralNext++
	id := b.ephemeralNext
	b.ephemeral[id] = fn
	b.mu.Unlock()
	return func() { b.mu.Lock(); delete(b.ephemeral, id); b.mu.Unlock() }
}

// SubscribeLive is Subscribe with explicit lifetime ownership. Removing a
// subscriber never closes its channel, so an in-flight publisher stays safe.
func (b *Bus) SubscribeLive(size int) (<-chan types.Event, func()) {
	if size <= 0 {
		size = 64
	}
	ch := make(chan types.Event, size)
	b.mu.Lock()
	b.subs = append(b.subs, ch)
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for i, c := range b.subs {
			if c == ch {
				b.subs = append(b.subs[:i], b.subs[i+1:]...)
				break
			}
		}
	}
}
