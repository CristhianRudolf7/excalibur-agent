package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// subscription is one live event feed: a cursor plus an optional type
// filter. After a disconnect the client replays from its cursor, so
// delivery has no gaps and no duplicates (Section 3.5). The tail
// goroutine owns the cursor after attach; the read loop only sets it
// under mu before attaching.
type subscription struct {
	mu     sync.Mutex
	id     string
	cursor types.Cursor
	types  []string
	ch     chan types.Event
}

func (s *Server) newSub(filter []string) *subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub := &subscription{
		id:    fmt.Sprintf("sub_%d", len(s.subs)+1),
		types: filter,
		ch:    make(chan types.Event, 128),
	}
	s.subs[sub.id] = sub
	return sub
}

func (s *Server) dropSub(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subs, id)
}

func (s *Server) closeAllSubs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.subs {
		delete(s.subs, id)
	}
}

func matchTypes(ev types.Event, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, t := range filter {
		if string(ev.Type) == t {
			return true
		}
	}
	return false
}

// replay returns buffered events after cursor, filtered by types.
func (s *Server) replay(cursor types.Cursor, filter []string) []types.Event {
	if s.bus == nil {
		return nil
	}
	var out []types.Event
	for _, ev := range s.bus.Replay(cursor) {
		if matchTypes(ev, filter) {
			out = append(out, ev)
		}
	}
	return out
}

// attach streams bus events after the subscribe cursor to the socket
// beside the read loop: the handler returns at once and the tail ends
// with the request context. Writes serialize through the peer, so RPC
// responses and event pushes never interleave mid-frame.
func (sub *subscription) attach(ctx context.Context, s *Server, peer *wsPeer) {
	if s.bus == nil {
		return
	}
	live := s.bus.Subscribe(128)
	safego.Go("gateway-live", nil, func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-live:
				sub.mu.Lock()
				cur, typ := sub.cursor, sub.types
				if ev.Seq > cur.Seq && matchTypes(ev, typ) {
					sub.cursor = types.Cursor{Seq: ev.Seq}
				} else {
					sub.mu.Unlock()
					continue
				}
				sub.mu.Unlock()
				notif, err := json.Marshal(map[string]any{
					"jsonrpc": api.JSONRPCVersion,
					"method":  "event",
					"params":  ev,
				})
				if err != nil {
					continue
				}
				if err := peer.writeRaw(ctx, notif); err != nil {
					return
				}
			}
		}
	})
}

// clientIDOf extracts the calling client identity for idempotency
// scoping. HTTP callers pass X-Client-ID; WebSocket callers pass it
// as a query parameter. Empty means the default client.
func clientIDOf(ctx context.Context) string {
	if v, ok := ctx.Value(clientIDKey{}).(string); ok {
		return v
	}
	return ""
}

type clientIDKey struct{}

// PublishEvent emits one typed bus event and fans it out to matching
// live subscriptions. Gate handlers call this after every state
// change so subscribers observe progress without polling.
func (s *Server) PublishEvent(typ types.EventType, runID *types.RunID, taskID *types.TaskID, payload any) (types.Event, error) {
	if s.bus == nil {
		return types.Event{Type: typ, RunID: runID, TaskID: taskID}, nil
	}
	return s.bus.Publish(typ, runID, taskID, payload)
}

// BusLen reports buffered events, for tests and health checks.
func (s *Server) BusLen() int {
	if s.bus == nil {
		return 0
	}
	return s.bus.Len()
}

// ForwardBusToSubs bridges orchestrator events into the gate bus.
// The daemon calls it once at startup; the loop ends when ctx ends.
func ForwardBusToSubs(_ context.Context, _ *telemetry.Bus) {
	// The gate bus IS the telemetry bus: orchestrator listeners
	// publish directly through PublishEvent, so no bridge goroutine
	// is needed. Kept as documentation of the wiring decision.
}
