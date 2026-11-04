package robotlab

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProtocolHelper(t *testing.T) {
	if os.Getenv("EXCALIBUR_ROBOTLAB_HELPER") != "1" {
		return
	}
	checkChildSecrets()
	for name, want := range map[string]string{"MUJOCO_GL": mujocoGL(), "PYTHONUNBUFFERED": "1", "OMP_NUM_THREADS": "2", "MKL_NUM_THREADS": "2"} {
		if os.Getenv(name) != want {
			os.Exit(4)
		}
	}
	if _, err := os.Stat(filepath.Join("robotlab", "server.py")); err != nil {
		os.Exit(5)
	}
	enc := json.NewEncoder(os.Stdout)
	if os.Getenv("EXCALIBUR_ROBOTLAB_HELPER_NO_READY") != "1" {
		_ = enc.Encode(map[string]any{"event": "ready", "data": map[string]any{"version": "test"}})
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var q struct {
			ID     uint64 `json:"id"`
			Method string `json:"method"`
		}
		_ = json.Unmarshal(scan.Bytes(), &q)
		switch q.Method {
		case "crash_stderr":
			os.Stderr.WriteString(strings.Repeat("x", 20000) + "stderr-end")
			os.Exit(2)
		case "crash":
			os.Exit(2)
		case "timeout":
			continue
		case "error":
			_ = enc.Encode(map[string]any{"id": q.ID, "error": map[string]any{"code": "not_found", "message": "missing"}})
			continue
		}
		_ = enc.Encode(map[string]any{"event": "metrics", "data": map[string]any{"step": 1}})
		_ = enc.Encode(map[string]any{"id": q.ID, "result": map[string]any{"method": q.Method}})
	}
	os.Exit(0)
}
func fakeSidecar(t *testing.T) *ProcessSidecar {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("Unix runtime")
	}
	if !runtimeSupported() {
		t.Skip("Robot Lab runtime is unavailable on this platform")
	}
	t.Setenv("EXCALIBUR_ROBOTLAB", "1")
	s := NewSidecar(t.TempDir())
	s.RestartDelay = 10 * time.Millisecond
	s.Command = func(_, _, _ string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestProtocolHelper$")
		cmd.Env = nil
		return cmd
	}
	t.Setenv("EXCALIBUR_ROBOTLAB_HELPER", "1")
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func TestSidecarProtocol(t *testing.T) {
	s := fakeSidecar(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	events := make(chan Event, 8)
	unsub := s.Subscribe(func(e Event) { events <- e })
	defer unsub()
	var wg sync.WaitGroup
	for _, m := range []string{"one", "two", "three"} {
		wg.Add(1)
		method := m
		safelyCall := func() {
			defer wg.Done()
			raw, err := s.Call(ctx, method, map[string]any{})
			if err != nil {
				t.Error(err)
				return
			}
			var v map[string]string
			_ = json.Unmarshal(raw, &v)
			if v["method"] != method {
				t.Error("response mismatch")
			}
		} // Test helpers follow the same goroutine ownership rule.
		safego.Go("robotlab-test", nil, safelyCall)
	}
	wg.Wait()
	select {
	case <-events:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if st := s.Status(ctx); !st.Running || st.Version != "test" {
		t.Fatalf("status: %+v", st)
	}
	_, err := s.Call(ctx, "error", map[string]any{})
	var re *RuntimeError
	if !errors.As(err, &re) || re.Code != "not_found" {
		t.Fatalf("error: %v", err)
	}
	short, c := context.WithTimeout(ctx, 20*time.Millisecond)
	defer c()
	_, err = s.Call(short, "timeout", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
func TestSidecarCrashAndRestart(t *testing.T) {
	s := fakeSidecar(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for n := 0; n < 4; n++ {
		if _, err := s.Call(ctx, "ping", map[string]any{}); err != nil {
			t.Fatal(err)
		}
		_, err := s.Call(ctx, "crash", nil)
		if err == nil {
			t.Fatal("crash succeeded")
		}
		if n < 3 {
			deadline := time.Now().Add(4 * time.Second)
			for {
				st := s.Status(ctx)
				if st.Running {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("restart: %+v", st)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}
	_, err := s.Call(ctx, "ping", nil)
	if err == nil {
		t.Fatal("restart limit not enforced")
	}
}
func TestSlowSubscriberRetainsReliableEvents(t *testing.T) {
	blocked := make(chan struct{})
	started := make(chan struct{})
	got := make(chan Event, 256)
	sub := newEventSubscriber(func(e Event) {
		if e.Name == "block" {
			close(started)
			<-blocked
		}
		got <- e
	})
	defer sub.close()
	sub.enqueue(Event{Name: "block", Data: json.RawMessage(`{}`)})
	<-started
	for n := 0; n < 200; n++ {
		sub.enqueue(Event{Name: "frame", Data: json.RawMessage(`{}`)})
	}
	for n := 0; n < 150; n++ {
		b, _ := json.Marshal(n)
		sub.enqueue(Event{Name: "metrics", Data: b})
	}
	close(blocked)
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	count := 0
	frames := 0
	for count < 150 {
		select {
		case e := <-got:
			if e.Name == "metrics" {
				var n int
				_ = json.Unmarshal(e.Data, &n)
				if n != count {
					t.Fatalf("order: %d != %d", n, count)
				}
				count++
			}
			if e.Name == "frame" {
				frames++
			}
		case <-deadline.C:
			t.Fatal("reliable events lost")
		}
	}
	if frames >= 200 {
		t.Fatal("frames were not dropped")
	}
}

func TestSidecarStderrTailAndBackoff(t *testing.T) {
	s := fakeSidecar(t)
	s.RestartDelay = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.Call(ctx, "crash_stderr", nil); err == nil {
		t.Fatal("crash succeeded")
	}
	st := s.Status(ctx)
	if !strings.Contains(st.Error, "stderr-end") || len(st.Error) > 8500 {
		t.Fatalf("stderr tail length=%d", len(st.Error))
	}
	if _, err := s.Call(ctx, "ping", nil); err == nil {
		t.Fatal("call bypassed crash backoff")
	}
	s.mu.Lock()
	attempts := len(s.starts)
	s.mu.Unlock()
	if attempts != 1 {
		t.Fatal("extra process started during backoff")
	}
}
func TestSidecarWaitsForReady(t *testing.T) {
	t.Setenv("EXCALIBUR_ROBOTLAB_HELPER_NO_READY", "1")
	s := fakeSidecar(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := s.Call(ctx, "ping", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("readiness wait: %v", err)
	}
	if s.Status(context.Background()).Running {
		t.Fatal("reported ready before handshake")
	}
}
func TestRestartBudgetExpires(t *testing.T) {
	s := fakeSidecar(t)
	s.starts = []time.Time{time.Now(), time.Now(), time.Now(), time.Now()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.Call(ctx, "ping", nil); err == nil {
		t.Fatal("budget not enforced")
	}
	for n := range s.starts {
		s.starts[n] = time.Now().Add(-6 * time.Minute)
	}
	if _, err := s.Call(ctx, "ping", nil); err != nil {
		t.Fatalf("expired budget: %v", err)
	}
}

func TestSidecarRetriesTransientRestartSpawnFailure(t *testing.T) {
	s := fakeSidecar(t)
	factory := s.Command
	attempts := 0
	s.Command = func(python, source, data string) *exec.Cmd {
		attempts++
		if attempts == 2 {
			return exec.Command(filepath.Join(source, "missing-interpreter"))
		}
		return factory(python, source, data)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.Call(ctx, "crash", nil); err == nil {
		t.Fatal("crash succeeded")
	}
	deadline := time.Now().Add(3 * time.Second)
	for !s.Status(ctx).Running {
		if time.Now().After(deadline) {
			t.Fatalf("restart not retried: %+v", s.Status(ctx))
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.mu.Lock()
	count := attempts
	s.mu.Unlock()
	if count != 3 {
		t.Fatalf("spawn attempts = %d", count)
	}
}

func TestMissingInterpreterDoesNotExhaustCrashBudget(t *testing.T) {
	s := fakeSidecar(t)
	factory := s.Command
	s.Command = func(_, source, _ string) *exec.Cmd { return exec.Command(filepath.Join(source, "not-installed-yet")) }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for n := 0; n < 5; n++ {
		if _, err := s.Call(ctx, "ping", nil); err == nil {
			t.Fatal("missing interpreter succeeded")
		}
	}
	s.Command = factory
	if _, err := s.Call(ctx, "ping", nil); err != nil {
		t.Fatalf("runtime remained blocked after prerequisites were supplied: %v", err)
	}
}

func TestSidecarRejectsOversizedProtocolRequest(t *testing.T) {
	s := fakeSidecar(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.Call(ctx, "large", map[string]any{"data": strings.Repeat("x", 8<<20)})
	var runtimeError *RuntimeError
	if !errors.As(err, &runtimeError) || runtimeError.Code != "invalid_params" {
		t.Fatal("oversized request not rejected")
	}
	// A rejected request must leave framing and the running helper usable.
	if _, err = s.Call(ctx, "ping", nil); err != nil {
		t.Fatal(err)
	}
}
