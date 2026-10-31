package fake

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

// Paths serves fixed directories.
type Paths struct {
	Data    string
	Config  string
	Cache   string
	Runtime string
}

// Compile-time interface check.
var _ platform.Paths = Paths{}

// TempPaths returns Paths rooted at a fresh temp dir.
func TempPaths(prefix string) (Paths, error) {
	root, err := os.MkdirTemp("", prefix)
	if err != nil {
		return Paths{}, err
	}
	p := Paths{
		Data:    root + "/data",
		Config:  root + "/config",
		Cache:   root + "/cache",
		Runtime: root + "/run",
	}
	for _, d := range []string{p.Data, p.Config, p.Cache, p.Runtime} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return Paths{}, err
		}
	}
	return p, nil
}

// DataDir implements platform.Paths.
func (p Paths) DataDir() (string, error) { return p.Data, nil }

// ConfigDir implements platform.Paths.
func (p Paths) ConfigDir() (string, error) { return p.Config, nil }

// CacheDir implements platform.Paths.
func (p Paths) CacheDir() (string, error) { return p.Cache, nil }

// RuntimeDir implements platform.Paths.
func (p Paths) RuntimeDir() (string, error) { return p.Runtime, nil }

// ExtensionsDir implements platform.Paths.
func (p Paths) ExtensionsDir() []string { return []string{p.Data + "/extensions"} }

// ProcessGroup records configured commands and killed pids.
type ProcessGroup struct {
	mu       sync.Mutex
	Killed   []int
	Combined int
}

// Compile-time interface check.
var _ platform.ProcessGroup = (*ProcessGroup)(nil)

// Configure implements platform.ProcessGroup.
func (g *ProcessGroup) Configure(_ *exec.Cmd) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Combined++
}

// Kill implements platform.ProcessGroup.
func (g *ProcessGroup) Kill(pid int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Killed = append(g.Killed, pid)
	return nil
}

// ProcessPriority records lowered pids.
type ProcessPriority struct {
	mu      sync.Mutex
	Lowered []int
	Thread  int
}

// Compile-time interface check.
var _ platform.ProcessPriority = (*ProcessPriority)(nil)

// LowerProcess implements platform.ProcessPriority.
func (p *ProcessPriority) LowerProcess(pid int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Lowered = append(p.Lowered, pid)
	return nil
}

// LowerCurrentThread implements platform.ProcessPriority.
func (p *ProcessPriority) LowerCurrentThread() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Thread++
	return nil
}

// ResourceSampler serves a scripted sample.
type ResourceSampler struct {
	Sampled []int
	Next    platform.ResourceSample
	Err     error
	mu      sync.Mutex
}

// Compile-time interface check.
var _ platform.ResourceSampler = (*ResourceSampler)(nil)

// Sample implements platform.ResourceSampler.
func (s *ResourceSampler) Sample(pid int) (platform.ResourceSample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Sampled = append(s.Sampled, pid)
	return s.Next, s.Err
}

// FileLock is a no-op lock that records acquisitions.
type FileLock struct {
	mu       sync.Mutex
	Acquired []string
	Released int
}

// Compile-time interface check.
var _ platform.FileLock = (*FileLock)(nil)

// Acquire implements platform.FileLock.
func (l *FileLock) Acquire(path string) (platform.Release, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Acquired = append(l.Acquired, path)
	return func() error {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.Released++
		return nil
	}, nil
}

// Keyring keeps secrets in a map.
type Keyring struct {
	mu   sync.Mutex
	data map[string][]byte
}

// Compile-time interface check.
var _ platform.Keyring = (*Keyring)(nil)

// NewKeyring returns an empty keyring.
func NewKeyring() *Keyring { return &Keyring{data: map[string][]byte{}} }

func key(service, user string) string { return service + "\x00" + user }

// Get implements platform.Keyring.
func (k *Keyring) Get(service, user string) ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.data[key(service, user)]
	if !ok {
		return nil, fmt.Errorf("fake: no keyring entry for %s/%s", service, user)
	}
	return append([]byte(nil), v...), nil
}

// Set implements platform.Keyring.
func (k *Keyring) Set(service, user string, secret []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.data[key(service, user)] = append([]byte(nil), secret...)
	return nil
}

// Delete implements platform.Keyring.
func (k *Keyring) Delete(service, user string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.data, key(service, user))
	return nil
}

// ServiceManager records install state.
type ServiceManager struct {
	mu        sync.Mutex
	Installed bool
}

// Compile-time interface check.
var _ platform.ServiceManager = (*ServiceManager)(nil)

// Install implements platform.ServiceManager.
func (s *ServiceManager) Install() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Installed = true
	return nil
}

// Uninstall implements platform.ServiceManager.
func (s *ServiceManager) Uninstall() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Installed = false
	return nil
}

// Status implements platform.ServiceManager.
func (s *ServiceManager) Status() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Installed {
		return "installed", nil
	}
	return "absent", nil
}

// PTYSession is an in-memory PTY session backed by buffers.
type PTYSession struct {
	mu     sync.Mutex
	In     bytes.Buffer
	Out    bytes.Buffer
	Sizes  []platform.Winsize
	Closed bool
	Code   error
}

// Compile-time interface check.
var _ platform.PTYSession = (*PTYSession)(nil)

// Read implements platform.PTYSession.
func (s *PTYSession) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Out.Read(p)
}

// Write implements platform.PTYSession.
func (s *PTYSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.In.Write(p)
}

// Resize implements platform.PTYSession.
func (s *PTYSession) Resize(ws platform.Winsize) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Sizes = append(s.Sizes, ws)
	return nil
}

// Close implements platform.PTYSession.
func (s *PTYSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Closed = true
	return nil
}

// Wait implements platform.PTYSession.
func (s *PTYSession) Wait() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Code
}

// PTY serves scripted sessions.
type PTY struct {
	mu       sync.Mutex
	Started  int
	LastSize platform.Winsize
	Session  *PTYSession
}

// Compile-time interface check.
var _ platform.PTY = (*PTY)(nil)

// Start implements platform.PTY.
func (p *PTY) Start(_ *exec.Cmd, ws platform.Winsize) (platform.PTYSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Started++
	p.LastSize = ws
	if p.Session == nil {
		p.Session = &PTYSession{}
	}
	return p.Session, nil
}

// Isolator records isolated commands.
type Isolator struct {
	mu       sync.Mutex
	Isolated int
}

// Compile-time interface check.
var _ platform.Isolator = (*Isolator)(nil)

// Isolate implements platform.Isolator: the wave-0 policy runs the
// process as-is and only records the call.
func (iso *Isolator) Isolate(_ *exec.Cmd) error {
	iso.mu.Lock()
	defer iso.mu.Unlock()
	iso.Isolated++
	return nil
}
