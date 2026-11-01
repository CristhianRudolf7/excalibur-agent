package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// Terminal limits from Section 13.5 and Appendix B.
const (
	maxSessionsPerOwner = 16
	maxSessionsGlobal   = 64
	protectedSessions   = 8

	defaultYieldMs = 10000
	minYieldMs     = 250
	maxYieldMs     = 30000

	termKillMargin = 2 * time.Second

	// LiveTailLines and LiveTailBytes bound the status live queue
	// (Section 15.6): last 20 lines up to 2 KB.
	LiveTailLines = 20
	LiveTailBytes = 2 * 1024
)

// TerminalDeps wires platform interfaces into the session manager.
// Nil entries degrade gracefully for tests and embedded use.
type TerminalDeps struct {
	Group    platform.ProcessGroup
	Priority platform.ProcessPriority
	Isolator platform.Isolator
	PTY      platform.PTY
	OnEvict  func(sessionID SessionID, owner, cmd string)
}

// SessionID identifies a terminal session.
type SessionID string

// DetachBackgroundSwitch gates run-end service survival (W10):
// background exec sessions detach into their own session (setsid,
// stdin from /dev/null, output to a log file under /tmp) so they
// survive the agent's exit. Default ON; off restores the
// pre-detach behaviour where every child dies with the parent.
const DetachBackgroundSwitch = "EXCALIBUR_DETACH_BACKGROUND"

// DetachBackgroundEnabled reports whether background sessions detach.
func DetachBackgroundEnabled() bool {
	v, ok := os.LookupEnv(DetachBackgroundSwitch)
	if !ok || len(strings.TrimSpace(v)) == 0 {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "off", "false", "no":
		return false
	default:
		return true
	}
}

// SurvivingProcess is one detached background process that outlives
// the run, recorded in the trial records (services.json).
type SurvivingProcess struct {
	Session SessionID `json:"session"`
	Pid     int       `json:"pid"`
	Command string    `json:"command"`
	LogPath string    `json:"log_path"`
	Started time.Time `json:"started"`
	Alive   bool      `json:"alive"`
}

// Session is one live process behind exec/exec_io.
type Session struct {
	ID      SessionID
	Owner   string
	Cmd     string
	Dir     string
	UsePTY  bool
	Started time.Time

	mu       sync.Mutex
	buf      bytes.Buffer
	full     bytes.Buffer
	done     chan struct{}
	exitErr  error
	exited   bool
	lastUse  time.Time
	pty      platform.PTYSession
	proc     *os.Process
	Detached bool
	LogPath  string
	Pid      int
}

func (s *Session) appendOut(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf.Write(p)
	s.full.Write(p)
	s.lastUse = time.Now()
	// Bound the live buffer to MaxBufferedOutput head+tail style: keep
	// first and last quarters when over budget.
	if int64(s.buf.Len()) > MaxBufferedOutput {
		data := s.buf.Bytes()
		half := int(MaxBufferedOutput) / 2
		kept := append([]byte(nil), data[:half]...)
		kept = append(kept, data[len(data)-half:]...)
		s.buf.Reset()
		s.buf.Write(kept)
	}
}

func (s *Session) markDone(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.exited {
		s.exited = true
		s.exitErr = err
		s.lastUse = time.Now()
	}
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

func (s *Session) snapshot() (alive bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.exited
}

// Exit reports the process exit code when the session finished:
// (code, true) after completion, (0, false) while still running.
// Start failures surface as code 127 with the start error in the
// output so callers can render exit/duration up front.
func (s *Session) Exit() (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.exited {
		return 0, false
	}
	if s.exitErr == nil {
		return 0, true
	}
	if ee, ok := s.exitErr.(*exec.ExitError); ok {
		return ee.ExitCode(), true
	}
	if strings.Contains(s.exitErr.Error(), "start error") {
		return 127, true
	}
	return 1, true
}

// SnapshotFull returns the full buffered output for header
// accounting (offsets shown to the model by exec/exec_io).
func (s *Session) SnapshotFull() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.full.String()
}

