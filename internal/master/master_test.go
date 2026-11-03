package master

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
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

func TestPlainMessagesReachTurnWithoutTriage(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		for _, text := range []string{"Hola", "Hello", "¿Cuál es la capital de Perú?", "Explain the capital of Peru", "baja la pinza 5 cm", "move the gripper down 5 cm"} {
			t.Run(fmt.Sprintf("deferred=%t/%s", deferred, text), func(t *testing.T) {
				t.Setenv("EXCALIBUR_INTAKE_GATED_TRIAGE", "true")
				dec := &FakeDecider{}
				m := New(Config{}, nil, nil, dec, nil, nil, nil)
				seen := make(contextTurnRecorder, 1)
				m.SetTurnCaller(seen)
				t.Cleanup(func() {
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					_ = m.Turns().Stop(ctx)
				})
				if deferred {
					_, err := m.ReceiveDeferred(context.Background(), "client", "message", "conversation", text, nil, nil)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					_, out, err := m.Receive(context.Background(), "client", "message", "conversation", text)
					if err != nil || out.Handled {
						t.Fatalf("receive: %+v %v", out, err)
					}
				}
				select {
				case got := <-seen:
					if got.Text != text || dec.Calls != 0 {
						t.Fatalf("turn=%+v, decider calls=%d", got, dec.Calls)
					}
				case <-time.After(time.Second):
					t.Fatal("plain message did not reach turn")
				}
			})
		}
	}
}

func TestTimedOutIntakeStillReachesTurn(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		t.Run(fmt.Sprint(deferred), func(t *testing.T) {
			t.Setenv("EXCALIBUR_INTAKE_DECIDER_TIMEOUT", "20ms")
			release := make(chan struct{})
			defer close(release)
			m := New(Config{}, nil, nil, slowDecider{release}, nil, nil, nil)
			seen := make(contextTurnRecorder, 1)
			m.SetTurnCaller(seen)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = m.Turns().Stop(ctx)
			})
			text := "pause R3"
			if deferred {
				if _, err := m.ReceiveDeferred(context.Background(), "c", "m", "conv", text, nil, nil); err != nil {
					t.Fatal(err)
				}
			} else if _, out, err := m.Receive(context.Background(), "c", "m", "conv", text); err != nil || out.Handled {
				t.Fatalf("timeout received: %+v %v", out, err)
			}
			select {
			case got := <-seen:
				if got.Text != text {
					t.Fatalf("lost timed out message: %+v", got)
				}
			case <-time.After(time.Second):
				t.Fatal("timeout did not reach turn")
			}
		})
	}
}

func TestIntakeTimingLoggedOncePerMessage(t *testing.T) {
	old := slog.Default()
	defer slog.SetDefault(old)
	for _, deferred := range []bool{false, true} {
		for _, tc := range []struct {
			text     string
			gated    bool
			calls    int
			fastLane bool
			timeout  bool
		}{
			{"Hello private message", true, 0, false, false},
			{"pause run_1", false, 1, true, false},
			{"/status run_1", false, 0, true, false},
			{"pause run_1 and explain this", false, 1, true, false},
			{"pause R3 private message", false, 1, false, true},
		} {
			t.Run(fmt.Sprintf("deferred=%t/%s", deferred, tc.text), func(t *testing.T) {
				t.Setenv("EXCALIBUR_INTAKE_TIMING", "true")
				t.Setenv("EXCALIBUR_INTAKE_GATED_TRIAGE", "true")
				var log lockedIntakeLog
				slog.SetDefault(slog.New(slog.NewJSONHandler(&log, nil)))
				controls := newFakeControls()
				controls.seed("run_1", "worker", types.RunRunning)
				var dec Decider = &FakeDecider{Fn: func(string, Preanalysis) Triage { return Triage{Type: TriageQuickAction, Confidence: .99} }}
				if tc.timeout {
					t.Setenv("EXCALIBUR_INTAKE_DECIDER_TIMEOUT", "20ms")
					release := make(chan struct{})
					defer close(release)
					dec = slowDecider{release}
				}
				m := New(Config{}, nil, nil, dec, controls, nil, nil)
				seen := make(contextTurnRecorder, 1)
				m.SetTurnCaller(seen)
				t.Cleanup(func() {
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					_ = m.Turns().Stop(ctx)
				})
				if deferred {
					replied := make(chan string, 1)
					if _, err := m.ReceiveDeferred(context.Background(), "c", "m", "conv", tc.text, nil, func(reply string) { replied <- reply }); err != nil {
						t.Fatal(err)
					}
					select {
					case <-replied:
					case <-seen:
					case <-time.After(time.Second):
						t.Fatal("message was not routed")
					}
				} else if _, _, err := m.Receive(context.Background(), "c", "m", "conv", tc.text); err != nil {
					t.Fatal(err)
				}
				lines := strings.Split(strings.TrimSpace(log.String()), "\n")
				var timings []map[string]any
				for _, line := range lines {
					var record map[string]any
					if err := json.Unmarshal([]byte(line), &record); err != nil {
						t.Fatal(err)
					}
					if record["msg"] == "intake timing" {
						timings = append(timings, record)
					}
				}
				if len(timings) != 1 {
					t.Fatalf("want one timing record, got %s", log.String())
				}
				r := timings[0]
				if r["level"] != "INFO" || r["gated"] != tc.gated || r["decider_calls"] != float64(tc.calls) || r["fast_lane"] != tc.fastLane {
					t.Fatalf("timing=%+v", r)
				}
				ms, ok := r["decider_ms"].(float64)
				if !ok || ms < 0 || (tc.calls == 0 && ms != 0) {
					t.Fatalf("decider time=%v", r["decider_ms"])
				}
				if tc.timeout && ms < 10 {
					t.Fatalf("timeout not measured: %f ms", ms)
				}
				// The structured event has exactly the four timing fields.
				if len(r) != 7 || strings.Contains(log.String(), tc.text) {
					t.Fatalf("unexpected intake log data: %s", log.String())
				}
			})
		}
	}
}

// Log writes may race with deferred intake or turn completion.
type lockedIntakeLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedIntakeLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *lockedIntakeLog) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buf.String() }

func TestIntakeTimingSwitch(t *testing.T) {
	t.Setenv("EXCALIBUR_INTAKE_TIMING", "false")
	old := slog.Default()
	defer slog.SetDefault(old)
	var log lockedIntakeLog
	slog.SetDefault(slog.New(slog.NewJSONHandler(&log, nil)))
	lane := &FastLane{}
	lane.Handle(context.Background(), fastIntake("Hello", nil, nil))
	if log.String() != "" {
		t.Fatalf("timing disabled: %s", log.String())
	}
}
