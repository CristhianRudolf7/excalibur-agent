package master

import (
	"context"
	"testing"
	"time"
)

func TestReceiveDeferredReturnsBeforeSlowTriage(t *testing.T) {
	release := make(chan struct{})
	m := New(Config{Turns: DefaultTurnConfig()}, NewMemStore(), NewLexicon(), slowDecider{release}, nil, nil, nil)
	start := time.Now()
	id, err := m.ReceiveDeferred(context.Background(), "c", "m1", "cnv", "hola", nil, nil)
	if err != nil || id == "" {
		t.Fatal(id, err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("receipt waited for triage: %s", time.Since(start))
	}
	again, _ := m.ReceiveDeferred(context.Background(), "c", "m1", "cnv", "hola", nil, nil)
	if again != id {
		t.Fatalf("duplicate client_msg_id got %s want %s", again, id)
	}
	close(release)
}

type slowDecider struct{ release chan struct{} }

func (d slowDecider) Classify(ctx context.Context, _ string, _ Preanalysis) Triage {
	select {
	case <-d.release:
	case <-ctx.Done():
	}
	return Triage{}
}