// ReadSince returns output after offset with the new offset.
func (s *Session) ReadSince(offset int) (string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.full.Bytes()
	if offset < 0 {
		offset = 0
	}
	if offset > len(data) {
		offset = len(data)
	}
	return string(data[offset:]), len(data)
}

// LiveTail returns the last lines bounded for status.
func (s *Session) LiveTail() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.full.Bytes()
	if len(data) > LiveTailBytes*4 {
		data = data[len(data)-LiveTailBytes*4:]
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > LiveTailLines {
		lines = lines[len(lines)-LiveTailLines:]
	}
	out := strings.Join(lines, "\n")
	if len(out) > LiveTailBytes {
		out = out[len(out)-LiveTailBytes:]
	}
	return out
}

// SessionManager keeps processes alive between interactions.
type SessionManager struct {
	mu       sync.Mutex
	sessions map[SessionID]*Session
	order    []SessionID
	seq      uint64
	deps     TerminalDeps
	now      func() time.Time
}

// NewSessionManager returns a manager with the given platform deps.
func NewSessionManager(deps TerminalDeps) *SessionManager {
	return &SessionManager{sessions: map[SessionID]*Session{}, deps: deps, now: time.Now}
}

// Start launches cmd for owner and waits up to yieldMs.
func (m *SessionManager) Start(owner, cmd, dir string, usePTY bool, yieldMs int, env []string) (*Session, string, bool) {
	return m.StartContext(context.Background(), owner, cmd, dir, usePTY, yieldMs, env)
}

// StartContext starts a session like Start, but cancellation releases the
// initial wait so the run can write its final records promptly.
func (m *SessionManager) StartContext(ctx context.Context, owner, cmd, dir string, usePTY bool, yieldMs int, env []string) (*Session, string, bool) {
	if yieldMs < minYieldMs {
		yieldMs = minYieldMs
	}
	if yieldMs > maxYieldMs {
		yieldMs = maxYieldMs
	}
	m.evictIfNeeded(owner)
	s := &Session{
		ID: SessionID(fmt.Sprintf("sess-%d", m.nextSeq())), Owner: owner,
		Cmd: cmd, Dir: dir, UsePTY: usePTY, Started: m.now(), done: make(chan struct{}),
	}
	s.lastUse = s.Started
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.order = append(m.order, s.ID)
	m.mu.Unlock()

	if usePTY && m.deps.PTY != nil {
		m.startPTY(s, cmd, dir, env)
	} else {
		m.startPipe(s, cmd, dir, env)
	}
	out, alive := s.waitYield(ctx, time.Duration(yieldMs)*time.Millisecond)
	return s, out, alive
}

func (m *SessionManager) nextSeq() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	return m.seq
}

func (m *SessionManager) startPipe(s *Session, cmd, dir string, env []string) {
	if err := guardSignals(cmd); err != nil {
		s.appendOut([]byte(err.Error()))
		s.markDone(err)
		return
	}
	shell := DetectShell()
	c := exec.Command(shell, "-c", cmd)
	if dir != "" {
		c.Dir = dir
	}
	c.Env = pipeEnv(env)
	c.SysProcAttr = sysProcAttr()
	if m.deps.Group != nil {
		m.deps.Group.Configure(c)
	}
	if m.deps.Isolator != nil {
		_ = m.deps.Isolator.Isolate(c)
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		s.appendOut([]byte("start error: " + err.Error()))
		s.markDone(err)
		return
	}
	c.Stderr = c.Stdout
	if err := c.Start(); err != nil {
		s.appendOut([]byte("start error: " + err.Error()))
		s.markDone(err)
		return
	}
	s.mu.Lock()
	s.proc = c.Process
	if m.deps.Priority != nil && c.Process != nil {
		_ = m.deps.Priority.LowerProcess(c.Process.Pid)
	}
	s.mu.Unlock()
	safego.Go("tools/terminal-pipe", nil, func() {
		buf := make([]byte, 8192)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				s.appendOut(buf[:n])
			}
			if err != nil {
				break
			}
		}
		s.markDone(c.Wait())
	})
}

