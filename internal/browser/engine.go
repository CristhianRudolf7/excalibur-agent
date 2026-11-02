package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	pw "github.com/mxschmitt/playwright-go"
)

const inlineBudget = 16 * 1024 // Conservative inline limit below the adapter's 40 KiB default.
type arguments map[string]json.RawMessage

func (a arguments) str(k string) string { var s string; _ = json.Unmarshal(a[k], &s); return s }
func (a arguments) number(k string, fallback float64) float64 {
	var n float64
	if json.Unmarshal(a[k], &n) != nil || n <= 0 {
		return fallback
	}
	return n
}
func (a arguments) action(fallback string) string {
	if s := a.str("action"); s != "" {
		return s
	}
	return fallback
}

type tab struct {
	id, url, title, snapshot, blocked string
	ready                             bool
	emulation                         pw.CDPSession
	page                              pw.Page
	refs                              map[string]Target
	status                            int
	private                           bool
	events                            *eventLog
}
type session struct {
	id, profile, active string
	headed              bool
	tabs                map[string]*tab
}
type task struct {
	result    capabilities.TaskResult
	done      chan struct{}
	cancel    context.CancelFunc
	interrupt func()
	tab       *tab
	session   string
	cancelled bool
}
type outcome struct {
	result capabilities.TaskResult
	err    error
	done   chan struct{}
}

// Engine serializes profile mutations while allowing concurrent task polling and cancellation.
type Engine struct {
	cfg               Config
	opMu              sync.Mutex
	mu                sync.Mutex
	tasks             map[string]*task
	dedup             map[string]*outcome
	sessions          map[string]*session
	routines          map[string][]Step
	driver            *pw.Playwright
	browser           pw.BrowserContext
	seq               int
	closed, suspended bool
	lastUse           time.Time
	stop              chan struct{}
	idleDone          chan struct{}
	secretsMu         sync.RWMutex
	secrets           []string
}

var _ capabilities.Capability = (*Engine)(nil)

// New constructs an engine without starting Chrome. On unsupported platforms,
// Invoke returns errors.ErrUnsupported (the constructor signature has no error).
func New(cfg Config) *Engine {
	cfg = cfg.defaults()
	return &Engine{cfg: cfg, lastUse: cfg.Clock(), tasks: map[string]*task{}, dedup: map[string]*outcome{}, sessions: map[string]*session{}, stop: make(chan struct{})}
}

// Manifest returns the unmodified shared contract.
func (e *Engine) Manifest() capabilities.Manifest { return capabilities.BrowserManifest() }
func (e *Engine) id(prefix string) string         { e.seq++; return fmt.Sprintf("%s-%d", prefix, e.seq) }
func failed(message string, class capabilities.ErrorClass) capabilities.TaskResult {
	return capabilities.Failed("", message, class)
}
func complete(id string, v any) capabilities.TaskResult { return capabilities.Completed(id, v) }
func (e *Engine) clean(s string) string {
	e.secretsMu.RLock()
	defer e.secretsMu.RUnlock()
	for _, secret := range e.secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[secret]")
		}
	}
	return s
}
func clone(r capabilities.TaskResult) capabilities.TaskResult {
	r.Payload = append(json.RawMessage(nil), r.Payload...)
	return r
}

