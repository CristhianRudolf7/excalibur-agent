package robotlab

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// CommandRunner allows installation to be verified without network or Python.
type CommandRunner interface {
	Run(context.Context, string, ...string) (string, error)
}
type execRunner struct{ Dir string }

func (r execRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = childEnvironment(true)
	cmd.Dir = r.Dir
	group := sidecarProcessGroup()
	group.Configure(cmd)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return group.Kill(cmd.Process.Pid)
	}
	cmd.WaitDelay = 3 * time.Second
	var out tailWriter
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := startSidecarCommand(cmd, group)
	if err == nil {
		defer cleanupSidecarGroup(group)
		_ = lowerSidecarPriority(cmd.Process.Pid)
		err = cmd.Wait()
	}
	return out.String(), err
}

type tailWriter struct {
	mu sync.Mutex
	b  []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.b = append(w.b, p...)
	if len(w.b) > 8192 {
		w.b = append([]byte(nil), w.b[len(w.b)-8192:]...)
	}
	return len(p), nil
}
func (w *tailWriter) String() string { w.mu.Lock(); defer w.mu.Unlock(); return string(w.b) }

// Installer coalesces explicit install requests. It never runs from status/startup.
type Installer struct {
	// Extract overrides embedded extraction for isolated installation tests.
	Extract func(string) (string, error)
	DataDir string
	Runner  CommandRunner
	Publish func(string, any)
	mu      sync.Mutex
	done    chan struct{}
	cancel  context.CancelFunc
	closed  bool
}

func (i *Installer) Start() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if !Enabled() || !runtimeSupported() || i.closed {
		return &RuntimeError{"unavailable", "Robot Lab installation unavailable"}
	}
	if i.done != nil {
		select {
		case <-i.done:
		default:
			return nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	i.cancel = cancel
	i.done = make(chan struct{})
	done := i.done
	safego.Go("robotlab-install", nil, func() {
		defer close(done)
		defer cancel()
		if err := i.install(ctx); err != nil {
			i.progress("error", err.Error(), 0, true)
		}
	})
	return nil
}
func (i *Installer) Close() {
	i.mu.Lock()
	i.closed = true
	cancel, done := i.cancel, i.done
	i.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}
func (i *Installer) progress(phase, message string, percent int, done bool) {
	if i.Publish != nil {
		p := map[string]any{"phase": phase, "message": message, "percent": percent}
		if done {
			p["done"] = true
		}
		if phase == "error" {
			p["error"] = message
		}
		i.Publish("robotlab.install", p)
	}
}
func (i *Installer) install(ctx context.Context) error {
	runner := i.Runner
	if runner == nil {
		runner = execRunner{}
	}
	i.progress("checking", prerequisiteMessage(), 0, false)
	bootstrap, prefix, err := bootstrapPython()
	if err != nil {
		return err
	}
	version, err := runner.Run(ctx, bootstrap, append(append([]string{}, prefix...), "-c", "import sys; print('%d.%d' % sys.version_info[:2])")...)
	if err != nil {
		return fmt.Errorf("%s: %w", bootstrap, err)
	}
	var major, minor int
	if _, err := fmt.Sscanf(strings.TrimSpace(version), "%d.%d", &major, &minor); err != nil || major < 3 || (major == 3 && minor < 10) {
		return fmt.Errorf("python >=3.10 required (found %s)", strings.TrimSpace(version))
	}
	extract := i.Extract
	if extract == nil {
		extract = ExtractRuntime
	}
	source, err := extract(i.DataDir)
	if err != nil {
		return err
	}
	lock := filepath.Join(source, "requirements.lock")
	b, err := os.ReadFile(lock)
	if err != nil {
		return fmt.Errorf("embedded requirements.lock missing: %w", err)
	}
	if lock, _, err = prepareRuntimeRequirements(lock, b); err != nil {
		return fmt.Errorf("prepare runtime requirements: %w", err)
	}
	venv := filepath.Join(i.DataDir, "robotlab", "venv")
	i.progress("venv", "Creating the Robot Lab Python environment", 10, false)
	_ = os.Remove(filepath.Join(venv, ".robotlab-installed"))
	if _, err = runner.Run(ctx, bootstrap, append(append([]string{}, prefix...), "-m", "venv", venv)...); err != nil {
		return fmt.Errorf("create venv: %w", err)
	}
	python := venvPython(venv)
	if i.Runner == nil {
		runner = execRunner{Dir: source}
	}
	i.progress("packages", "Installing pinned runtime packages", 30, false)
	if _, err = runner.Run(ctx, python, "-m", "pip", "install", "-r", lock); err != nil {
		return fmt.Errorf("install packages: %w", err)
	}
	i.progress("assets", "Downloading pinned robot assets and licenses", 60, false)
	if _, err = runner.Run(ctx, python, "-m", "robotlab.assets", "fetch", "--dest", filepath.Join(i.DataDir, "robotlab", "menagerie")); err != nil {
		return fmt.Errorf("fetch robot assets: %w", err)
	}
	i.progress("verify", "Verifying runtime imports", 90, false)
	if _, err = runner.Run(ctx, python, "-c", "import mujoco, numpy, cv2, PIL"); err != nil {
		return fmt.Errorf("verify runtime: %w", err)
	}
	if err = os.WriteFile(filepath.Join(venv, ".robotlab-installed"), []byte("1\n"), 0600); err != nil {
		return err
	}
	message := "Robot Lab installed"
	if !detectOSMesa() {
		message += "; install the system OSMesa library before rendering"
	}
	i.progress("done", message, 100, true)
	return nil
}
