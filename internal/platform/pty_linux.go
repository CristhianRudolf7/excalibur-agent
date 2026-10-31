//go:build linux

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

// LinuxPTY starts interactive terminal sessions (Section 13.5) via
// creack/pty.
type LinuxPTY struct{}

// Compile-time interface check.
var _ PTY = LinuxPTY{}

// Start implements platform.PTY: it launches cmd under a PTY sized ws
// and returns a session wrapping the master side.
func (LinuxPTY) Start(cmd *exec.Cmd, ws Winsize) (PTYSession, error) {
	if cmd == nil {
		return nil, errNilCmd
	}
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: ws.Rows, Cols: ws.Cols})
	if err != nil {
		return nil, err
	}
	return &linuxPTYSession{master: master, cmd: cmd}, nil
}

// linuxPTYSession wraps the PTY master fd.
type linuxPTYSession struct {
	master  *os.File
	cmd     *exec.Cmd
	closeMu sync.Mutex
	closed  bool
}

// Compile-time interface check.
var _ PTYSession = (*linuxPTYSession)(nil)

// Read implements platform.PTYSession.
func (s *linuxPTYSession) Read(p []byte) (int, error) {
	return s.master.Read(p)
}

// Write implements platform.PTYSession.
func (s *linuxPTYSession) Write(p []byte) (int, error) {
	return s.master.Write(p)
}

// Resize implements platform.PTYSession.
func (s *linuxPTYSession) Resize(ws Winsize) error {
	return pty.Setsize(s.master, &pty.Winsize{Rows: ws.Rows, Cols: ws.Cols})
}

// Close implements platform.PTYSession: it closes the master side;
// the child observes SIGHUP/EIO and exits, reaped by Wait.
func (s *linuxPTYSession) Close() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.master.Close()
}

// Wait implements platform.PTYSession.
func (s *linuxPTYSession) Wait() error {
	return s.cmd.Wait()
}
