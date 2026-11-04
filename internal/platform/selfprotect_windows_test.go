package platform

import (
	"errors"
	"os"
	"testing"
)

func TestWindowsProtectedSignalTargets(t *testing.T) {
	targets, err := ProtectedSignalTargets()
	if err != nil {
		t.Fatal(err)
	}
	found := make(map[int]bool)
	for _, target := range targets {
		found[target.PID] = true
		if target.Group != 0 || target.Name == "" || target.Command == "" {
			t.Fatalf("bad target: %+v", target)
		}
	}
	if !found[os.Getpid()] || !found[os.Getppid()] {
		t.Fatalf("missing self or parent: %+v", targets)
	}
	if err := Reexec(nil, nil); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Reexec: %v", err)
	}
}
