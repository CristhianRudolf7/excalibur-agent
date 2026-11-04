package platform

import (
	"errors"
	"os/exec"
)

// Compatibility stubs retained for callers that explicitly request an
// unsupported backend. Native macOS backends live in separate Darwin
// files and implement the same shared interfaces (Section 3.4).

// OtherPaths is the non-Linux Paths stub.
type OtherPaths struct{}

// Compile-time interface check.
var _ Paths = OtherPaths{}

func (OtherPaths) DataDir() (string, error)    { return "", errors.ErrUnsupported }
func (OtherPaths) ConfigDir() (string, error)  { return "", errors.ErrUnsupported }
func (OtherPaths) CacheDir() (string, error)   { return "", errors.ErrUnsupported }
func (OtherPaths) RuntimeDir() (string, error) { return "", errors.ErrUnsupported }
func (OtherPaths) ExtensionsDir() []string     { return nil }

// OtherProcessGroup is the non-Linux ProcessGroup stub.
type OtherProcessGroup struct{}

// Compile-time interface check.
var _ ProcessGroup = OtherProcessGroup{}

func (OtherProcessGroup) Configure(_ *exec.Cmd) {}
func (OtherProcessGroup) Kill(_ int) error      { return errors.ErrUnsupported }

// OtherProcessPriority is the non-Linux ProcessPriority stub.
type OtherProcessPriority struct{}

// Compile-time interface check.
var _ ProcessPriority = OtherProcessPriority{}

func (OtherProcessPriority) LowerProcess(_ int) error  { return errors.ErrUnsupported }
func (OtherProcessPriority) LowerCurrentThread() error { return errors.ErrUnsupported }

// OtherResourceSampler is the non-Linux ResourceSampler stub.
type OtherResourceSampler struct{}

// Compile-time interface check.
var _ ResourceSampler = OtherResourceSampler{}

func (OtherResourceSampler) Sample(_ int) (ResourceSample, error) {
	return ResourceSample{}, errors.ErrUnsupported
}

// OtherFileLock is the non-Linux FileLock stub.
type OtherFileLock struct{}

// Compile-time interface check.
var _ FileLock = OtherFileLock{}

func (OtherFileLock) Acquire(_ string) (Release, error) {
	return nil, errors.ErrUnsupported
}

// OtherKeyring is the non-Linux Keyring stub.
type OtherKeyring struct{}

// Compile-time interface check.
var _ Keyring = OtherKeyring{}

func (OtherKeyring) Get(_, _ string) ([]byte, error) { return nil, errors.ErrUnsupported }
func (OtherKeyring) Set(_, _ string, _ []byte) error { return errors.ErrUnsupported }
func (OtherKeyring) Delete(_, _ string) error        { return errors.ErrUnsupported }

// OtherServiceManager is the non-Linux ServiceManager stub.
type OtherServiceManager struct{}

// Compile-time interface check.
var _ ServiceManager = OtherServiceManager{}

func (OtherServiceManager) Install() error          { return errors.ErrUnsupported }
func (OtherServiceManager) Uninstall() error        { return errors.ErrUnsupported }
func (OtherServiceManager) Status() (string, error) { return "", errors.ErrUnsupported }

// OtherPTY is the non-Linux PTY stub.
type OtherPTY struct{}

// Compile-time interface check.
var _ PTY = OtherPTY{}

func (OtherPTY) Start(_ *exec.Cmd, _ Winsize) (PTYSession, error) {
	return nil, errors.ErrUnsupported
}

// OtherIsolator is the non-Linux Isolator stub.
type OtherIsolator struct{}

// Compile-time interface check.
var _ Isolator = OtherIsolator{}

func (OtherIsolator) Isolate(_ *exec.Cmd) error { return errors.ErrUnsupported }
