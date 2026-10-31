//go:build linux

package platform

import (
	"os/exec"
)

// Wave-0 Linux placeholders. Milestone H2 replaces every method with
// the real implementation (cgroups, /proc, flock, Secret Service,
// systemd, pty). The types below exist so the core compiles against the
// interfaces from day zero.

var errLinuxPending = ErrNotImplemented

// LinuxPaths is the placeholder Paths implementation.
type LinuxPaths struct{}

// Compile-time interface check.
var _ Paths = LinuxPaths{}

func (LinuxPaths) DataDir() (string, error)    { return "", errLinuxPending }
func (LinuxPaths) ConfigDir() (string, error)  { return "", errLinuxPending }
func (LinuxPaths) CacheDir() (string, error)   { return "", errLinuxPending }
func (LinuxPaths) RuntimeDir() (string, error) { return "", errLinuxPending }
func (LinuxPaths) ExtensionsDir() []string     { return nil }

// LinuxProcessGroup is the placeholder ProcessGroup implementation.
type LinuxProcessGroup struct{}

// Compile-time interface check.
var _ ProcessGroup = LinuxProcessGroup{}

func (LinuxProcessGroup) Configure(_ *exec.Cmd) {}
func (LinuxProcessGroup) Kill(_ int) error      { return errLinuxPending }

// LinuxProcessPriority is the placeholder ProcessPriority implementation.
type LinuxProcessPriority struct{}

// Compile-time interface check.
var _ ProcessPriority = LinuxProcessPriority{}

func (LinuxProcessPriority) LowerProcess(_ int) error  { return errLinuxPending }
func (LinuxProcessPriority) LowerCurrentThread() error { return errLinuxPending }

// LinuxResourceSampler is the placeholder ResourceSampler implementation.
type LinuxResourceSampler struct{}

// Compile-time interface check.
var _ ResourceSampler = LinuxResourceSampler{}

func (LinuxResourceSampler) Sample(_ int) (ResourceSample, error) {
	return ResourceSample{}, errLinuxPending
}

// LinuxFileLock is the placeholder FileLock implementation.
type LinuxFileLock struct{}

// Compile-time interface check.
var _ FileLock = LinuxFileLock{}

func (LinuxFileLock) Acquire(_ string) (Release, error) {
	return nil, errLinuxPending
}

// LinuxKeyring is the placeholder Keyring implementation.
type LinuxKeyring struct{}

// Compile-time interface check.
var _ Keyring = LinuxKeyring{}

func (LinuxKeyring) Get(_, _ string) ([]byte, error) { return nil, errLinuxPending }
func (LinuxKeyring) Set(_, _ string, _ []byte) error { return errLinuxPending }
func (LinuxKeyring) Delete(_, _ string) error        { return errLinuxPending }

// LinuxServiceManager is the placeholder ServiceManager implementation.
type LinuxServiceManager struct{}

// Compile-time interface check.
var _ ServiceManager = LinuxServiceManager{}

func (LinuxServiceManager) Install() error          { return errLinuxPending }
func (LinuxServiceManager) Uninstall() error        { return errLinuxPending }
func (LinuxServiceManager) Status() (string, error) { return "", errLinuxPending }

// LinuxPTY is the placeholder PTY implementation.
type LinuxPTY struct{}

// Compile-time interface check.
var _ PTY = LinuxPTY{}

func (LinuxPTY) Start(_ *exec.Cmd, _ Winsize) (PTYSession, error) {
	return nil, errLinuxPending
}

// LinuxIsolator is the empty extension point: it runs the process
// as-is. Deferred security (Section 20) connects here.
type LinuxIsolator struct{}

// Compile-time interface check.
var _ Isolator = LinuxIsolator{}

func (LinuxIsolator) Isolate(_ *exec.Cmd) error { return nil }