func (m *SessionManager) startPTY(s *Session, cmd, dir string, env []string) {
	if err := guardSignals(cmd); err != nil {
		s.appendOut([]byte(err.Error()))
		s.markDone(err)
		return
	}
	shell := DetectShell()
	c := exec.Command(shell, "-c", cmd)
	if dir != "" {
		c.Dir = dir
	}
	c.Env = ptyEnv(env)
	c.SysProcAttr = sysProcAttr()
	if m.deps.Group != nil {
		m.deps.Group.Configure(c)
	}
	if m.deps.Isolator != nil {
		_ = m.deps.Isolator.Isolate(c)
	}
	sess, err := m.deps.PTY.Start(c, platform.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		s.appendOut([]byte("pty start error: " + err.Error()))
		s.markDone(err)
		return
	}
	s.mu.Lock()
	s.pty = sess
	if c.Process != nil {
		s.proc = c.Process
	}
	if m.deps.Priority != nil && s.proc != nil {
		_ = m.deps.Priority.LowerProcess(s.proc.Pid)
	}
	s.mu.Unlock()
	safego.Go("tools/terminal-pty", nil, func() {
		buf := make([]byte, 8192)
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				s.appendOut(buf[:n])
			}
			if err != nil {
				break
			}
		}
		s.markDone(sess.Wait())
	})
}

// StartDetached launches cmd as a run-end service (W10): the child
// is detached into its own session (setsid, no Pdeathsig), stdin
// from /dev/null, stdout+stderr to a log file under /tmp, so it
// survives the agent's exit. The manager tails the log for polling
// during the run; after the run the process keeps running and the
// log file remains. PTY sessions never detach: they fall back to
// the foreground path so interactive behaviour is unchanged.
func (m *SessionManager) StartDetached(owner, cmd, dir string, env []string) (*Session, string, bool) {
	m.evictIfNeeded(owner)
	s := &Session{
		ID: SessionID(fmt.Sprintf("sess-%d", m.nextSeq())), Owner: owner,
		Cmd: cmd, Dir: dir, Started: m.now(), done: make(chan struct{}),
		Detached: true,
	}
	s.lastUse = s.Started
	logPath := filepath.Join(os.TempDir(), fmt.Sprintf("excalibur-%s-%d.log", string(s.ID), s.Started.UnixNano()))
	s.LogPath = logPath
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.order = append(m.order, s.ID)
	m.mu.Unlock()

	if runtime.GOOS != "linux" || !DetachBackgroundEnabled() {
		// Stubs and opt-out keep the foreground path byte-for-byte.
		s.Detached = false
		s.LogPath = ""
		m.startPipe(s, cmd, dir, env)
		out, alive := s.waitYield(context.Background(), time.Duration(minYieldMs)*time.Millisecond)
		return s, out, alive
	}
	m.startDetachedPipe(s, cmd, dir, env, logPath)
	out, alive := s.waitYield(context.Background(), time.Duration(minYieldMs)*time.Millisecond)
	return s, out, alive
}

