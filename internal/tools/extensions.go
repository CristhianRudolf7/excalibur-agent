package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/fsnotify/fsnotify"
)

// Extensions kit (Section 14.12). Any new tool or capability joins
// through one of four routes — MCP server, app with interface, tool
// package, in-process Go — and lands in the same catalog through the
// same execution pipeline. Integrated tools register with the same
// interface and manifest as an extension: there is no privileged path.

// ExtensionFileName is the single manifest file every route uses.
const ExtensionFileName = "excalibur-extension.yaml"

// ExtensionStatus is the lifecycle state of a loaded extension.
type ExtensionStatus string

// Extension states.
const (
	ExtensionLoaded     ExtensionStatus = "loaded"
	ExtensionActive     ExtensionStatus = "active"
	ExtensionDisabled   ExtensionStatus = "disabled"
	ExtensionFailed     ExtensionStatus = "failed"
	ExtensionSuperseded ExtensionStatus = "superseded"
)

// LoadedExtension is one discovered extension with its runtime state.
type LoadedExtension struct {
	Manifest Manifest
	Dir      string
	Status   ExtensionStatus
	FailWhy  string

	tools    []Tool
	inflight atomic.Int64
	lastUsed atomic.Int64 // unix nanos

	mu        sync.Mutex
	connector ioCloser
	channel   AppChannel
}

// Tools returns the active tools of the extension.
func (e *LoadedExtension) Tools() []Tool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Tool(nil), e.tools...)
}

// InFlight reports the calls currently executing in the extension.
func (e *LoadedExtension) InFlight() int64 { return e.inflight.Load() }

type ioCloser interface{ Close() error }

// trackedTool wraps one extension tool to gate updates on in-flight
// calls (Section 14.12: a new version activates when no calls to the
// old one are in flight).
type trackedTool struct {
	Tool
	ext *LoadedExtension
}

func (t *trackedTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	t.ext.inflight.Add(1)
	defer t.ext.inflight.Add(-1)
	t.ext.lastUsed.Store(time.Now().UnixNano())
	return t.Tool.Invoke(ctx, inv)
}

// Loader discovers, validates, activates and retires extensions. All
// four routes end in the same catalog as deferred tools, so adding or
// removing an extension never changes the fixed prefix of a running
// execution (Sections 11.6, 13.3, 14.12).
type Loader struct {
	mu       sync.Mutex
	reg      *MapRegistry
	cat      *DeferredCatalog
	exec     *Executor
	pkgs     *PackageManager
	exts     map[string]*LoadedExtension
	builtins map[string][]Tool
	apps     map[string]AppChannel
	watchers []*fsnotify.Watcher
	closed   bool
}

// NewLoader returns an extension loader over the shared catalog.
func NewLoader(reg *MapRegistry, cat *DeferredCatalog, exec *Executor) *Loader {
	return &Loader{
		reg: reg, cat: cat, exec: exec,
		pkgs:     NewPackageManager(reg, cat, exec),
		exts:     map[string]*LoadedExtension{},
		builtins: map[string][]Tool{},
		apps:     map[string]AppChannel{},
	}
}

// ProvideBuiltin registers the in-process tools of a kind-builtin
// extension manifest (route 4). Activation pairs them by id.
func (l *Loader) ProvideBuiltin(id string, tools []Tool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.builtins[id] = tools
}

// BindApp registers the channel of a route-2 application extension.
// The app registers via capability.register (Section 3.5); events
// flow through capability.event into the same channel.
func (l *Loader) BindApp(id string, ch AppChannel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.apps[id] = ch
}

