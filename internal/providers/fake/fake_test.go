package fake

import (
	"context"
	"errors"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

func TestFakeProviderChat(t *testing.T) {
	f := New("test")
	var _ providers.Provider = f
	f.Responses = []providers.ChatResponse{
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "hi"}, Model: "m"},
	}
	resp, err := f.Chat(context.Background(), providers.ChatRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message.Content != "hi" || len(f.ChatRequests) != 1 {
		t.Fatal("chat script not consumed")
	}
}

func TestFakeProviderStream(t *testing.T) {
	f := New("test")
	f.Streams = [][]providers.StreamEvent{
		{{Delta: "a"}, {Delta: "b", Done: true}},
	}
	var got []string
	err := f.Stream(context.Background(), providers.ChatRequest{}, func(ev providers.StreamEvent) error {
		got = append(got, ev.Delta)
		return nil
	})
	if err != nil || len(got) != 2 || len(f.Emitted) != 1 {
		t.Fatalf("stream: %v %v", err, got)
	}
	emitErr := errors.New("stop")
	f.Streams = [][]providers.StreamEvent{{{Delta: "c"}}}
	if err := f.Stream(context.Background(), providers.ChatRequest{}, func(providers.StreamEvent) error { return emitErr }); err != emitErr {
		t.Fatal("emit error should abort the stream")
	}
}

func TestFakeProviderError(t *testing.T) {
	f := New("test")
	f.Err = &providers.ProviderError{Kind: providers.KindRateLimited}
	if _, err := f.Chat(context.Background(), providers.ChatRequest{}); err != f.Err {
		t.Fatal("expected scripted error")
	}
	if err := f.Stream(context.Background(), providers.ChatRequest{}, func(providers.StreamEvent) error { return nil }); err != f.Err {
		t.Fatal("expected scripted stream error")
	}
	if err := f.Close(); err != nil || !f.Closed {
		t.Fatal("close")
	}
}
