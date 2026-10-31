package capabilities

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Browser capability (Section 14.2). The browser itself is a black box
// built by the user; this file is the harness side of the contract:
// the manifest of deferred tools plus a deterministic simulation that
// enforces the harness navigation rules so backend tests can verify
// them without a real browser.

// Blocked states a page load can report (Section 14.2).
const (
	BlockedCaptcha  = "blocked_captcha"
	BlockedLogin    = "needs_login"
	Blocked2FA      = "needs_2fa"
	BlockedBySite   = "blocked_by_site"
	StaleRefMessage = "stale_ref"
	NotReadyMessage = "not ready: call browser_wait_ready first"
)

// BrowserManifest publishes the 13 browser operations of Section 14.2
// with their Section 13.1 execution classes. Reads are immediate;
// navigation and actions are cooperative: the harness asks the
// capability to stop and re-polls the final state.
func BrowserManifest() Manifest {
	str := func() json.RawMessage { return AnySchema() }
	ops := []Operation{
		Op("browser_open_session", "Open a browser session with a persistent profile, headed or headless", "exec", "keyed", "graceful", false, "browser", []string{"browser", "open", "session", "profile"}, str()),
		Op("browser_close_session", "Close a browser session", "exec", "idempotent", "graceful", false, "browser", []string{"browser", "close", "session"}, str()),
		Op("browser_suspend_session", "Suspend an idle session to free resources keeping its state", "exec", "idempotent", "graceful", false, "browser", []string{"browser", "suspend", "session", "idle"}, str()),
		Op("browser_tabs", "List, open, switch and close tabs of a session", "exec", "keyed", "cooperative", false, "browser", []string{"browser", "tabs", "list", "open", "switch"}, str()),
		Op("browser_navigate", "Go to a URL, back, forward or reload in a tab", "write_external", "keyed", "cooperative", false, "browser", []string{"browser", "navigate", "url", "back", "reload"}, str()),
		Op("browser_wait_ready", "Wait until the needed content finished loading by network, DOM stability or expected selector", "read_only", "pure", "immediate", false, "browser", []string{"browser", "wait", "ready", "load"}, str()),
		Op("browser_snapshot", "Capture screenshot, full DOM tree and the list of interactive elements with refs", "read_only", "pure", "immediate", false, "browser", []string{"browser", "snapshot", "dom", "screenshot", "elements"}, str()),
		Op("browser_act", "Act on a snapshot ref: click, type, select, hover, scroll, press, upload, drag", "write_external", "keyed", "cooperative", false, "browser", []string{"browser", "act", "click", "type", "element"}, str()),
		Op("browser_download", "Download a file to the blob store", "write_external", "keyed", "cooperative", false, "browser", []string{"browser", "download", "file"}, str()),
		Op("browser_devtools", "Read console, network requests and performance metrics of a tab", "read_only", "pure", "immediate", false, "browser", []string{"browser", "devtools", "console", "network"}, str()),
		Op("browser_emulate", "Emulate a screen size or device to check a page", "exec", "idempotent", "immediate", false, "browser", []string{"browser", "emulate", "device", "viewport"}, str()),
		Op("browser_status", "Show sessions, tabs, RAM and idle time of the browser", "read_only", "pure", "immediate", true, "browser", []string{"browser", "status", "sessions"}, str()),
		Op("browser_routine", "Record, list and replay click and form routines with per-step assertions", "exec", "keyed", "cooperative", false, "browser", []string{"browser", "routine", "replay", "record", "macro"}, str()),
	}
	return Manifest{
		ID: "browser", Version: "1",
		Operations: ops,
		Platforms:  []string{"linux"},
		Profiles:   []string{"default", "delegate", "eval"},
		Resources:  ResourceLimits{MaxSessions: 8, Concurrency: 4},
	}
}

type browserTab struct {
	id         string
	url        string
	title      string
	ready      bool
	blocked    string
	snapshotID string
	refs       []string
	pageText   string
}

type browserSession struct {
	id        string
	profile   string
	headed    bool
	suspended bool
	tabs      map[string]*browserTab
	active    string
}

type routineStep struct {
	Action string `json:"action"`
	Ref    string `json:"ref"`
	Value  string `json:"value,omitempty"`
	Assert string `json:"assert,omitempty"`
}

