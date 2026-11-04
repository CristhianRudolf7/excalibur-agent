//go:build darwin

package platform

import (
	"errors"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// errNilCmd is returned when Start receives a nil command.
var errNilCmd = errors.New("platform: nil command")

// DarwinPTY starts interactive terminal sessions (Section 13.5) via
// creack/pty.
type DarwinPTY struct{}

// Compile-time interface check.
var _ PTY = DarwinPTY{}

// Start implements platform.PTY: it launches cmd under a PTY sized ws
// and returns a session wrapping the master side.
func (DarwinPTY) Start(cmd *exec.Cmd, ws Winsize) (PTYSession, error) {
	if cmd == nil {
		return nil, errNilCmd
	}
	// creack/pty starts the child with Setsid and Setctty. A session
	// leader is already its own process-group leader, and a later
	// setpgid on it fails with EPERM, so drop any group request the
	// caller configured.
	if cmd.SysProcAttr != nil {
		cmd.SysProcAttr.Setpgid = false
		cmd.SysProcAttr.Pgid = 0
	}
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: ws.Rows, Cols: ws.Cols})
	if err != nil {
		return nil, err
	}
	return &darwinPTYSession{master: master, cmd: cmd}, nil
}

// darwinPTYSession wraps the PTY master fd.
type darwinPTYSession struct {
	master  *os.File
	cmd     *exec.Cmd
	closeMu sync.Mutex
	closed  bool
}

// Compile-time interface check.
var _ PTYSession = (*darwinPTYSession)(nil)

// Read implements platform.PTYSession.
func (s *darwinPTYSession) Read(p []byte) (int, error) {
	return s.master.Read(p)
}

// Write implements platform.PTYSession.
func (s *darwinPTYSession) Write(p []byte) (int, error) {
	return s.master.Write(p)
}

// Resize implements platform.PTYSession.
func (s *darwinPTYSession) Resize(ws Winsize) error {
	return pty.Setsize(s.master, &pty.Winsize{Rows: ws.Rows, Cols: ws.Cols})
}

// Close implements platform.PTYSession: it closes the master side;
// the child observes SIGHUP/EIO and exits, reaped by Wait.
func (s *darwinPTYSession) Close() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.master.Close()
}

// Wait implements platform.PTYSession.
func (s *darwinPTYSession) Wait() error {
	return s.cmd.Wait()
}