// LoadDir reads and validates one extension directory without
// activating it.
func (l *Loader) LoadDir(dir string) (*LoadedExtension, error) {
	raw, err := os.ReadFile(filepath.Join(dir, ExtensionFileName))
	if err != nil {
		return nil, fmt.Errorf("tools: extension in %q: %w", dir, err)
	}
	m, err := loadManifestYAML(raw)
	if err != nil {
		return nil, fmt.Errorf("tools: extension in %q: bad manifest: %w", dir, err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &LoadedExtension{Manifest: m, Dir: dir, Status: ExtensionLoaded}, nil
}

// Activate validates the route bindings and registers every tool as a
// deferred catalog entry. Activation never touches the fixed prefix:
// tools resolve through search_tools from the next step.
func (l *Loader) Activate(ext *LoadedExtension) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return fmt.Errorf("tools: loader is closed")
	}
	tools, connector, err := l.bindLocked(ext)
	if err != nil {
		ext.Status = ExtensionFailed
		ext.FailWhy = err.Error()
		return err
	}
	for _, t := range tools {
		def := t.Definition()
		if existing, err := l.reg.Get(def.Name); err == nil {
			_ = existing
			ext.Status = ExtensionFailed
			ext.FailWhy = fmt.Sprintf("catalog already has tool %q", def.Name)
			return fmt.Errorf("tools: extension %q: %s", ext.Manifest.ID, ext.FailWhy)
		}
	}
	registered := []Tool{}
	for _, t := range tools {
		wrapped := &trackedTool{Tool: t, ext: ext}
		if err := l.reg.Register(wrapped); err != nil {
			ext.Status = ExtensionFailed
			ext.FailWhy = err.Error()
			return fmt.Errorf("tools: extension %q: %w", ext.Manifest.ID, err)
		}
		if l.cat != nil {
			l.cat.MarkDeferred(wrapped.Definition().Name)
		}
		registered = append(registered, wrapped)
	}
	ext.mu.Lock()
	ext.tools = registered
	ext.connector = connector
	if ch, ok := l.apps[ext.Manifest.ID]; ok && ext.Manifest.Kind == ExtensionApp {
		ext.channel = ch
	}
	ext.Status = ExtensionActive
	ext.FailWhy = ""
	ext.mu.Unlock()
	if prev, ok := l.exts[ext.Manifest.ID]; ok && prev != ext {
		prev.Status = ExtensionSuperseded
	}
	l.exts[ext.Manifest.ID] = ext
	return nil
}

// bindLocked builds the route tools for an extension.
func (l *Loader) bindLocked(ext *LoadedExtension) ([]Tool, ioCloser, error) {
	m := ext.Manifest
	switch m.Kind {
	case ExtensionBuiltin:
		tools, ok := l.builtins[m.ID]
		if !ok {
			return nil, nil, fmt.Errorf("no in-process tools provided for builtin extension %q (call ProvideBuiltin)", m.ID)
		}
		if len(tools) != len(m.Tools) {
			return nil, nil, fmt.Errorf("builtin extension %q provides %d tools for %d manifest entries", m.ID, len(tools), len(m.Tools))
		}
		return tools, nil, nil
	case ExtensionPackage:
		return l.bindPackage(ext)
	case ExtensionMCP:
		return l.bindMCP(ext)
	case ExtensionApp:
		return l.bindApp(ext)
	default:
		return nil, nil, fmt.Errorf("unknown extension kind %q", m.Kind)
	}
}

// bindPackage runs the route-3 flow: the directory holds tool.json
// with the PackageManifest, validated and test-activated through the
// 13.4 pipeline like any agent-created tool (Section 13.9).
func (l *Loader) bindPackage(ext *LoadedExtension) ([]Tool, ioCloser, error) {
	raw, err := os.ReadFile(filepath.Join(ext.Dir, "tool.json"))
	if err != nil {
		return nil, nil, fmt.Errorf("package extension %q needs tool.json: %w", ext.Manifest.ID, err)
	}
	var pm PackageManifest
	if err := json.Unmarshal(raw, &pm); err != nil {
		return nil, nil, fmt.Errorf("package extension %q: bad tool.json: %w", ext.Manifest.ID, err)
	}
	rec, err := l.pkgs.Create(context.Background(), pm)
	if err != nil {
		return nil, nil, fmt.Errorf("package extension %q failed its tests: %w", ext.Manifest.ID, err)
	}
	t, err := l.reg.Get(rec.Manifest.Name)
	if err != nil {
		return nil, nil, fmt.Errorf("package extension %q: tool %q not registered: %w", ext.Manifest.ID, rec.Manifest.Name, err)
	}
	// The package manager already registered the tool as deferred;
	// activation only tracks it. Unregister first so Activate can wrap
	// it with in-flight tracking through the single path.
	l.reg.Unregister(rec.Manifest.Name)
	if l.cat != nil {
		l.cat.UnmarkDeferred(rec.Manifest.Name)
	}
	return []Tool{t}, nil, nil
}