// BrowserFake is the deterministic browser simulation (Section
// 14.13): it enforces wait-before-act, snapshot-bound refs, blocked
// states via input_required, and deterministic routine replay with
// per-step assertions.
type BrowserFake struct {
	mu       sync.Mutex
	manifest Manifest
	sessions map[string]*browserSession
	routines map[string][]routineStep
	dedup    KeyDedup
	calls    map[string]int
	effects  map[string]int
	seq      int
	tasks    map[string]TaskResult
	next     map[string][]TaskResult
}

// NewBrowserFake returns a browser simulation.
func NewBrowserFake() *BrowserFake {
	return &BrowserFake{
		manifest: BrowserManifest(),
		sessions: map[string]*browserSession{},
		routines: map[string][]routineStep{},
		calls:    map[string]int{},
		effects:  map[string]int{},
		tasks:    map[string]TaskResult{},
		next:     map[string][]TaskResult{},
	}
}

// Manifest implements Capability.
func (b *BrowserFake) Manifest() Manifest { return b.manifest }

// Calls counts Invoke calls per operation.
func (b *BrowserFake) Calls(op string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls[op]
}

// Effects counts first-effect executions per operation: repeats with
// the same idempotency key do not increment it.
func (b *BrowserFake) Effects(op string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.effects[op]
}

// SetPageText fixes the simulated visible text of a tab for routine
// assertions and snapshot titles.
func (b *BrowserFake) SetPageText(session, tab, text string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := b.sessions[session]; ok {
		if t, ok := s.tabs[tab]; ok {
			t.pageText = text
		}
	}
}

// Invoke implements Capability.
func (b *BrowserFake) Invoke(operation string, args json.RawMessage, key string) (TaskResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls[operation]++
	switch operation {
	case "browser_open_session":
		return b.openSession(args, key)
	case "browser_close_session":
		return b.closeSession(args)
	case "browser_suspend_session":
		return b.suspendSession(args)
	case "browser_tabs":
		return b.tabs(args, key)
	case "browser_navigate":
		return b.navigate(args, key)
	case "browser_wait_ready":
		return b.waitReady(args)
	case "browser_snapshot":
		return b.snapshot(args)
	case "browser_act":
		return b.act(args, key)
	case "browser_download":
		return b.download(args, key)
	case "browser_devtools":
		return b.devtools(args)
	case "browser_emulate":
		return b.emulate(args)
	case "browser_status":
		return b.status()
	case "browser_routine":
		return b.routine(args, key)
	default:
		return Failed("", fmt.Sprintf("browser: unknown operation %q", operation), ErrInput), nil
	}
}

func (b *BrowserFake) effect(op, key string) {
	if b.dedup.First(op + "|" + key) {
		b.effects[op]++
	}
}

func (b *BrowserFake) openSession(args json.RawMessage, key string) (TaskResult, error) {
	if len(b.sessions) >= 8 {
		return Failed("", "browser: session capacity reached (8)", ErrTransient), nil
	}
	b.effect("browser_open_session", key)
	b.seq++
	id := fmt.Sprintf("sess-%d", b.seq)
	profile := argString(args, "profile")
	if profile == "" {
		profile = "default"
	}
	headed := argString(args, "mode") == "headed"
	b.sessions[id] = &browserSession{id: id, profile: profile, headed: headed, tabs: map[string]*browserTab{}}
	return Completed(id, map[string]any{"session_id": id, "profile": profile}), nil
}

func (b *BrowserFake) closeSession(args json.RawMessage) (TaskResult, error) {
	id := argString(args, "session_id")
	if id == "" {
		return Failed("", "browser: session_id is required", ErrInput), nil
	}
	delete(b.sessions, id)
	return Completed(id, map[string]any{"session_id": id, "state": "closed"}), nil
}

func (b *BrowserFake) suspendSession(args json.RawMessage) (TaskResult, error) {
	s, ok := b.sessionOf(args)
	if !ok {
		return Failed("", "browser: unknown session", ErrInput), nil
	}
	s.suspended = true
	return Completed(s.id, map[string]any{"session_id": s.id, "state": "suspended"}), nil
}

func (b *BrowserFake) sessionOf(args json.RawMessage) (*browserSession, bool) {
	id := argString(args, "session_id")
	s, ok := b.sessions[id]
	return s, ok
}

