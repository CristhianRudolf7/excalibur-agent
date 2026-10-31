package platform

import (
	"errors"
	"io"
	"os/exec"
	"time"
)

// ErrNotImplemented is a retained sentinel from the wave-0 contract:
// the Linux backends are implemented since milestone H2 and no
// longer return it. It stays for compatibility with out-of-tree
// platform backends that have not implemented an operation yet.
var ErrNotImplemented = errors.New("platform: operation not implemented")

// Paths resolves OS-specific directories (XDG on Linux).
type Paths interface {
	// DataDir is the per-user data root (<data>/excalibur).
	DataDir() (string, error)
	// ConfigDir is the per-user configuration root.
	ConfigDir() (string, error)
	// CacheDir is the per-user cache root.
	CacheDir() (string, error)
	// RuntimeDir is the per-user runtime root ($XDG_RUNTIME_DIR).
	RuntimeDir() (string, error)
	// ExtensionsDir lists user extension directories in lookup order.
	ExtensionsDir() []string
}

// ProcessGroup manages tool processes as killable groups (Section 3.2).
type ProcessGroup interface {
	// Configure detaches cmd into its own process group.
	Configure(cmd *exec.Cmd)
	// Kill terminates the whole group for pid, including grandchildren.
	Kill(pid int) error
}

// ProcessPriority lowers tool and background-thread priority so the
// interactive path stays responsive (Sections 3.2, 5.2).
type ProcessPriority interface {
	// LowerProcess applies the low-priority policy to pid (cgroup
	// weight, or nice/ioprio fallback on Linux).
	LowerProcess(pid int) error
	// LowerCurrentThread lowers the calling worker thread (Linux: nice
	// on the tid).
	LowerCurrentThread() error
}

// ResourceSample is one RAM/CPU/process observation (Sections 5.2,
// 22.2).
type ResourceSample struct {
	At         time.Time
	RSSBytes   uint64
	CPUSeconds float64
	Processes  int
	OOMKills   int
}

// ResourceSampler reads memory, CPU and process counters (Linux: /proc
// and cgroup v2).
type ResourceSampler interface {
	// Sample takes one observation of the daemon (and optionally pid).
	Sample(pid int) (ResourceSample, error)
}

// Release unlocks one held file lock.
type Release func() error

// FileLock serializes writers across processes (Linux: flock).
type FileLock interface {
	// Acquire blocks until the lock on path is held or ctx expires.
	Acquire(path string) (Release, error)
}

// Keyring stores the vault master key (Linux: Secret Service via D-Bus;
// Section 20.1).
type Keyring interface {
	Get(service, user string) ([]byte, error)
	Set(service, user string, secret []byte) error
	Delete(service, user string) error
}

// ServiceManager installs and queries the user-level supervisor unit
// (Linux: systemd user unit).
type ServiceManager interface {
	Install() error
	Uninstall() error
	Status() (string, error)
}

// Winsize is a PTY size in cells.
type Winsize struct {
	Rows uint16
	Cols uint16
}

// PTYSession is one interactive terminal session (Section 13.5).
type PTYSession interface {
	io.ReadWriter
	// Resize changes the terminal size.
	Resize(ws Winsize) error
	// Close terminates the session.
	Close() error
	// Wait waits for the process and reports its exit.
	Wait() error
}

// PTY starts interactive terminal sessions (Linux: creack/pty).
type PTY interface {
	// Start launches cmd under a PTY of size ws.
	Start(cmd *exec.Cmd, ws Winsize) (PTYSession, error)
}

// Isolator is the extension point where deferred security (Section 20)
// connects. In this version it runs the process as-is.
type Isolator interface {
	// Isolate applies the isolation policy to cmd before start.
	Isolate(cmd *exec.Cmd) error
}