func (m *SessionManager) startDetachedPipe(s *Session, cmd, dir string, env []string, logPath string) {
	if err := guardSignals(cmd); err != nil {
		s.appendOut([]byte(err.Error()))
		s.markDone(err)
		return
	}
	shell := DetectShell()
	c := exec.Command(shell, "-c", cmd)
	if dir != "" {
		c.Dir = dir
	}
	c.Env = pipeEnv(env)
	c.SysProcAttr = detachedSysProcAttr()
	// Never arm Pdeathsig or group-kill for detached services: the
	// point is survival after the agent exits. Isolator and priority
	// still apply when present.
	if m.deps.Isolator != nil {
		_ = m.deps.Isolator.Isolate(c)
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		s.appendOut([]byte("start error: open devnull: " + err.Error()))
		s.markDone(err)
		return
	}
	defer devNull.Close()
	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		s.appendOut([]byte("start error: open log: " + err.Error()))
		s.markDone(err)
		return
	}
	c.Stdin = devNull
	c.Stdout = logF
	c.Stderr = logF
	if err := c.Start(); err != nil {
		_ = logF.Close()
		s.appendOut([]byte("start error: " + err.Error()))
		s.markDone(err)
		return
	}
	s.mu.Lock()
	s.proc = c.Process
	s.Pid = c.Process.Pid
	if m.deps.Priority != nil && c.Process != nil {
		_ = m.deps.Priority.LowerProcess(c.Process.Pid)
	}
	s.mu.Unlock()
	_ = logF.Close()
	s.appendOut([]byte(fmt.Sprintf("detached service pid %d log %s\n", s.Pid, logPath)))
	safego.Go("tools/terminal-detached-wait", nil, func() {
		s.markDone(c.Wait())
	})
	safego.Go("tools/terminal-detached-tail", nil, func() {
		var off int64
		buf := make([]byte, 8192)
		for {
			f, err := os.Open(logPath)
			if err != nil {
				select {
				case <-s.done:
					return
				case <-time.After(100 * time.Millisecond):
					continue
				}
			}
			if off > 0 {
				_, _ = f.Seek(off, io.SeekStart)
			}
			n, _ := f.Read(buf)
			if n > 0 {
				s.appendOut(buf[:n])
				off += int64(n)
			}
			_ = f.Close()
			select {
			case <-s.done:
				// Drain once more after exit so the final
				// output is visible before polling stops.
				if f2, err := os.Open(logPath); err == nil {
					if off > 0 {
						_, _ = f2.Seek(off, io.SeekStart)
					}
					if n2, _ := f2.Read(buf); n2 > 0 {
						s.appendOut(buf[:n2])
					}
					_ = f2.Close()
				}
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	})
}

// Surviving returns the detached background services that are still
// alive (or were detached and have a log), for the trial records.
func (m *SessionManager) Surviving() []SurvivingProcess {
	m.mu.Lock()
	snaps := make([]*Session, 0, len(m.sessions))
	for _, sid := range m.order {
		if s, ok := m.sessions[sid]; ok {
			snaps = append(snaps, s)
		}
	}
	m.mu.Unlock()
	var out []SurvivingProcess
	for _, s := range snaps {
		s.mu.Lock()
		det := s.Detached
		pid := s.Pid
		if pid == 0 && s.proc != nil {
			pid = s.proc.Pid
		}
		alive := !s.exited
		logPath := s.LogPath
		cmd := s.Cmd
		started := s.Started
		id := s.ID
		s.mu.Unlock()
		if !det {
			continue
		}
		out = append(out, SurvivingProcess{
			Session: id, Pid: pid, Command: cmd,
			LogPath: logPath, Started: started, Alive: alive,
		})
	}
	return out
}

func (s *Session) waitYield(ctx context.Context, d time.Duration) (string, bool) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		out, _ := s.ReadSince(0)
		return out, s.snapshot()
	case <-s.done:
		out, _ := s.ReadSince(0)
		return out, false
	case <-timer.C:
		out, _ := s.ReadSince(0)
		return out, s.snapshot()
	}
}

// Get returns the session by id.
func (m *SessionManager) Get(id SessionID) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if ok {
		s.mu.Lock()
		s.lastUse = time.Now()
		s.mu.Unlock()
		// Refresh LRU order.
		for i, sid := range m.order {
			if sid == id {
				m.order = append(m.order[:i], m.order[i+1:]...)
				m.order = append(m.order, id)
				break
			}
		}
	}
	return s, ok
}

// List returns session summaries for owner (empty owner lists all).
func (m *SessionManager) List(owner string) []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Session
	for _, sid := range m.order {
		s := m.sessions[sid]
		if owner == "" || s.Owner == owner {
			out = append(out, s)
		}
	}
	return out
}

// Write sends text to the session stdin/pty.
func (m *SessionManager) Write(id SessionID, text string) error {
	s, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("unknown session %q", id)
	}
	s.mu.Lock()
	pty := s.pty
	proc := s.proc
	s.mu.Unlock()
	if pty != nil {
		_, err := io.WriteString(pty, text)
		return err
	}
	_ = proc
	return fmt.Errorf("session %q has no input channel (process stdin is not held); use a PTY session", id)
}

// Keys sends named keys to a PTY session.
func (m *SessionManager) Keys(id SessionID, keys []string) error {
	s, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("unknown session %q", id)
	}
	s.mu.Lock()
	pty := s.pty
	s.mu.Unlock()
	if pty == nil {
		return fmt.Errorf("session %q is not a PTY session", id)
	}
	for _, k := range keys {
		seq, err := keySequence(k)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(pty, seq); err != nil {
			return err
		}
	}
	return nil
}