func (b *BrowserFake) tabOf(s *browserSession, args json.RawMessage) (*browserTab, string, bool) {
	id := argString(args, "tab_id")
	if id == "" {
		id = s.active
	}
	t, ok := s.tabs[id]
	return t, id, ok
}

func (b *BrowserFake) tabs(args json.RawMessage, key string) (TaskResult, error) {
	s, ok := b.sessionOf(args)
	if !ok {
		return Failed("", "browser: unknown session", ErrInput), nil
	}
	if s.suspended {
		return Failed("", "browser: session is suspended", ErrInput), nil
	}
	action := argString(args, "action")
	if action == "" {
		action = "list"
	}
	switch action {
	case "list":
		out := []map[string]any{}
		for id, t := range s.tabs {
			out = append(out, map[string]any{"tab_id": id, "url": t.url, "title": t.title})
		}
		return Completed(s.id, map[string]any{"tabs": out}), nil
	case "new":
		b.effect("browser_tabs", key)
		b.seq++
		id := fmt.Sprintf("tab-%d", b.seq)
		url := argString(args, "url")
		s.tabs[id] = &browserTab{id: id, url: url, title: titleOf(url)}
		s.active = id
		return Completed(id, map[string]any{"tab_id": id, "url": url}), nil
	case "switch":
		t, id, ok := b.tabOf(s, args)
		if !ok {
			return Failed("", "browser: unknown tab", ErrInput), nil
		}
		s.active = id
		return Completed(id, map[string]any{"tab_id": id, "url": t.url}), nil
	case "close":
		_, id, ok := b.tabOf(s, args)
		if !ok {
			return Failed("", "browser: unknown tab", ErrInput), nil
		}
		delete(s.tabs, id)
		if s.active == id {
			s.active = ""
		}
		return Completed(s.id, map[string]any{"tab_id": id, "state": "closed"}), nil
	default:
		return Failed("", fmt.Sprintf("browser: unknown tabs action %q", action), ErrInput), nil
	}
}

func (b *BrowserFake) navigate(args json.RawMessage, key string) (TaskResult, error) {
	s, ok := b.sessionOf(args)
	if !ok {
		return Failed("", "browser: unknown session", ErrInput), nil
	}
	t, id, ok := b.tabOf(s, args)
	if !ok {
		return Failed("", "browser: unknown tab", ErrInput), nil
	}
	action := argString(args, "action")
	if action == "" {
		action = "go"
	}
	b.effect("browser_navigate", key)
	switch action {
	case "go":
		url := argString(args, "url")
		if url == "" {
			return Failed("", "browser: url is required", ErrInput), nil
		}
		t.url = url
		t.title = titleOf(url)
		t.ready = false
		t.snapshotID = ""
		t.blocked = blockFor(url)
	case "back", "forward", "reload":
		t.ready = false
		t.snapshotID = ""
	default:
		return Failed("", fmt.Sprintf("browser: unknown navigate action %q", action), ErrInput), nil
	}
	if t.blocked != "" {
		taskID := nextID("nav", &b.seq)
		b.tasks[taskID] = TaskResult{TaskID: taskID, State: StateInputRequired,
			Need: t.blocked + ": resolve in headed mode, then continue", Payload: marshal(blockedPayload(t))}
		return TaskResult{TaskID: taskID, State: StateWorking}, nil
	}
	return Completed(id, map[string]any{"tab_id": id, "url": t.url, "loading": true}), nil
}

func (b *BrowserFake) waitReady(args json.RawMessage) (TaskResult, error) {
	s, ok := b.sessionOf(args)
	if !ok {
		return Failed("", "browser: unknown session", ErrInput), nil
	}
	t, id, ok := b.tabOf(s, args)
	if !ok {
		return Failed("", "browser: unknown tab", ErrInput), nil
	}
	if t.blocked != "" {
		taskID := nextID("wait", &b.seq)
		b.tasks[taskID] = TaskResult{TaskID: taskID, State: StateInputRequired,
			Need: t.blocked + ": resolve in headed mode, then continue"}
		return TaskResult{TaskID: taskID, State: StateWorking}, nil
	}
	t.ready = true
	return Completed(id, map[string]any{"tab_id": id, "ready": true}), nil
}

