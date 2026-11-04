//go:build !linux && !darwin && !windows

package platform

import (
	"errors"
	"os/exec"
	"testing"
)

// Non-Linux stubs must keep building and report errors.ErrUnsupported
// (Section 3.4): the binary compiles for darwin and windows with no
// platform behaviour.
func TestOtherStubsUnsupported(t *testing.T) {
	if _, err := (OtherPaths{}).DataDir(); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("DataDir = %v", err)
	}
	if _, err := (OtherPaths{}).ConfigDir(); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("ConfigDir = %v", err)
	}
	if _, err := (OtherPaths{}).CacheDir(); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("CacheDir = %v", err)
	}
	if _, err := (OtherPaths{}).RuntimeDir(); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("RuntimeDir = %v", err)
	}
	if err := (OtherProcessGroup{}).Kill(1); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Kill = %v", err)
	}
	(OtherProcessGroup{}).Configure(exec.Command("true"))
	if err := (OtherProcessPriority{}).LowerProcess(1); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("LowerProcess = %v", err)
	}
	if err := (OtherProcessPriority{}).LowerCurrentThread(); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("LowerCurrentThread = %v", err)
	}
	if _, err := (OtherResourceSampler{}).Sample(0); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Sample = %v", err)
	}
	if _, err := (OtherFileLock{}).Acquire("x"); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Acquire = %v", err)
	}
	if _, err := (OtherKeyring{}).Get("s", "u"); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Get = %v", err)
	}
	if err := (OtherKeyring{}).Set("s", "u", nil); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Set = %v", err)
	}
	if err := (OtherKeyring{}).Delete("s", "u"); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Delete = %v", err)
	}
	if err := (OtherServiceManager{}).Install(); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Install = %v", err)
	}
	if err := (OtherServiceManager{}).Uninstall(); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Uninstall = %v", err)
	}
	if _, err := (OtherServiceManager{}).Status(); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Status = %v", err)
	}
	if _, err := (OtherPTY{}).Start(exec.Command("true"), Winsize{}); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Start = %v", err)
	}
	if err := (OtherIsolator{}).Isolate(exec.Command("true")); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Isolate = %v", err)
	}
}
