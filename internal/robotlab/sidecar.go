package robotlab

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// RuntimeError preserves the runtime's stable error codes across transports.
type RuntimeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *RuntimeError) Error() string { return e.Message }

type wireReply struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *RuntimeError   `json:"error"`
	Event  string          `json:"event"`
	Data   json.RawMessage `json:"data"`
}
type processRun struct {
	cmd     *exec.Cmd
	in      io.WriteCloser
	ready   chan struct{}
	done    chan struct{}
	writes  chan []byte
	isReady bool
	pending map[uint64]chan wireReply
}

// ProcessSidecar supervises one lazy, multiplexed Python runtime.
type ProcessSidecar struct {
	nextStart time.Time
	mu        sync.Mutex
	data      string
	python    string
	run       *processRun
	closed    bool
	next      uint64
	starts    []time.Time
	err       string
	version   string
	stderr    []byte
	subs      map[*eventSubscriber]struct{}
	stop      chan struct{}
	// Command is an optional command factory for protocol tests. Set before use.
	Command func(python, source, data string) *exec.Cmd
	// RestartDelay defaults to one second, exponentially increasing after crashes.
	RestartDelay time.Duration
}

func NewSidecar(dataDir string) *ProcessSidecar {
	if absolute, err := filepath.Abs(dataDir); err == nil {
		dataDir = absolute
	}
	python := os.Getenv("EXCALIBUR_ROBOTLAB_PYTHON")
	if python != "" {
		if resolved, err := exec.LookPath(python); err == nil {
			python = resolved
		}
	}
	if python == "" {
		python = filepath.Join(dataDir, "robotlab", "venv", "bin", "python")
	}
	return &ProcessSidecar{data: dataDir, python: python, subs: map[*eventSubscriber]struct{}{}, stop: make(chan struct{})}
}

func (s *ProcessSidecar) Status(context.Context) Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := os.Stat(s.python)
	if err == nil && os.Getenv("EXCALIBUR_ROBOTLAB_PYTHON") == "" {
		_, err = os.Stat(filepath.Join(s.data, "robotlab", "venv", ".robotlab-installed"))
	}
	st := Status{Enabled: Enabled(), Installed: err == nil, Python: s.python, Version: s.version, Running: s.run != nil && s.run.isReady, Requirements: Requirements{Python: ">=3.10", OSMesa: detectOSMesa()}, Error: s.err}
	if len(s.stderr) > 0 && st.Error != "" {
		st.Error += "\n" + string(s.stderr)
	}
	if runtime.GOOS != "linux" {
		st.Error = errors.ErrUnsupported.Error()
		st.Running = false
	}
	return st
}