func keySequence(k string) (string, error) {
	switch strings.ToLower(k) {
	case "enter", "return":
		return "\r", nil
	case "tab":
		return "\t", nil
	case "esc", "escape":
		return "\x1b", nil
	case "up":
		return "\x1b[A", nil
	case "down":
		return "\x1b[B", nil
	case "right":
		return "\x1b[C", nil
	case "left":
		return "\x1b[D", nil
	case "ctrl-c", "ctrl_c", "intr":
		return "\x03", nil
	case "ctrl-d":
		return "\x04", nil
	case "ctrl-z":
		return "\x1a", nil
	case "space":
		return " ", nil
	case "backspace":
		return "\x7f", nil
	default:
		if len(k) == 1 {
			return k, nil
		}
		return "", fmt.Errorf("unknown key %q (want enter, ctrl-c, up, down, left, right, tab, esc)", k)
	}
}

// Read returns output since offset.
func (m *SessionManager) Read(id SessionID, offset int) (string, int, bool, error) {
	s, ok := m.Get(id)
	if !ok {
		return "", 0, false, fmt.Errorf("unknown session %q", id)
	}
	out, next := s.ReadSince(offset)
	return out, next, !s.snapshot(), nil
}

// WaitFor blocks until pattern matches new output, the process ends,
// or timeout elapses (Section 13.5). It avoids polling loops.
func (m *SessionManager) WaitFor(ctx context.Context, id SessionID, pattern string, timeout time.Duration, fromOffset int) (string, int, bool, error) {
	s, ok := m.Get(id)
	if !ok {
		return "", 0, false, fmt.Errorf("unknown session %q", id)
	}
	var re *regexp.Regexp
	if pattern != "" {
		var err error
		re, err = regexp.Compile(pattern)
		if err != nil {
			return "", 0, false, fmt.Errorf("bad pattern: %v", err)
		}
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		out, next := s.ReadSince(fromOffset)
		if re != nil && re.MatchString(out) {
			return out, next, !s.snapshot(), nil
		}
		if re == nil && !s.snapshot() {
			return out, next, true, nil
		}
		if !time.Now().Before(deadline) {
			return out, next, !s.snapshot(), fmt.Errorf("wait_for timed out after %s", timeout)
		}
		select {
		case <-ctx.Done():
			return out, next, !s.snapshot(), ctx.Err()
		case <-ticker.C:
		}
	}
}

// Signal sends interrupt/terminate/kill to the session process group.
func (m *SessionManager) Signal(id SessionID, sig string) error {
	s, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("unknown session %q", id)
	}
	s.mu.Lock()
	proc := s.proc
	pty := s.pty
	s.mu.Unlock()
	switch strings.ToLower(sig) {
	case "interrupt", "sigint":
		if pty != nil {
			_, err := io.WriteString(pty, "\x03")
			return err
		}
		return m.signalProc(proc, sigINT())
	case "terminate", "sigterm":
		return m.signalProc(proc, sigTERM())
	case "kill", "sigkill":
		return m.killProc(s)
	default:
		return fmt.Errorf("unknown signal %q (want interrupt, terminate, kill)", sig)
	}
}

func (m *SessionManager) signalProc(proc *os.Process, sig os.Signal) error {
	if proc == nil {
		return fmt.Errorf("session has no live process")
	}
	if m.deps.Group != nil {
		// Prefer group kill through the platform interface when the
		// test fake records it; real group signaling happens in the
		// Linux implementation (H2).
		if sig == sigKILL() {
			return m.deps.Group.Kill(proc.Pid)
		}
	}
	return proc.Signal(sig)
}

func (m *SessionManager) killProc(s *Session) error {
	s.mu.Lock()
	proc := s.proc
	s.mu.Unlock()
	if proc == nil {
		return fmt.Errorf("session has no live process")
	}
	if m.deps.Group != nil {
		return m.deps.Group.Kill(proc.Pid)
	}
	return proc.Kill()
}

