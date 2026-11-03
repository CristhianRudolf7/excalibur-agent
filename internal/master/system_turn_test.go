package master

import (
	"context"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"sync"
	"testing"
	"time"
)

type systemCaller struct{ calls chan Normalized }

func (s *systemCaller) CompleteTurn(_ context.Context, _ string, n Normalized) (string, []string, error) {
	s.calls <- n
	return "Prepared", nil, nil
}
func TestReceiveSystemStartsOnceAndKeepsOrigin(t *testing.T) {
	m := New(Config{}, nil, nil, nil, nil, nil, nil)
	defer m.Turns().Stop(context.Background())
	caller := &systemCaller{calls: make(chan Normalized, 2)}
	m.SetTurnCaller(caller)
	app := api.AppContext{App: "classroom", CourseID: "course", TopicID: "topic"}
	ctx := api.WithAppContext(context.Background(), app)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		safego.Go("system-turn-test", nil, func() {
			defer wg.Done()
			if _, err := m.ReceiveSystem(ctx, "event", "conversation", "Prepare a class"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	select {
	case msg := <-caller.calls:
		if msg.Role != "system" || msg.Channel != "system" || msg.Text != "Prepare a class" {
			t.Fatalf("%+v", msg)
		}
		if msg.Context == nil || *msg.Context != app {
			t.Fatalf("system context lost: %+v", msg.Context)
		}
	case <-time.After(time.Second):
		t.Fatal("turn not started")
	}
	select {
	case <-caller.calls:
		t.Fatal("duplicate turn")
	default:
	}
	rows := m.TurnHistory("conversation", 0)
	count := 0
	for _, row := range rows {
		if row.Role == "system" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("system history count %d", count)
	}
}

func TestReceiveSystemRejectsInvalidAppContext(t *testing.T) {
	m := New(Config{}, nil, nil, nil, nil, nil, nil)
	ctx := api.WithAppContext(context.Background(), api.AppContext{App: ""})
	if _, err := m.ReceiveSystem(ctx, "event", "conversation", "Prepare a class"); err == nil {
		t.Fatal("invalid system context accepted")
	}
	if len(m.TurnHistory("conversation", 0)) != 0 {
		t.Fatal("invalid system message persisted")
	}
}