func (b *BrowserFake) snapshot(args json.RawMessage) (TaskResult, error) {
	s, ok := b.sessionOf(args)
	if !ok {
		return Failed("", "browser: unknown session", ErrInput), nil
	}
	t, id, ok := b.tabOf(s, args)
	if !ok {
		return Failed("", "browser: unknown tab", ErrInput), nil
	}
	if !t.ready {
		return Failed(id, NotReadyMessage, ErrInput), nil
	}
	b.seq++
	snap := fmt.Sprintf("snap-%d", b.seq)
	t.snapshotID = snap
	t.refs = []string{"r1", "r2", "r3"}
	elements := []map[string]any{
		{"ref": "r1", "role": "button", "name": "Search", "type": "button", "visible": true, "enabled": true},
		{"ref": "r2", "role": "textbox", "name": "Query", "type": "text", "visible": true, "enabled": true},
		{"ref": "r3", "role": "link", "name": "Results", "type": "link", "visible": true, "enabled": true},
	}
	return Completed(snap, map[string]any{
		"snapshot_id": snap, "tab_id": id, "url": t.url, "title": t.title,
		"loading": false, "interactive": elements, "capture_ref": "blob:shot-" + snap,
	}), nil
}

func (b *BrowserFake) act(args json.RawMessage, key string) (TaskResult, error) {
	s, ok := b.sessionOf(args)
	if !ok {
		return Failed("", "browser: unknown session", ErrInput), nil
	}
	t, _, ok := b.tabOf(s, args)
	if !ok {
		return Failed("", "browser: unknown tab", ErrInput), nil
	}
	if !t.ready {
		return Failed(t.snapshotID, NotReadyMessage, ErrInput), nil
	}
	if got := argString(args, "snapshot_id"); got == "" || got != t.snapshotID {
		return Failed(t.snapshotID, StaleRefMessage+": snapshot changed, observe again", ErrInput), nil
	}
	ref := argString(args, "ref")
	known := false
	for _, r := range t.refs {
		if r == ref {
			known = true
		}
	}
	if !known {
		return Failed(t.snapshotID, StaleRefMessage+": unknown ref "+ref, ErrInput), nil
	}
	action := argString(args, "action")
	if action == "" {
		return Failed(t.snapshotID, "browser: action is required", ErrInput), nil
	}
	b.effect("browser_act", key)
	t.snapshotID = ""
	t.ready = false
	return Completed(t.id, map[string]any{"action": action, "ref": ref, "navigated": false, "resnapshot": true}), nil
}

func (b *BrowserFake) download(args json.RawMessage, key string) (TaskResult, error) {
	s, ok := b.sessionOf(args)
	if !ok {
		return Failed("", "browser: unknown session", ErrInput), nil
	}
	if _, _, ok := b.tabOf(s, args); !ok {
		return Failed("", "browser: unknown tab", ErrInput), nil
	}
	b.effect("browser_download", key)
	b.seq++
	return Completed(fmt.Sprintf("blob-dl-%d", b.seq), map[string]any{"blob": fmt.Sprintf("blob-dl-%d", b.seq)}), nil
}

func (b *BrowserFake) devtools(args json.RawMessage) (TaskResult, error) {
	s, ok := b.sessionOf(args)
	if !ok {
		return Failed("", "browser: unknown session", ErrInput), nil
	}
	if _, _, ok := b.tabOf(s, args); !ok {
		return Failed("", "browser: unknown tab", ErrInput), nil
	}
	kind := argString(args, "type")
	if kind == "" {
		kind = "console"
	}
	return Completed(s.id, map[string]any{"type": kind, "entries": []string{}}), nil
}

func (b *BrowserFake) emulate(args json.RawMessage) (TaskResult, error) {
	s, ok := b.sessionOf(args)
	if !ok {
		return Failed("", "browser: unknown session", ErrInput), nil
	}
	device := argString(args, "device")
	if device == "" {
		device = argString(args, "viewport")
	}
	return Completed(s.id, map[string]any{"emulated": device}), nil
}

func (b *BrowserFake) status() (TaskResult, error) {
	sessions := []map[string]any{}
	for id, s := range b.sessions {
		sessions = append(sessions, map[string]any{
			"session_id": id, "tabs": len(s.tabs), "suspended": s.suspended, "headed": s.headed,
		})
	}
	return Completed("browser", map[string]any{"sessions": sessions, "ram_mb": 64, "max_sessions": 8}), nil
}