// Invoke deduplicates effects before argument validation, including concurrent repeats.
func (e *Engine) Invoke(op string, raw json.RawMessage, key string) (capabilities.TaskResult, error) {
	if err := supported(); err != nil {
		return capabilities.TaskResult{}, err
	}
	var entry *outcome
	if key != "" {
		e.mu.Lock()
		old := e.dedup[op+"|"+key]
		if old != nil {
			e.mu.Unlock()
			<-old.done
			return clone(old.result), old.err
		}
		entry = &outcome{done: make(chan struct{})}
		e.dedup[op+"|"+key] = entry
		e.mu.Unlock()
	}
	var result capabilities.TaskResult
	var err error
	defer func() {
		if entry != nil {
			e.mu.Lock()
			entry.result, entry.err = clone(result), err
			close(entry.done)
			e.mu.Unlock()
		}
	}()
	var a arguments
	if json.Unmarshal(raw, &a) != nil || a == nil {
		result = failed("browser: invalid arguments", capabilities.ErrInput)
		return result, nil
	}
	known := false
	for _, operation := range e.Manifest().Operations {
		if operation.Name == op {
			known = true
		}
	}
	if !known {
		result = failed(fmt.Sprintf("browser: unknown operation %q", op), capabilities.ErrInput)
		return result, nil
	}
	if op == "browser_navigate" || op == "browser_act" || op == "browser_download" || (op == "browser_routine" && a.action("list") == "run") {
		e.opMu.Lock()
		if e.closed {
			e.opMu.Unlock()
			result = failed("browser: engine is closed", capabilities.ErrPermanent)
			return result, nil
		}
		id := e.id("task")
		e.opMu.Unlock()
		timeout := a.number("timeout_ms", 30000)
		if timeout > 60000 {
			timeout = 60000
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Millisecond)
		t := &task{result: capabilities.TaskResult{TaskID: id, State: capabilities.StateWorking}, done: make(chan struct{}), cancel: cancel}
		e.mu.Lock()
		e.tasks[id] = t
		e.mu.Unlock()
		watchDone := make(chan struct{})
		safego.Go("browser task deadline", nil, func() {
			defer close(watchDone)
			select {
			case <-t.done:
				return
			case <-ctx.Done():
			}
			e.mu.Lock()
			interrupt := t.interrupt
			e.mu.Unlock()
			if interrupt != nil {
				interrupt()
			}
		})
		safego.Go("browser task", func(string, any, []byte) {
			e.finish(t, failed("browser: task failed", capabilities.ErrPermanent))
			cancel()
		}, func() {
			defer cancel()
			e.opMu.Lock()
			defer e.opMu.Unlock()
			r := e.execute(ctx, op, a, t)
			r.TaskID = id
			e.finish(t, r)
			cancel()
			<-watchDone
		})
		result = capabilities.TaskResult{TaskID: id, State: capabilities.StateWorking}
		return result, nil
	}
	e.opMu.Lock()
	defer e.opMu.Unlock()
	result = e.execute(context.Background(), op, a, nil)
	if result.TaskID == "" {
		result.TaskID = e.id("result")
	}
	e.mu.Lock()
	if e.tasks[result.TaskID] == nil {
		e.tasks[result.TaskID] = &task{result: clone(result)}
	}
	e.mu.Unlock()
	return result, err
}
func (e *Engine) finish(t *task, r capabilities.TaskResult) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if t.cancelled {
		r = capabilities.TaskResult{TaskID: t.result.TaskID, State: capabilities.StateCancelled}
	}
	if r.TaskID == "" {
		r.TaskID = t.result.TaskID
	}
	t.result = clone(r)
	t.interrupt = nil
	select {
	case <-t.done:
	default:
		close(t.done)
	}
}

// Poll retains the last outcome, so repeated polls never lose blocked or failed states.
func (e *Engine) Poll(id string) (capabilities.TaskResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if t := e.tasks[id]; t != nil {
		return clone(t.result), nil
	}
	return complete(id, map[string]any{"ok": true}), nil
}

// Cancel interrupts Playwright's auto-wait by closing only the affected page.
// Its remembered URL allows subsequent calls to recreate the tab.
func (e *Engine) Cancel(id string) (capabilities.TaskResult, error) {
	e.mu.Lock()
	t := e.tasks[id]
	var interrupt func()
	if t != nil && (t.result.State == capabilities.StateWorking || t.result.State == capabilities.StateInputRequired) {
		t.cancelled = true
		if t.cancel != nil {
			t.cancel()
		}
		interrupt = t.interrupt
		t.result = capabilities.TaskResult{TaskID: id, State: capabilities.StateCancelled}
	}
	e.mu.Unlock()
	if interrupt != nil {
		interrupt()
	}
	if t != nil {
		return e.Poll(id)
	}
	return capabilities.TaskResult{TaskID: id, State: capabilities.StateCancelled}, nil
}

