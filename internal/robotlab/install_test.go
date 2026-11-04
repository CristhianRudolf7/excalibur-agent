package robotlab

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRunner struct {
	mu      sync.Mutex
	calls   [][]string
	version string
	fail    string
	block   chan struct{}
	entered chan struct{}
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, append([]string{name}, args...))
	n := len(f.calls)
	f.mu.Unlock()
	if n == 1 && f.block != nil {
		close(f.entered)
		select {
		case <-f.block:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if strings.Contains(strings.Join(args, " "), f.fail) && f.fail != "" {
		return "", errors.New("fake failure")
	}
	if n == 1 {
		return f.version, nil
	}
	if len(args) > 1 && args[0] == "-m" && args[1] == "venv" {
		return "", os.MkdirAll(filepath.Join(args[2], "bin"), 0700)
	}
	return "", nil
}
func installerFixture(t *testing.T, f *fakeRunner) (*Installer, <-chan map[string]any) {
	t.Helper()
	t.Setenv("EXCALIBUR_ROBOTLAB", "1")
	data := t.TempDir()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "requirements.lock"), []byte("mujoco==3.3.7\nnumpy==2.5.4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	events := make(chan map[string]any, 16)
	i := &Installer{DataDir: data, Runner: f, Extract: func(string) (string, error) { return source, nil }, Publish: func(_ string, p any) { events <- p.(map[string]any) }}
	t.Cleanup(i.Close)
	return i, events
}
func awaitInstall(t *testing.T, events <-chan map[string]any) map[string]any {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e := <-events:
			if e["done"] == true {
				return e
			}
		case <-timer.C:
			t.Fatal("install did not finish")
			return nil
		}
	}
}
func TestInstallFlowAndCoalescing(t *testing.T) {
	f := &fakeRunner{version: "3.12", block: make(chan struct{}), entered: make(chan struct{})}
	i, events := installerFixture(t, f)
	if err := i.Start(); err != nil {
		t.Fatal(err)
	}
	<-f.entered
	if err := i.Start(); err != nil {
		t.Fatal(err)
	}
	close(f.block)
	e := awaitInstall(t, events)
	if e["phase"] != "done" {
		t.Fatal(e)
	}
	f.mu.Lock()
	calls := f.calls
	f.mu.Unlock()
	if len(calls) != 5 {
		t.Fatalf("coalescing/commands: %v", calls)
	}
	want := []string{"-m", "robotlab.assets", "fetch", "--dest", filepath.Join(i.DataDir, "robotlab", "menagerie")}
	if !reflect.DeepEqual(calls[3][1:], want) {
		t.Fatalf("asset command: %v", calls[3])
	}
	if _, err := os.Stat(filepath.Join(i.DataDir, "robotlab", "venv", ".robotlab-installed")); err != nil {
		t.Fatal(err)
	}
}
func TestInstallFailures(t *testing.T) {
	for _, tt := range []struct{ name, version, fail string }{{"old Python", "3.9", ""}, {"pip failure", "3.12", "pip"}, {"asset failure", "3.12", "robotlab.assets"}, {"import failure", "3.12", "import mujoco"}} {
		t.Run(tt.name, func(t *testing.T) {
			i, events := installerFixture(t, &fakeRunner{version: tt.version, fail: tt.fail})
			if err := i.Start(); err != nil {
				t.Fatal(err)
			}
			e := awaitInstall(t, events)
			if e["phase"] != "error" || e["error"] == nil {
				t.Fatal(e)
			}
			if _, err := os.Stat(filepath.Join(i.DataDir, "robotlab", "venv", ".robotlab-installed")); !os.IsNotExist(err) {
				t.Fatal("failed install marked complete")
			}
		})
	}
}
func TestInstallCloseCancels(t *testing.T) {
	f := &fakeRunner{version: "3.12", block: make(chan struct{}), entered: make(chan struct{})}
	i, _ := installerFixture(t, f)
	if err := i.Start(); err != nil {
		t.Fatal(err)
	}
	<-f.entered
	i.Close()
}