func (b *BrowserFake) routine(args json.RawMessage, key string) (TaskResult, error) {
	action := argString(args, "action")
	if action == "" {
		action = "list"
	}
	switch action {
	case "list":
		names := []string{}
		for name := range b.routines {
			names = append(names, name)
		}
		return Completed("routines", map[string]any{"routines": names}), nil
	case "record":
		name := argString(args, "routine")
		if name == "" {
			return Failed("", "browser: routine name is required", ErrInput), nil
		}
		var steps []routineStep
		if err := json.Unmarshal(argsValue(args, "steps"), &steps); err != nil || len(steps) == 0 {
			return Failed("", "browser: routine needs at least one step", ErrInput), nil
		}
		b.routines[name] = steps
		return Completed(name, map[string]any{"routine": name, "steps": len(steps)}), nil
	case "run":
		name := argString(args, "routine")
		steps, ok := b.routines[name]
		if !ok {
			return Failed("", fmt.Sprintf("browser: unknown routine %q", name), ErrInput), nil
		}
		s, ok := b.sessionOf(args)
		if !ok {
			return Failed("", "browser: unknown session", ErrInput), nil
		}
		t, _, ok := b.tabOf(s, args)
		if !ok {
			return Failed("", "browser: unknown tab", ErrInput), nil
		}
		b.effect("browser_routine", key)
		for i, st := range steps {
			if st.Assert != "" && !strings.Contains(t.pageText, st.Assert) {
				return Failed(fmt.Sprintf("%s-step-%d", name, i),
					fmt.Sprintf("routine %q step %d assertion failed: page does not contain %q; control returns to the agent at this step", name, i, st.Assert),
					ErrInput), nil
			}
		}
		return Completed(name, map[string]any{"routine": name, "steps_run": len(steps)}), nil
	default:
		return Failed("", fmt.Sprintf("browser: unknown routine action %q", action), ErrInput), nil
	}
}

// Poll implements Capability: resumes blocked navigations and waits.
func (b *BrowserFake) Poll(taskID string) (TaskResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if queued, ok := b.next[taskID]; ok && len(queued) > 0 {
		res := queued[0]
		b.next[taskID] = queued[1:]
		if res.TaskID == "" {
			res.TaskID = taskID
		}
		return res, nil
	}
	if res, ok := b.tasks[taskID]; ok {
		delete(b.tasks, taskID)
		return res, nil
	}
	return Completed(taskID, map[string]any{"ok": true}), nil
}

// Cancel implements Capability: cooperative cancel is recorded and the
// final state is re-polled by the harness.
func (b *BrowserFake) Cancel(taskID string) (TaskResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.tasks[taskID]; ok {
		delete(b.tasks, taskID)
		return TaskResult{TaskID: taskID, State: StateCancelled}, nil
	}
	return TaskResult{TaskID: taskID, State: StateCancelled}, nil
}

// Update implements Capability: answers an input_required task (login,
// CAPTCHA, 2FA, site block) and resumes the navigation.
func (b *BrowserFake) Update(taskID string, _ json.RawMessage) (TaskResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.tasks, taskID)
	// The simulated user resolved the block in headed mode: clear every
	// blocked tab so the run continues from the current page.
	for _, s := range b.sessions {
		for _, t := range s.tabs {
			t.blocked = ""
			t.ready = true
		}
	}
	return Completed(taskID, map[string]any{"resumed": true}), nil
}

func argsValue(args json.RawMessage, name string) json.RawMessage {
	var decoded map[string]any
	if err := json.Unmarshal(args, &decoded); err != nil {
		return nil
	}
	raw, err := json.Marshal(decoded[name])
	if err != nil {
		return nil
	}
	return raw
}

func titleOf(url string) string {
	if url == "" {
		return "blank"
	}
	return "page:" + url
}

func blockFor(url string) string {
	u := strings.ToLower(url)
	switch {
	case strings.Contains(u, "captcha"):
		return BlockedCaptcha
	case strings.Contains(u, "login"):
		return BlockedLogin
	case strings.Contains(u, "2fa"):
		return Blocked2FA
	case strings.Contains(u, "blocked"):
		return BlockedBySite
	}
	return ""
}

func blockedPayload(t *browserTab) map[string]any {
	return map[string]any{"tab_id": t.id, "url": t.url, "blocked": t.blocked}
}

func marshal(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}