// Update rechecks the actual page; an answer cannot claim that a block disappeared.
func (e *Engine) Update(id string, _ json.RawMessage) (capabilities.TaskResult, error) {
	e.opMu.Lock()
	defer e.opMu.Unlock()
	e.mu.Lock()
	t := e.tasks[id]
	if t == nil || t.result.State != capabilities.StateInputRequired {
		e.mu.Unlock()
		return failed("browser: task is not input_required", capabilities.ErrInput), nil
	}
	tab := t.tab
	e.mu.Unlock()
	if e.closed || tab == nil || tab.page == nil || tab.page.IsClosed() {
		return failed("browser: unknown tab", capabilities.ErrInput), nil
	}
	e.detect(tab)
	r := complete(id, map[string]any{"resumed": true})
	if tab.blocked != "" {
		r = e.blocked(tab)
		r.TaskID = id
	} else {
		tab.ready = true
	}
	e.mu.Lock()
	t.result = clone(r)
	e.mu.Unlock()
	return r, nil
}
func (e *Engine) attach(t *task, s *session, b *tab) {
	if t == nil {
		return
	}
	e.mu.Lock()
	t.tab, t.session = b, s.id
	page := b.page
	t.interrupt = func() { _ = page.Close() }
	cancelled := t.cancelled
	e.mu.Unlock()
	if cancelled {
		_ = page.Close()
	}
}
func (e *Engine) start() error {
	if e.browser != nil {
		return nil
	}
	if e.cfg.Channel != "chrome" && e.cfg.Channel != "chromium" {
		return errors.New("browser: unsupported channel")
	}
	for _, dir := range []string{e.cfg.ProfileDir, e.cfg.DownloadsDir, e.cfg.RoutinesDir} {
		if dir == "" {
			return errors.New("browser: profile directory required")
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return errors.New("browser: directory unavailable")
		}
	}
	profile, err := filepath.Abs(e.cfg.ProfileDir)
	if err != nil {
		return err
	}
	e.cfg.ProfileDir = profile
	driver, err := pw.Run()
	if err != nil {
		return errors.New("browser: driver unavailable; run browser.Install first")
	}
	launch := pw.BrowserTypeLaunchPersistentContextOptions{
		Channel: pw.String(e.cfg.Channel), Headless: e.cfg.Headless, ChromiumSandbox: e.cfg.Sandbox, IgnoreHttpsErrors: pw.Bool(false), BypassCSP: pw.Bool(false), AcceptDownloads: pw.Bool(true), DownloadsPath: pw.String(e.cfg.DownloadsDir), Timeout: pw.Float(30000),
	}
	if e.cfg.Channel == "chromium" {
		path, err := chromiumPath()
		if err != nil {
			_ = driver.Stop()
			return err
		}
		launch.Channel = nil
		launch.ExecutablePath = pw.String(path)
	}
	browser, err := driver.Chromium.LaunchPersistentContext(profile, launch)
	if err != nil {
		_ = driver.Stop()
		return errors.New("browser: Chrome launch failed; verify channel, installation and sandbox support")
	}
	e.driver, e.browser = driver, browser
	browser.SetDefaultTimeout(10000)
	browser.SetDefaultNavigationTimeout(15000)
	// Enforce navigation policy on redirects, popups, and action-triggered requests too.
	if err := browser.Route("**/*", func(route pw.Route) {
		request := route.Request()
		if request.IsNavigationRequest() && allowedURL(request.URL()) != nil {
			_ = route.Abort()
			return
		}
		_ = route.Continue()
	}); err != nil {
		_ = browser.Close()
		_ = driver.Stop()
		e.driver, e.browser = nil, nil
		return errors.New("browser: navigation policy unavailable")
	}
	for _, page := range browser.Pages() {
		_ = page.Close()
	}
	for _, s := range e.sessions {
		for _, t := range s.tabs {
			if err := e.restore(t); err != nil {
				_ = e.suspend()
				return err
			}
		}
	}
	e.suspended = false
	if e.idleDone == nil {
		e.idleDone = make(chan struct{})
		interval := e.cfg.IdleTimeout / 2
		if interval > time.Second {
			interval = time.Second
		}
		if interval < 10*time.Millisecond {
			interval = 10 * time.Millisecond
		}
		safego.Go("browser idle", nil, func() {
			defer close(e.idleDone)
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-e.stop:
					return
				case <-ticker.C:
					if e.opMu.TryLock() {
						if !e.closed && e.browser != nil && e.cfg.Clock().Sub(e.lastUse) >= e.cfg.IdleTimeout {
							_ = e.suspend()
						}
						e.opMu.Unlock()
					}
				}
			}
		})
	}
	return nil
}
func (e *Engine) restore(t *tab) error {
	page, err := e.browser.NewPage()
	if err != nil {
		return errors.New("browser: could not open tab")
	}
	t.page = page
	t.emulation = nil
	t.events = &eventLog{}
	e.listen(t)
	t.ready, t.snapshot = false, ""
	t.refs = nil
	if t.url != "" && t.url != "about:blank" {
		if allowedURL(t.url) != nil {
			_ = page.Close()
			return errors.New("browser: blocked URL")
		}
		if _, err = page.Goto(t.url); err != nil {
			_ = page.Close()
			t.page = nil
			return errors.New("browser: could not restore tab")
		}
	}
	return nil
}
func (e *Engine) suspend() error {
	if e.browser == nil {
		e.suspended = true
		return nil
	}
	for _, s := range e.sessions {
		for _, t := range s.tabs {
			if t.page != nil && !t.page.IsClosed() {
				t.url = t.page.URL()
			}
			t.ready, t.snapshot = false, ""
			t.refs = nil
		}
	}
	err := e.browser.Close()
	stopErr := e.driver.Stop()
	e.browser, e.driver = nil, nil
	e.suspended = true
	if err != nil {
		return errors.New("browser: close failed")
	}
	return stopErr
}

