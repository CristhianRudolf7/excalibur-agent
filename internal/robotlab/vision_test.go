package robotlab

import (
	"context"
	"testing"
)

func TestCompatibilityVisionHookHasNoSideEffects(t *testing.T) {
	sidecar := &toolSidecarFake{}
	publisher := &fakePublisher{}
	var hook func(context.Context, string, string) (any, error)
	tool := NewTool(sidecar, publisher, nil, &hook)
	if tool.Vision == nil || hook == nil {
		t.Fatal("missing compatibility hook")
	}
	if result, err := hook(context.Background(), "s", "look"); err != nil || result != nil {
		t.Fatalf("compatibility hook returned %v, %v", result, err)
	}
	if len(sidecar.calls) != 0 || len(publisher.names) != 0 {
		t.Fatal("retired vision operation had side effects")
	}
}
