//go:build linux

package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/creack/pty"
)

// A detached output pump retains the PTY master after the harness exits.
// The child keeps a real terminal (including keys and resize), while the
// harness reads its output from a log. Closing the harness's copy of the
// master cannot hang up a service or break its output stream.
func (m *SessionManager) trySurvivablePTY(s *Session, command, dir string, env []string) bool {
	if _, native := m.deps.PTY.(platform.LinuxPTY); !native || !CurrentExecScope().Active || !switchDefaultOn("EXCALIBUR_SURVIVE_SESSIONS") {
		return false
	}
	fail := func(err error) { s.appendOut([]byte("pty start error: " + err.Error())); s.markDone(err) }
	if err := m.preflight(s, command); err != nil {
		fail(err)
		return true
	}
	logPath := filepath.Join(os.TempDir(), fmt.Sprintf("excalibur-%s-%d.log", s.ID, s.Started.UnixNano()))
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		fail(err)
		return true
	}
	defer log.Close()
	c := exec.Command(DetectShell(), "-c", command)
	c.Dir = dir
	c.Env = ptyEnv(env)
	// creack/pty supplies setsid and the controlling terminal. Parent-death
	// signalling is intentionally absent for autonomous surviving sessions.
	if m.deps.Isolator != nil {
		_ = m.deps.Isolator.Isolate(c)
	}
	master, err := pty.StartWithSize(c, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		fail(err)
		return true
	}
	pump := exec.Command("cat")
	pump.Stdin = master
	pump.Stdout = log
	pump.Stderr = log
	pump.SysProcAttr = detachedSysProcAttr()
	if err := pump.Start(); err != nil {
		_ = master.Close()
		_ = c.Process.Kill()
		_ = c.Wait()
		fail(err)
		return true
	}
	s.mu.Lock()
	s.proc = c.Process
	s.Pid = c.Process.Pid
	s.Survivable = true
	s.LogPath = logPath
	s.pty = &survivingPTY{master: master, command: c}
	if m.deps.Priority != nil {
		_ = m.deps.Priority.LowerProcess(c.Process.Pid)
	}
	s.mu.Unlock()
	waited := make(chan error, 1)
	safego.Go("tools/terminal-persistent-pty-wait", nil, func() {
		err := c.Wait()
		_ = pump.Wait()
		_ = master.Close()
		waited <- err
	})
	m.tailSessionLog(s, logPath, waited)
	return true
}

type survivingPTY struct {
	master  *os.File
	command *exec.Cmd
}

func (s *survivingPTY) Read(p []byte) (int, error)  { return s.master.Read(p) }
func (s *survivingPTY) Write(p []byte) (int, error) { return s.master.Write(p) }
func (s *survivingPTY) Resize(ws platform.Winsize) error {
	return pty.Setsize(s.master, &pty.Winsize{Rows: ws.Rows, Cols: ws.Cols})
}
func (s *survivingPTY) Close() error { return s.master.Close() }
func (s *survivingPTY) Wait() error  { return s.command.Wait() }