// Close cancels pending work, joins the idle worker and flushes the persistent profile.
func (e *Engine) Close() error {
	e.mu.Lock()
	ids := []string{}
	pending := []chan struct{}{}
	for id, t := range e.tasks {
		if t.done != nil {
			pending = append(pending, t.done)
		}
		if t.result.State == capabilities.StateWorking {
			ids = append(ids, id)
		}
	}
	e.mu.Unlock()
	for _, id := range ids {
		_, _ = e.Cancel(id)
	}
	e.opMu.Lock()
	if e.closed {
		e.opMu.Unlock()
		return nil
	}
	e.closed = true
	close(e.stop)
	err := e.suspend()
	done := e.idleDone
	e.opMu.Unlock()
	if done != nil {
		<-done
	}
	for _, finished := range pending {
		<-finished
	}
	return err
}
func (e *Engine) blocked(t *tab) capabilities.TaskResult {
	raw, _ := json.Marshal(map[string]any{"tab_id": t.id, "url": e.safeURL(t.url), "blocked": t.blocked})
	return capabilities.TaskResult{State: capabilities.StateInputRequired, Need: t.blocked + ": resolve in headed mode, then continue", Payload: raw}
}
func (e *Engine) detect(t *tab) {
	if t.events != nil {
		t.events.mu.Lock()
		t.status = t.events.navStatus
		t.events.mu.Unlock()
	}
	html, err := t.page.Content()
	if err == nil {
		t.blocked = detectBlock(t.page.URL(), html, t.status)
	}
	t.url = t.page.URL()
	t.title, _ = t.page.Title()
}