func (s *ProcessSidecar) startLocked() (*processRun, error) {
	if !Enabled() {
		return nil, &RuntimeError{"unavailable", "Robot Lab is disabled"}
	}
	if runtime.GOOS != "linux" {
		return nil, errors.ErrUnsupported
	}
	if s.closed {
		return nil, &RuntimeError{"unavailable", "Robot Lab is closed"}
	}
	if s.run != nil {
		return s.run, nil
	}
	now := time.Now()
	if now.Before(s.nextStart) {
		return nil, &RuntimeError{"unavailable", "runtime restarting after crash"}
	}
	recent := s.starts[:0]
	for _, t := range s.starts {
		if now.Sub(t) < 5*time.Minute {
			recent = append(recent, t)
		}
	}
	s.starts = recent
	if len(s.starts) >= 4 {
		s.err = "runtime restart limit exceeded (3 restarts in 5 minutes)"
		return nil, &RuntimeError{"unavailable", s.err}
	}
	source, err := ExtractRuntime(s.data)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(s.python, "-m", "robotlab.server", "--data", filepath.Join(s.data, "robotlab"))
	if s.Command != nil {
		cmd = s.Command(s.python, source, filepath.Join(s.data, "robotlab"))
	}
	cmd.Dir = source
	cmd.Env = childEnvironment(false)
	group := sidecarProcessGroup()
	group.Configure(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	cmd.Stderr = sidecarStderr{s}
	// Missing prerequisites before the first successful start are not crashes.
	// Once running, failed restart spawns consume the same bounded budget.
	restarting := len(s.starts) > 0
	if restarting {
		s.starts = append(s.starts, now)
	}
	if err = cmd.Start(); err != nil {
		in.Close()
		out.Close()
		s.err = err.Error()
		return nil, &RuntimeError{"unavailable", s.err}
	}
	if !restarting {
		s.starts = append(s.starts, now)
	}
	_ = lowerSidecarPriority(cmd.Process.Pid)
	r := &processRun{cmd: cmd, in: in, ready: make(chan struct{}), done: make(chan struct{}), writes: make(chan []byte, 64), pending: map[uint64]chan wireReply{}}
	s.run = r
	s.stderr = nil
	s.err = ""
	safego.Go("robotlab-write", nil, func() {
		for {
			select {
			case <-r.done:
				return
			case b := <-r.writes:
				if _, err := in.Write(b); err != nil {
					return
				}
			}
		}
	})
	safego.Go("robotlab-supervisor", nil, func() {
		scan := bufio.NewScanner(out)
		scan.Buffer(make([]byte, 4096), 16<<20)
		for scan.Scan() {
			var v wireReply
			if err := json.Unmarshal(scan.Bytes(), &v); err != nil {
				continue
			}
			if v.Event != "" {
				if v.Event == "ready" {
					s.mu.Lock()
					if !r.isReady {
						r.isReady = true
						var data struct {
							Version string `json:"version"`
						}
						_ = json.Unmarshal(v.Data, &data)
						s.version = data.Version
						close(r.ready)
					}
					s.mu.Unlock()
				} else {
					s.emit(Event{Name: v.Event, Data: append(json.RawMessage(nil), v.Data...)})
				}
				continue
			}
			s.mu.Lock()
			ch := r.pending[v.ID]
			delete(r.pending, v.ID)
			s.mu.Unlock()
			if ch != nil {
				ch <- v
			}
		}
		// Reap concurrently with group shutdown so an exited leader is not
		// left a zombie for the platform's entire termination grace period.
		waited := make(chan error, 1)
		safego.Go("robotlab-reap", nil, func() { waited <- cmd.Wait() })
		_ = group.Kill(cmd.Process.Pid)
		waitErr := <-waited
		in.Close()
		s.mu.Lock()
		if s.run == r {
			s.run = nil
		}
		s.err = fmt.Sprintf("runtime exited: %v", waitErr)
		close(r.done)
		closed := s.closed
		attempt := len(s.starts) - 1
		delay := s.RestartDelay
		if delay <= 0 {
			delay = time.Second
		}
		if attempt > 3 {
			attempt = 3
		}
		delay *= time.Duration(1 << attempt)
		s.nextStart = time.Now().Add(delay)
		s.mu.Unlock()
		if closed {
			return
		}
		// A transient spawn failure also consumes a restart attempt. Keep
		// retrying within the same budget instead of abandoning supervision.
		for retry := 0; retry < 3; retry++ {
			timer := time.NewTimer(delay)
			select {
			case <-s.stop:
				timer.Stop()
				return
			case <-timer.C:
			}
			s.mu.Lock()
			_, err := s.startLocked()
			if err == nil || s.closed || !Enabled() || len(s.starts) >= 4 {
				s.mu.Unlock()
				return
			}
			s.err = err.Error()
			delay *= 2
			s.nextStart = time.Now().Add(delay)
			s.mu.Unlock()
		}
	})
	// A runtime which never becomes ready must not leave calls or shutdown hanging.
	safego.Go("robotlab-ready-deadline", nil, func() {
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		select {
		case <-r.ready:
		case <-r.done:
		case <-timer.C:
			_ = group.Kill(cmd.Process.Pid)
		}
	})
	return r, nil
}

func (s *ProcessSidecar) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	r, err := s.startLocked()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case <-r.ready:
	case <-r.done:
		return nil, &RuntimeError{"unavailable", "runtime exited before ready"}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	s.mu.Lock()
	s.next++
	id := s.next
	ch := make(chan wireReply, 1)
	r.pending[id] = ch
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(r.pending, id); s.mu.Unlock() }()
	b, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	b = append(b, '\n')
	select {
	case r.writes <- b:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.done:
		return nil, &RuntimeError{"unavailable", "runtime exited"}
	}
	select {
	case v := <-ch:
		if v.Error != nil {
			return nil, v.Error
		}
		return v.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.done:
		return nil, &RuntimeError{"unavailable", "runtime exited"}
	}
}

// Close stops the whole process group and all subscription workers.
func (s *ProcessSidecar) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.stop)
	r := s.run
	subs := s.subs
	s.subs = map[*eventSubscriber]struct{}{}
	s.mu.Unlock()
	for sub := range subs {
		sub.close()
	}
	if r != nil {
		err := sidecarProcessGroup().Kill(r.cmd.Process.Pid)
		<-r.done
		return err
	}
	return nil
}
func (s *ProcessSidecar) emit(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sub := range s.subs {
		sub.enqueue(e)
	}
}
func (s *ProcessSidecar) Subscribe(fn func(Event)) func() {
	sub := newEventSubscriber(fn)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		sub.close()
		return func() {}
	}
	s.subs[sub] = struct{}{}
	s.mu.Unlock()
	return func() { s.mu.Lock(); delete(s.subs, sub); s.mu.Unlock(); sub.close() }
}

// sidecarStderr is consumed by os/exec's joined copy worker. Wait flushes the
// final tail before the supervisor publishes an exit status.
type sidecarStderr struct{ owner *ProcessSidecar }

func (w sidecarStderr) Write(p []byte) (int, error) {
	s := w.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stderr = append(s.stderr, p...)
	if len(s.stderr) > 8192 {
		s.stderr = append([]byte(nil), s.stderr[len(s.stderr)-8192:]...)
	}
	return len(p), nil
}
