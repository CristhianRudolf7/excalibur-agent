package fake

import (
	"context"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// FakeProvider is a scripted providers.Provider. Calls consume
// Responses and Streams in order; when the scripts run out, the zero
// ChatResponse (or no events) is returned. Set Err to fail every call.
type FakeProvider struct {
	mu        sync.Mutex
	name      string
	Responses []providers.ChatResponse
	Streams   [][]providers.StreamEvent
	Err       error

	ChatRequests   []providers.ChatRequest
	StreamRequests []providers.ChatRequest
	Emitted        [][]providers.StreamEvent
	Closed         bool
}

// New returns a FakeProvider named name.
func New(name string) *FakeProvider { return &FakeProvider{name: name} }

// Name implements providers.Provider.
func (f *FakeProvider) Name() string { return f.name }

// Chat implements providers.Provider.
func (f *FakeProvider) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ChatRequests = append(f.ChatRequests, req)
	if f.Err != nil {
		return providers.ChatResponse{}, f.Err
	}
	if len(f.Responses) == 0 {
		return providers.ChatResponse{}, nil
	}
	resp := f.Responses[0]
	f.Responses = f.Responses[1:]
	return resp, nil
}

// Stream implements providers.Provider.
func (f *FakeProvider) Stream(_ context.Context, req providers.ChatRequest, emit func(providers.StreamEvent) error) error {
	f.mu.Lock()
	f.StreamRequests = append(f.StreamRequests, req)
	var events []providers.StreamEvent
	if len(f.Streams) > 0 {
		events = f.Streams[0]
		f.Streams = f.Streams[1:]
	}
	err := f.Err
	f.mu.Unlock()
	if err != nil {
		return err
	}
	var out []providers.StreamEvent
	for _, ev := range events {
		out = append(out, ev)
		if err := emit(ev); err != nil {
			return err
		}
	}
	f.mu.Lock()
	f.Emitted = append(f.Emitted, out)
	f.mu.Unlock()
	return nil
}

// Close implements providers.Provider.
func (f *FakeProvider) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Closed = true
	return nil
}