// Close terminates the session: SIGTERM, 2 s margin, SIGKILL.
func (m *SessionManager) Close(id SessionID) error {
	s, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("unknown session %q", id)
	}
	s.mu.Lock()
	proc := s.proc
	pty := s.pty
	s.mu.Unlock()
	if proc != nil {
		_ = m.signalProc(proc, sigTERM())
		select {
		case <-s.done:
		case <-time.After(termKillMargin):
			_ = m.killProc(s)
			select {
			case <-s.done:
			case <-time.After(2 * time.Second):
			}
		}
	}
	if pty != nil {
		_ = pty.Close()
		s.markDone(fmt.Errorf("closed"))
	} else if proc == nil {
		s.markDone(fmt.Errorf("closed"))
	}
	m.mu.Lock()
	delete(m.sessions, id)
	for i, sid := range m.order {
		if sid == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	return nil
}

func (m *SessionManager) evictIfNeeded(owner string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	owned := 0
	for _, s := range m.sessions {
		if s.Owner == owner {
			owned++
		}
	}
	need := len(m.sessions) >= maxSessionsGlobal || owned >= maxSessionsPerOwner
	if !need {
		return
	}
	// Evict least-recently-used, protecting the 8 most recent globally.
	type cand struct {
		id   SessionID
		use  time.Time
		rank int
	}
	var cands []cand
	for i, sid := range m.order {
		s := m.sessions[sid]
		s.mu.Lock()
		use := s.lastUse
		s.mu.Unlock()
		cands = append(cands, cand{id: sid, use: use, rank: i})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].use.Before(cands[j].use) })
	protected := map[SessionID]bool{}
	for i := len(m.order) - protectedSessions; i < len(m.order); i++ {
		if i >= 0 {
			protected[m.order[i]] = true
		}
	}
	for _, c := range cands {
		if protected[c.id] {
			continue
		}
		s := m.sessions[c.id]
		if s == nil {
			continue
		}
		if len(m.sessions) < maxSessionsGlobal {
			ownedNow := 0
			for _, x := range m.sessions {
				if x.Owner == owner {
					ownedNow++
				}
			}
			if ownedNow < maxSessionsPerOwner {
				return
			}
		}
		delete(m.sessions, c.id)
		for i, sid := range m.order {
			if sid == c.id {
				m.order = append(m.order[:i], m.order[i+1:]...)
				break
			}
		}
		if s.proc != nil {
			_ = s.proc.Kill()
		}
		if s.pty != nil {
			_ = s.pty.Close()
		}
		s.markDone(fmt.Errorf("evicted"))
		if m.deps.OnEvict != nil {
			onEvict := m.deps.OnEvict
			evID, ow, cmd := c.id, s.Owner, s.Cmd
			// Call outside the lock to avoid reentrancy.
			defer func() { onEvict(evID, ow, cmd) }()
		}
		return
	}
}

// NonInteractiveEnvSwitch gates the extra non-interactive defaults
// (Section 13.5: avoid pagers, credential prompts and installer
// confirmations). Default ON for the ablation protocol.
const NonInteractiveEnvSwitch = "EXCALIBUR_NONINTERACTIVE_ENV"

// nonInteractiveEnvEnabled reports whether the extra defaults apply.
func nonInteractiveEnvEnabled() bool {
	v, ok := os.LookupEnv(NonInteractiveEnvSwitch)
	if !ok || len(v) == 0 {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "off", "false", "no":
		return false
	default:
		return true
	}
}

// pipeEnv forces non-interactive pager settings (Section 13.5).
func pipeEnv(extra []string) []string {
	base := os.Environ()
	over := map[string]string{"PAGER": "cat", "GIT_PAGER": "cat", "TERM": "dumb", "NO_COLOR": "1"}
	if nonInteractiveEnvEnabled() {
		// Never prompt: git credentials, apt confirmations and
		// listchanges pagers would hang a background session
		// waiting on a TTY that never answers.
		over["GIT_TERMINAL_PROMPT"] = "0"
		over["DEBIAN_FRONTEND"] = "noninteractive"
		over["APT_LISTCHANGES_FRONTEND"] = "none"
		over["NEEDRESTART_MODE"] = "a"
	}
	return mergeEnv(base, over, extra)
}