// Disable retires an extension without deleting it: its tools leave
// the catalog and its connector closes.
func (l *Loader) Disable(id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	ext, ok := l.exts[id]
	if !ok {
		return fmt.Errorf("tools: unknown extension %q", id)
	}
	for _, t := range ext.Tools() {
		l.reg.Unregister(t.Definition().Name)
		if l.cat != nil {
			l.cat.UnmarkDeferred(t.Definition().Name)
		}
	}
	ext.mu.Lock()
	if ext.connector != nil {
		_ = ext.connector.Close()
		ext.connector = nil
	}
	ext.tools = nil
	ext.Status = ExtensionDisabled
	ext.mu.Unlock()
	return nil
}

// Remove disables and forgets an extension.
func (l *Loader) Remove(id string) error {
	if err := l.Disable(id); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.exts, id)
	return nil
}

// Enable reactivates a disabled extension directory.
func (l *Loader) Enable(id string) error {
	l.mu.Lock()
	ext, ok := l.exts[id]
	l.mu.Unlock()
	if !ok {
		return fmt.Errorf("tools: unknown extension %q", id)
	}
	if ext.Status == ExtensionActive {
		return nil
	}
	if ext.Dir == "" {
		return fmt.Errorf("tools: extension %q has no directory to re-enable", id)
	}
	fresh, err := l.LoadDir(ext.Dir)
	if err != nil {
		return err
	}
	return l.Activate(fresh)
}

// Update activates a new manifest version when no calls to the old
// one are in flight; otherwise it reports the pending state and keeps
// the old version serving (Section 14.12).
func (l *Loader) Update(dir string) (string, error) {
	fresh, err := l.LoadDir(dir)
	if err != nil {
		return "", err
	}
	l.mu.Lock()
	old, ok := l.exts[fresh.Manifest.ID]
	l.mu.Unlock()
	if !ok {
		if err := l.Activate(fresh); err != nil {
			return "", err
		}
		return "activated", nil
	}
	if old.InFlight() > 0 {
		return "pending", fmt.Errorf("tools: extension %q has %d calls in flight; new version waits", fresh.Manifest.ID, old.InFlight())
	}
	if err := l.Disable(old.Manifest.ID); err != nil {
		return "", err
	}
	if err := l.Activate(fresh); err != nil {
		return "", err
	}
	return "activated", nil
}

// PrefixHash returns the fixed-prefix hash of the catalog: activating
// or retiring extensions must never change it (Sections 11.6, 13.3).
func (l *Loader) PrefixHash() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cat == nil {
		return ""
	}
	return l.cat.PrefixHash()
}

// ActivateChecked is the store-install gate (Section 14.12): the
// extension activates in an isolated sandbox first and the
// conformance suite runs there. Only a passing suite activates into
// the live catalog; a failing extension never registers. Probes carry
// the behavioral cases per tool.
func (l *Loader) ActivateChecked(ext *LoadedExtension, probes []ConformanceProbe) (ConformanceReport, error) {
	l.mu.Lock()
	builtins := make(map[string][]Tool, len(l.builtins))
	for id, tools := range l.builtins {
		builtins[id] = tools
	}
	apps := make(map[string]AppChannel, len(l.apps))
	for id, ch := range l.apps {
		apps[id] = ch
	}
	l.mu.Unlock()

	sandboxReg := NewMapRegistry()
	if err := RegisterBuiltins(sandboxReg, nil); err != nil {
		return ConformanceReport{}, err
	}
	sandboxCat := NewDeferredCatalog(sandboxReg)
	sandboxExec := NewExecutor(sandboxReg)
	WireSearchCall(sandboxReg, sandboxCat, sandboxExec)
	sandbox := NewLoader(sandboxReg, sandboxCat, sandboxExec)
	for id, tools := range builtins {
		sandbox.ProvideBuiltin(id, tools)
	}
	for id, ch := range apps {
		sandbox.BindApp(id, ch)
	}
	if err := sandbox.Activate(ext); err != nil {
		return ConformanceReport{Extension: ext.Manifest.ID, Passed: false,
			Checks: []ConformanceCheck{{Name: "activate", Passed: false, Detail: err.Error()}}}, err
	}
	report := RunConformance(context.Background(), sandboxReg, sandboxExec, ext, probes)
	// The sandbox wired live connectors (MCP processes, channels);
	// retire them before the verdict, whatever it is.
	_ = sandbox.Disable(ext.Manifest.ID)
	if !report.Passed {
		return report, fmt.Errorf("tools: extension %q failed conformance and was not activated", ext.Manifest.ID)
	}
	if err := l.Activate(ext); err != nil {
		report.Passed = false
		return report, err
	}
	return report, nil
}