func ptyEnv(extra []string) []string {
	base := os.Environ()
	over := map[string]string{"PAGER": "cat", "GIT_PAGER": "cat", "TERM": "xterm-256color"}
	if nonInteractiveEnvEnabled() {
		over["GIT_TERMINAL_PROMPT"] = "0"
		over["DEBIAN_FRONTEND"] = "noninteractive"
		over["APT_LISTCHANGES_FRONTEND"] = "none"
		over["NEEDRESTART_MODE"] = "a"
	}
	return mergeEnv(base, over, extra)
}

func mergeEnv(base []string, over map[string]string, extra []string) []string {
	out := append([]string(nil), base...)
	idx := map[string]int{}
	for i, kv := range out {
		if k, _, ok := strings.Cut(kv, "="); ok {
			idx[k] = i
		}
	}
	for k, v := range over {
		if i, ok := idx[k]; ok {
			out[i] = k + "=" + v
		} else {
			out = append(out, k+"="+v)
			idx[k] = len(out) - 1
		}
	}
	for _, kv := range extra {
		if k, v, ok := strings.Cut(kv, "="); ok {
			if i, dup := idx[k]; dup {
				out[i] = k + "=" + v
			} else {
				out = append(out, kv)
				idx[k] = len(out) - 1
			}
		}
	}
	return out
}

// EnvProfile is the detected execution environment (Section 13.5).
type EnvProfile struct {
	OS              string          `json:"os"`
	Arch            string          `json:"arch"`
	Shells          []string        `json:"shells"`
	DefaultShell    string          `json:"default_shell"`
	PackageManagers []string        `json:"package_managers"`
	Tools           map[string]bool `json:"tools"`
	DetectedAt      time.Time       `json:"detected_at"`
}

// DetectEnv detects OS, arch, shells, package managers and tools.
func DetectEnv() EnvProfile {
	p := EnvProfile{OS: runtime.GOOS, Arch: runtime.GOARCH, Tools: map[string]bool{}, DetectedAt: time.Now()}
	for _, sh := range []string{"bash", "zsh", "fish", "sh", "dash"} {
		if _, err := exec.LookPath(sh); err == nil {
			p.Shells = append(p.Shells, sh)
		}
	}
	p.DefaultShell = DetectShell()
	for _, pm := range []string{"apt", "dnf", "pacman", "brew", "nix", "apk"} {
		if _, err := exec.LookPath(pm); err == nil {
			p.PackageManagers = append(p.PackageManagers, pm)
		}
	}
	for _, t := range []string{"git", "rg", "fd", "fzf", "node", "python3", "go", "rustc", "cargo", "docker"} {
		_, err := exec.LookPath(t)
		p.Tools[t] = err == nil
	}
	return p
}

// DetectShell picks the default interpreter: bash or zsh on Linux.
func DetectShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		base := filepath.Base(sh)
		if base == "bash" || base == "zsh" || base == "fish" || base == "sh" {
			if _, err := exec.LookPath(sh); err == nil {
				return sh
			}
			if _, err := exec.LookPath(base); err == nil {
				return base
			}
		}
	}
	for _, cand := range []string{"bash", "zsh", "sh"} {
		if p, err := exec.LookPath(cand); err == nil {
			return p
		}
	}
	return "/bin/sh"
}

// SessionSummary is the list entry for exec_io list and status.
type SessionSummary struct {
	ID      SessionID `json:"id"`
	Owner   string    `json:"owner"`
	Cmd     string    `json:"cmd"`
	Alive   bool      `json:"alive"`
	Started time.Time `json:"started"`
	Tail    string    `json:"tail"`
}

// Summaries lists sessions for owner with live tails.
func (m *SessionManager) Summaries(owner string) []SessionSummary {
	var out []SessionSummary
	for _, s := range m.List(owner) {
		s.mu.Lock()
		alive := !s.exited
		started := s.Started
		s.mu.Unlock()
		out = append(out, SessionSummary{ID: s.ID, Owner: s.Owner, Cmd: s.Cmd, Alive: alive, Started: started, Tail: s.LiveTail()})
	}
	return out
}