// Get returns a loaded extension.
func (l *Loader) Get(id string) (*LoadedExtension, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ext, ok := l.exts[id]
	return ext, ok
}

// List returns the loaded extension ids in sorted order.
func (l *Loader) List() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.exts))
	for id := range l.exts {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// DoctorIssue is one validation, health or version finding.
type DoctorIssue struct {
	Extension string `json:"extension"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

// Doctor reports validation, health and versions of every loaded
// extension (excalibur ext doctor).
func (l *Loader) Doctor() []DoctorIssue {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []DoctorIssue{}
	for id, ext := range l.exts {
		if ext.Status == ExtensionFailed {
			out = append(out, DoctorIssue{Extension: id, Level: "error", Message: ext.FailWhy})
			continue
		}
		if err := ext.Manifest.Validate(); err != nil {
			out = append(out, DoctorIssue{Extension: id, Level: "error", Message: err.Error()})
			continue
		}
		if ext.Manifest.Contract != ExtensionContract {
			out = append(out, DoctorIssue{Extension: id, Level: "error", Message: fmt.Sprintf("unsupported contract %q", ext.Manifest.Contract)})
			continue
		}
		out = append(out, DoctorIssue{Extension: id, Level: "ok",
			Message: fmt.Sprintf("%s %s (%s): %d tools, status %s", id, ext.Manifest.Version, ext.Manifest.Kind, len(ext.Tools()), ext.Status)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Extension < out[j].Extension })
	return out
}

// Watch follows a directory of extensions with fsnotify: new or
// changed manifests load and activate, deleted ones retire. The
// watcher only reacts to excalibur-extension.yaml files. fsnotify is
// not recursive, so the loader watches one level of subdirectories
// and picks up new ones as they appear.
func (l *Loader) Watch(dir string) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("tools: extension watch on %q: %w", dir, err)
	}
	add := func(path string) {
		_ = w.Add(path)
	}
	add(dir)
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				add(filepath.Join(dir, e.Name()))
			}
		}
	}
	l.mu.Lock()
	l.watchers = append(l.watchers, w)
	l.mu.Unlock()
	// Long-lived watcher goroutines go through safego.Go so a panic
	// marks the owner instead of killing the daemon (Section 3.2).
	safego.Go("tools/ext-loader-watch", nil, func() { l.serveWatch(w, dir) })
	return nil
}

func (l *Loader) serveWatch(w *fsnotify.Watcher, dir string) {
	defer w.Close()
	for {
		select {
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			l.mu.Lock()
			closed := l.closed
			l.mu.Unlock()
			if closed {
				return
			}
			// New extension directories join the watch so their
			// manifest lands without a restart. The directory is
			// scanned right away: files written between the mkdir
			// and this watch would otherwise never emit an event.
			if ev.Op&fsnotify.Create != 0 && filepath.Base(ev.Name) != ExtensionFileName {
				if st, err := os.Stat(ev.Name); err == nil && st.IsDir() {
					_ = w.Add(ev.Name)
					if _, err := os.Stat(filepath.Join(ev.Name, ExtensionFileName)); err == nil {
						l.activateBestEffort(ev.Name)
					}
				}
			}
			if filepath.Base(ev.Name) != ExtensionFileName {
				continue
			}
			switch {
			case ev.Op&(fsnotify.Create|fsnotify.Write) != 0:
				sub := filepath.Dir(ev.Name)
				if sub == dir {
					continue
				}
				l.activateBestEffort(sub)
			case ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0:
				_ = l.Remove(filepath.Base(filepath.Dir(ev.Name)))
			}
		case <-w.Errors:
			return
		}
	}
}

// activateBestEffort loads and activates a directory, retrying
// briefly while the writer still holds the manifest.
func (l *Loader) activateBestEffort(dir string) {
	for i := 0; i < 6; i++ {
		if _, err := l.Update(dir); err == nil {
			return
		}
		if l.isNew(dir) {
			if fresh, err := l.LoadDir(dir); err == nil {
				if err := l.Activate(fresh); err == nil {
					return
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (l *Loader) isNew(dir string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, ext := range l.exts {
		if ext.Dir == dir {
			return false
		}
	}
	return true
}

// Close stops the watcher goroutines.
func (l *Loader) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	for _, w := range l.watchers {
		_ = w.Close()
	}
	l.watchers = nil
}
