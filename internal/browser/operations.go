package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	pw "github.com/mxschmitt/playwright-go"
)

func (e *Engine) execute(ctx context.Context, op string, a arguments, job *task) capabilities.TaskResult {
	defer e.refreshLive()
	if e.closed {
		return failed("browser: engine is closed", capabilities.ErrPermanent)
	}
	if ctx.Err() == context.DeadlineExceeded {
		return failed("browser: task timed out", capabilities.ErrTransient)
	}
	if ctx.Err() != nil {
		return capabilities.TaskResult{State: capabilities.StateCancelled}
	}
	if op == "browser_status" {
		return e.status()
	}
	e.lastUse = e.cfg.Clock()
	defer func() { e.lastUse = e.cfg.Clock() }()
	if op == "browser_routine" && a.action("list") != "run" {
		return e.routine(ctx, a, nil)
	}
	if op == "browser_close_session" {
		id := a.str("session_id")
		if id == "" {
			return failed("browser: session_id is required", capabilities.ErrInput)
		}
		if s := e.sessions[id]; s != nil {
			for _, t := range s.tabs {
				if t.page != nil && !t.page.IsClosed() {
					_ = t.page.Close()
				}
			}
			delete(e.sessions, id)
			e.refreshLive()
			e.emitLive(LiveEvent{Type: "session_closed", Session: id})
		}
		if len(e.sessions) == 0 {
			_ = e.suspend()
		}
		return complete(id, map[string]any{"session_id": id, "state": "closed"})
	}
	if op == "browser_suspend_session" {
		s := e.sessions[a.str("session_id")]
		if s == nil {
			return failed("browser: unknown session", capabilities.ErrInput)
		}
		if err := e.suspend(); err != nil {
			return failed("browser: suspend failed", capabilities.ErrTransient)
		}
		return complete(s.id, map[string]any{"session_id": s.id, "state": "suspended"})
	}
	if op == "browser_open_session" {
		if len(e.sessions) >= 8 {
			return failed("browser: session capacity reached (8)", capabilities.ErrTransient)
		}
		if mode := a.str("mode"); mode != "" {
			if mode != "headed" && mode != "headless" {
				return failed("browser: unknown session mode", capabilities.ErrInput)
			}
			headless := mode == "headless"
			if e.browser != nil && headless != *e.cfg.Headless {
				return failed("browser: single profile already uses another mode", capabilities.ErrInput)
			}
			e.cfg.Headless = boolPtr(headless)
		}
		if err := e.start(); err != nil {
			return failed(err.Error(), capabilities.ErrPermanent)
		}
		id := e.id("sess")
		profile := a.str("profile")
		if profile == "" {
			profile = "default"
		}
		e.sessions[id] = &session{id: id, profile: profile, headed: !*e.cfg.Headless, tabs: map[string]*tab{}}
		e.refreshLive()
		e.emitLive(LiveEvent{Type: "session_opened", Session: id})
		return complete(id, map[string]any{"session_id": id, "profile": profile})
	}
	s := e.sessions[a.str("session_id")]
	if s == nil {
		return failed("browser: unknown session", capabilities.ErrInput)
	}
	if err := e.start(); err != nil {
		return failed(err.Error(), capabilities.ErrPermanent)
	}
	if op == "browser_tabs" {
		return e.tabs(s, a)
	}
	id := a.str("tab_id")
	if id == "" {
		id = s.active
	}
	t := s.tabs[id]
	if t == nil {
		return failed("browser: unknown tab", capabilities.ErrInput)
	}
	if t.page == nil || t.page.IsClosed() {
		if err := e.restore(t); err != nil {
			return failed(err.Error(), capabilities.ErrTransient)
		}
	}
	e.attach(job, s, t)
	// Each task's deadline also bounds Playwright auto-waits.
	timeout := a.number("timeout_ms", 10000)
	if timeout > 60000 {
		timeout = 60000
	}
	t.page.SetDefaultTimeout(timeout)
	t.page.SetDefaultNavigationTimeout(timeout)
	e.refreshLive()
	switch op {
	case "browser_navigate":
		return e.observed(ctx, t, a, "navigate", func() capabilities.TaskResult { return e.navigate(t, a) })
	case "browser_wait_ready":
		r := e.observed(ctx, t, a, "wait", func() capabilities.TaskResult { return e.wait(t, a) })
		if r.State == capabilities.StateInputRequired {
			taskID := e.id("wait")
			r.TaskID = taskID
			e.mu.Lock()
			e.tasks[taskID] = &task{result: clone(r), tab: t, session: s.id}
			e.mu.Unlock()
			return capabilities.TaskResult{TaskID: taskID, State: capabilities.StateWorking}
		}
		return r
	case "browser_snapshot":
		return e.observed(ctx, t, a, "read", func() capabilities.TaskResult { return e.snapshot(ctx, t) })
	case "browser_act":
		return e.act(ctx, t, a)
	case "browser_download":
		return e.download(ctx, t, a)
	case "browser_devtools":
		return e.devtools(t, a)
	case "browser_emulate":
		return e.emulate(t, a)
	case "browser_routine":
		return e.routine(ctx, a, t)
	}
	return failed("browser: unsupported operation", capabilities.ErrPermanent)
}
func (e *Engine) tabs(s *session, a arguments) capabilities.TaskResult {
	switch a.action("list") {
	case "list":
		out := []map[string]any{}
		ids := []string{}
		for id := range s.tabs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			t := s.tabs[id]
			if t.page != nil && !t.page.IsClosed() {
				t.url = t.page.URL()
				t.title, _ = t.page.Title()
			}
			out = append(out, map[string]any{"tab_id": id, "url": e.safeURL(t.url), "title": e.clean(t.title)})
		}
		return complete(s.id, map[string]any{"tabs": out})
	case "new", "open":
		count := 0
		for _, session := range e.sessions {
			count += len(session.tabs)
		}
		if count >= e.cfg.MaxTabs {
			return failed("browser: tab capacity reached", capabilities.ErrTransient)
		}
		rawURL := a.str("url")
		if rawURL == "" {
			rawURL = "about:blank"
		}
		if err := allowedURL(rawURL); err != nil {
			return failed(err.Error(), capabilities.ErrPermanent)
		}
		t := &tab{id: e.id("tab"), url: rawURL, session: s.id}
		if err := e.restore(t); err != nil {
			return failed(err.Error(), capabilities.ErrTransient)
		}
		s.tabs[t.id] = t
		s.active = t.id
		e.detect(t)
		return complete(t.id, map[string]any{"tab_id": t.id, "url": e.safeURL(t.page.URL())})
	case "switch", "close":
		id := a.str("tab_id")
		if id == "" {
			id = s.active
		}
		t := s.tabs[id]
		if t == nil {
			return failed("browser: unknown tab", capabilities.ErrInput)
		}
		if a.action("") == "switch" {
			s.active = id
			if err := t.page.BringToFront(); err != nil {
				return failed("browser: tab unavailable", capabilities.ErrTransient)
			}
			return complete(id, map[string]any{"tab_id": id, "url": e.safeURL(t.page.URL())})
		}
		if t.page != nil {
			_ = t.page.Close()
		}
		delete(s.tabs, id)
		if s.active == id {
			s.active = ""
		}
		return complete(s.id, map[string]any{"tab_id": id, "state": "closed"})
	default:
		return failed(fmt.Sprintf("browser: unknown tabs action %q", a.str("action")), capabilities.ErrInput)
	}
}
func (e *Engine) navigate(t *tab, a arguments) capabilities.TaskResult {
	action := a.action("go")
	if action == "go" || action == "url" {
		if a.str("url") == "" {
			return failed("browser: url is required", capabilities.ErrInput)
		}
		if err := allowedURL(a.str("url")); err != nil {
			return failed(err.Error(), capabilities.ErrPermanent)
		}
	}
	t.ready, t.snapshot = false, ""
	t.refs = nil
	t.blocked = ""
	var response pw.Response
	var err error
	switch action {
	case "go", "url":
		response, err = t.page.Goto(a.str("url"))
	case "back":
		response, err = t.page.GoBack()
	case "forward":
		response, err = t.page.GoForward()
	case "reload":
		response, err = t.page.Reload()
	default:
		return failed(fmt.Sprintf("browser: unknown navigate action %q", action), capabilities.ErrInput)
	}
	if err != nil {
		return failed("browser: navigation failed or timed out", capabilities.ErrTransient)
	}
	t.status = 0
	if response != nil {
		t.status = response.Status()
	}
	e.detect(t)
	if t.blocked != "" {
		return e.blocked(t)
	}
	return complete(t.id, map[string]any{"tab_id": t.id, "url": e.safeURL(t.url), "title": e.clean(t.title), "status": t.status, "loading": true})
}

func (e *Engine) wait(t *tab, a arguments) capabilities.TaskResult {
	e.detect(t)
	if t.blocked != "" && t.blocked != capabilities.BlockedLogin && t.blocked != capabilities.Blocked2FA {
		return e.blocked(t)
	}
	mode := a.str("mode")
	if mode == "" {
		mode = "load"
	}
	timeout := a.number("timeout_ms", 10000)
	if timeout > 60000 {
		timeout = 60000
	}
	var err error
	switch mode {
	case "load":
		err = t.page.WaitForLoadState(pw.PageWaitForLoadStateOptions{State: pw.LoadStateLoad, Timeout: pw.Float(timeout)})
	case "network_idle":
		err = t.page.WaitForLoadState(pw.PageWaitForLoadStateOptions{State: pw.LoadStateNetworkidle, Timeout: pw.Float(timeout)})
	case "selector":
		if a.str("selector") == "" {
			return failed("browser: selector is required", capabilities.ErrInput)
		}
		err = t.page.Locator(a.str("selector")).WaitFor(pw.LocatorWaitForOptions{Timeout: pw.Float(timeout)})
	case "dom_stable":
		// A bounded browser-side observer cannot survive an operation timeout.
		ms := a.number("stable_ms", 200)
		if ms >= timeout {
			return failed("browser: DOM stability timed out", capabilities.ErrTransient)
		}
		script := `opts => new Promise(resolve => { let timer; const finish = value => { clearTimeout(timer); clearTimeout(deadline); observer.disconnect(); resolve(value); }; const reset = () => { clearTimeout(timer); timer = setTimeout(() => finish(true),opts.ms); }; const observer = new MutationObserver(reset); const deadline = setTimeout(() => finish(false),opts.timeout); observer.observe(document,{subtree:true,childList:true,attributes:true,characterData:true}); reset(); })`
		var stable any
		stable, err = t.page.Evaluate(script, map[string]any{"ms": ms, "timeout": timeout})
		if err == nil && stable != true {
			return failed("browser: DOM stability timed out", capabilities.ErrTransient)
		}
	default:
		return failed("browser: unknown readiness mode", capabilities.ErrInput)
	}
	if err != nil {
		return failed("browser: readiness timed out or failed", capabilities.ErrTransient)
	}
	e.detect(t)
	if t.blocked != "" {
		// Login forms remain observable and actionable through secure references.
		// Readiness does not claim authentication or clear the detected block.
		t.ready = t.blocked == capabilities.BlockedLogin || t.blocked == capabilities.Blocked2FA
		return e.blocked(t)
	}
	t.ready = true
	return complete(t.id, map[string]any{"tab_id": t.id, "ready": true})
}
func (e *Engine) ref(t *tab, a arguments) (pw.Locator, capabilities.TaskResult, bool) {
	if !t.ready {
		return nil, failed(capabilities.NotReadyMessage, capabilities.ErrInput), false
	}
	if a.str("snapshot_id") == "" || a.str("snapshot_id") != t.snapshot {
		return nil, failed(capabilities.StaleRefMessage+": snapshot changed, observe again", capabilities.ErrInput), false
	}
	if _, ok := t.refs[a.str("ref")]; !ok {
		return nil, failed(capabilities.StaleRefMessage+": unknown ref "+a.str("ref"), capabilities.ErrInput), false
	}
	return t.page.Locator(`[data-excalibur-ref="` + t.snapshot + "-" + a.str("ref") + `"]`), capabilities.TaskResult{}, true
}
func (e *Engine) action(ctx context.Context, t *tab, locator pw.Locator, action, value string, a arguments) error {
	return e.actionWithTyping(ctx, t, locator, action, value, a, nil)
}
func (e *Engine) actionWithTyping(ctx context.Context, t *tab, locator pw.Locator, action, value string, a arguments, plan *liveTyping) error {
	switch action {
	case "click":
		return locator.Click()
	case "type":
		if strings.HasPrefix(value, "secret://") {
			u, _ := url.Parse(t.page.URL())
			if e.cfg.Secrets == nil || u == nil {
				return policyError("browser: secret unavailable for this host")
			}
			resolved, err := e.cfg.Secrets.Resolve(ctx, strings.TrimPrefix(value, "secret://"), u.Hostname())
			if err != nil || resolved == "" {
				return policyError("browser: secret unavailable for this host")
			}
			// Bind the secret to an element in this document, so auto-wait cannot
			// retarget a same-named input after a cross-host navigation.
			//lint:ignore SA1019 A document-bound handle prevents secret auto-wait from retargeting after navigation.
			handle, handleErr := locator.ElementHandle()
			if handleErr != nil || handle == nil {
				return inputError("browser: secret target unavailable")
			}
			defer handle.Dispose()
			current, _ := url.Parse(t.page.URL())
			if current == nil || !strings.EqualFold(current.Hostname(), u.Hostname()) {
				return policyError("browser: secret destination changed")
			}
			e.secretsMu.Lock()
			e.secrets = append(e.secrets, resolved)
			e.secretsMu.Unlock()
			t.private = true
			if plan != nil {
				if plan.withhold && t.live != nil {
					t.live.withheld.Store(true)
				}
				if plan.animate {
					//lint:ignore SA1019 Secret entry stays bound to the original document.
					if err := handle.Fill(""); err != nil {
						return err
					}
					delay := typingDelay(resolved)
					//lint:ignore SA1019 Secret entry stays bound to the original document.
					return handle.Type(resolved, pw.ElementHandleTypeOptions{Delay: pw.Float(float64(delay) / float64(time.Millisecond))})
				}
			}
			//lint:ignore SA1019 Fill must target the original document-bound element for secret domain safety.
			return handle.Fill(resolved)
		}
		if plan != nil && plan.animate {
			if err := locator.Fill(""); err == nil {
				err = locator.PressSequentially(value, pw.LocatorPressSequentiallyOptions{Delay: pw.Float(float64(plan.delay) / float64(time.Millisecond))})
				if err == nil {
					actual, verifyErr := locator.InputValue(pw.LocatorInputValueOptions{Timeout: pw.Float(1000)})
					if verifyErr == nil && actual == value {
						return nil
					}
				}
			}
		}
		return locator.Fill(value)
	case "select":
		_, err := locator.SelectOption(pw.SelectOptionValues{Values: &[]string{value}})
		return err
	case "hover":
		return locator.Hover()
	case "scroll":
		return locator.ScrollIntoViewIfNeeded()
	case "press":
		return locator.Press(value)
	case "upload":
		path, err := allowedUpload(value, e.cfg.AllowedUploadDirs)
		if err != nil {
			return policyError("browser: upload blocked by policy")
		}
		return locator.SetInputFiles(path)
	case "drag":
		target := a.str("target_ref")
		if _, ok := t.refs[target]; !ok {
			return inputError(capabilities.StaleRefMessage + ": unknown ref " + target)
		}
		return locator.DragTo(t.page.Locator(`[data-excalibur-ref="` + t.snapshot + "-" + target + `"]`))
	default:
		return inputError("browser: unsupported action")
	}
}

type inputError string

func (err inputError) Error() string { return string(err) }

type policyError string

func (err policyError) Error() string { return string(err) }
func actionFailure(err error) capabilities.TaskResult {
	switch err.(type) {
	case inputError:
		return failed(err.Error(), capabilities.ErrInput)
	case policyError:
		return failed(err.Error(), capabilities.ErrPermanent)
	}
	return failed("browser: action failed or timed out", capabilities.ErrTransient)
}
func (e *Engine) act(ctx context.Context, t *tab, a arguments) (r capabilities.TaskResult) {
	kind, target := a.str("action"), ""
	var box *LiveRect
	started := false
	defer func() {
		if !started {
			e.activity(t, kind, "start", target, nil)
		}
		e.activity(t, kind, livePhase(r), target, box)
	}()
	locator, r, ok := e.ref(t, a)
	if !ok {
		return r
	}
	action := a.str("action")
	if action == "" {
		return failed("browser: action is required", capabilities.ErrInput)
	}
	watched := e.LiveWatched(t.session)
	var plan *liveTyping
	if action == "type" && (watched || e.hasLiveObserver()) {
		decision := typingPlan(a.str("value"), livePassword(locator))
		if decision.password {
			kind = "type_password"
		}
		// Withholding also applies unwatched, because a viewer can join later and
		// a page can keep showing the value. Only animation requires a viewer.
		decision.animate = decision.animate && watched
		plan = &decision
	}
	target = t.refs[a.str("ref")].Name
	if watched {
		box = liveBox(locator)
	}
	e.activity(t, kind, "start", target, box)
	started = true
	if watched && !liveDelay(ctx.Done(), 350*time.Millisecond) {
		return capabilities.TaskResult{State: capabilities.StateCancelled}
	}
	before := t.page.URL()
	err := e.actionWithTyping(ctx, t, locator, action, a.str("value"), a, plan)
	if err != nil {
		return actionFailure(err)
	}
	t.ready, t.snapshot = false, ""
	t.refs = nil
	e.detect(t)
	if submitsForm(action, a.str("value")) {
		e.settleLogin(ctx, t, before)
	}
	if t.blocked != "" {
		return e.blocked(t)
	}
	payload := map[string]any{"action": action, "ref": a.str("ref"), "navigated": before != t.page.URL(), "resnapshot": true}
	if strings.HasPrefix(a.str("value"), "secret://") {
		payload["value"] = "[secret]"
	}
	return complete(t.id, payload)
}
func (e *Engine) download(ctx context.Context, t *tab, a arguments) (r capabilities.TaskResult) {
	target := ""
	var box *LiveRect
	started := false
	defer func() {
		if !started {
			e.activity(t, "download", "start", target, nil)
		}
		e.activity(t, "download", livePhase(r), target, box)
	}()
	locator, r, ok := e.ref(t, a)
	if !ok {
		return r
	}
	watched := e.LiveWatched(t.session)
	target = t.refs[a.str("ref")].Name
	if watched {
		box = liveBox(locator)
	}
	e.activity(t, "download", "start", target, box)
	started = true
	if watched && !liveDelay(ctx.Done(), 350*time.Millisecond) {
		return capabilities.TaskResult{State: capabilities.StateCancelled}
	}
	download, err := t.page.ExpectDownload(func() error { return locator.Click() })
	if err != nil {
		return failed("browser: download failed or timed out", capabilities.ErrTransient)
	}
	name := filepath.Base(download.SuggestedFilename())
	path, err := download.Path()
	if err != nil {
		return failed("browser: download unavailable", capabilities.ErrTransient)
	}
	// Playwright's downloadsPath owns the generated path, never a model filename.
	data, err := os.ReadFile(path)
	if err != nil {
		return failed("browser: download unavailable", capabilities.ErrTransient)
	}
	defer download.Delete()
	ref, err := e.cfg.Blobs.Put(ctx, "download", data)
	if err != nil {
		return failed("browser: blob storage failed", capabilities.ErrTransient)
	}
	return complete(ref, map[string]any{"blob": ref, "name": e.clean(name), "size": len(data), "mime": http.DetectContentType(data)})
}
func (e *Engine) status() capabilities.TaskResult {
	sessions := []map[string]any{}
	for id, s := range e.sessions {
		tabs := []map[string]any{}
		for _, t := range s.tabs {
			tabs = append(tabs, map[string]any{"tab_id": t.id, "url": e.safeURL(t.url), "title": e.clean(t.title), "ready": t.ready, "blocked": t.blocked})
		}
		sessions = append(sessions, map[string]any{"session_id": id, "tabs": len(s.tabs), "tab_details": tabs, "suspended": e.suspended, "headed": s.headed})
	}
	rss, _ := processStats(e.cfg.ProfileDir)
	idle := e.cfg.Clock().Sub(e.lastUse)
	if idle < 0 {
		idle = 0
	}
	return complete("browser", map[string]any{"sessions": sessions, "ram_mb": float64(rss) / (1024 * 1024), "rss_bytes": rss, "max_sessions": 8, "idle_ms": idle.Milliseconds(), "suspended": e.suspended})
}
func (e *Engine) routine(ctx context.Context, a arguments, t *tab) capabilities.TaskResult {
	if e.routines == nil {
		var err error
		e.routines, err = loadRoutines(e.cfg.RoutinesDir)
		if err != nil {
			return failed("browser: routine storage unavailable", capabilities.ErrPermanent)
		}
	}
	action := a.action("list")
	name := a.str("routine")
	switch action {
	case "list":
		return complete("routines", map[string]any{"routines": routineNames(e.routines)})
	case "record":
		if name == "" {
			return failed("browser: routine name is required", capabilities.ErrInput)
		}
		var steps []Step
		if json.Unmarshal(a["steps"], &steps) != nil || len(steps) == 0 {
			return failed("browser: routine needs at least one step", capabilities.ErrInput)
		}
		for i := range steps {
			st := &steps[i]
			if st.Ref != "" {
				s := e.sessions[a.str("session_id")]
				if s == nil {
					return failed("browser: unknown session", capabilities.ErrInput)
				}
				id := a.str("tab_id")
				if id == "" {
					id = s.active
				}
				tab := s.tabs[id]
				if tab == nil {
					return failed("browser: unknown tab", capabilities.ErrInput)
				}
				target, ok := tab.refs[st.Ref]
				if !ok || tab.snapshot == "" {
					return failed(capabilities.StaleRefMessage+": unknown ref "+st.Ref, capabilities.ErrInput)
				}
				st.Target = target
				st.Ref = ""
			}
			if e.clean(st.Target.Name) != st.Target.Name || e.clean(st.Value) != st.Value || e.clean(name) != name {
				return failed("browser: routine contains protected content", capabilities.ErrInput)
			}
			if st.Target.Role == "" {
				return failed("browser: routine target needs role and name", capabilities.ErrInput)
			}
			if len(st.Assert) > 0 {
				assertion, err := parseAssertion(st.Assert)
				if err != nil {
					return failed(err.Error(), capabilities.ErrInput)
				}
				if e.clean(assertion.Value) != assertion.Value || (assertion.Target != nil && e.clean(assertion.Target.Name) != assertion.Target.Name) {
					return failed("browser: routine contains protected content", capabilities.ErrInput)
				}
			}
			switch st.Action {
			case "drag":
				if st.Destination == nil || st.Destination.Role == "" || e.clean(st.Destination.Name) != st.Destination.Name {
					return failed("browser: routine drag needs a destination", capabilities.ErrInput)
				}
			case "click", "type", "select", "hover", "scroll", "press", "upload":
			default:
				return failed("browser: unsupported routine action", capabilities.ErrInput)
			}
		}
		next := map[string][]Step{}
		for k, v := range e.routines {
			next[k] = v
		}
		next[name] = steps
		if err := saveRoutines(e.cfg.RoutinesDir, next); err != nil {
			return failed("browser: routine storage unavailable", capabilities.ErrTransient)
		}
		e.routines = next
		return complete(name, map[string]any{"routine": name, "steps": len(steps)})
	case "run":
		steps, ok := e.routines[name]
		if !ok {
			return failed(fmt.Sprintf("browser: unknown routine %q", name), capabilities.ErrInput)
		}
		for i, st := range steps {
			if ctx.Err() != nil {
				return capabilities.TaskResult{State: capabilities.StateCancelled}
			}
			locator := t.page.GetByRole(pw.AriaRole(st.Target.Role), pw.PageGetByRoleOptions{Name: st.Target.Name, Exact: pw.Bool(true)})
			var actionErr error
			if st.Action == "drag" && st.Destination != nil {
				actionErr = locator.DragTo(t.page.GetByRole(pw.AriaRole(st.Destination.Role), pw.PageGetByRoleOptions{Name: st.Destination.Name, Exact: pw.Bool(true)}))
			} else {
				actionErr = e.action(ctx, t, locator, st.Action, st.Value, nil)
			}
			if err := actionErr; err != nil {
				r := actionFailure(err)
				r.TaskID = fmt.Sprintf("%s-step-%d", name, i)
				r.ErrorText = fmt.Sprintf("routine %q step %d: %s", name, i, r.ErrorText)
				return r
			}
			t.ready, t.snapshot = false, ""
			t.refs = nil
			if len(st.Assert) > 0 {
				assertion, err := parseAssertion(st.Assert)
				if err != nil {
					return failed(err.Error(), capabilities.ErrInput)
				}
				passed := false
				if assertion.Type == "element_visible" {
					passed, _ = t.page.GetByRole(pw.AriaRole(assertion.Target.Role), pw.PageGetByRoleOptions{Name: assertion.Target.Name, Exact: pw.Bool(true)}).IsVisible()
				} else {
					text, _ := t.page.Locator("body").InnerText()
					passed = textAssertion(assertion, text, t.page.URL())
				}
				if !passed {
					r := failed(fmt.Sprintf("routine %q step %d assertion failed; control returns to the agent at this step", name, i), capabilities.ErrInput)
					r.TaskID = fmt.Sprintf("%s-step-%d", name, i)
					return r
				}
			}
			e.detect(t)
			if t.blocked != "" {
				return e.blocked(t)
			}
		}
		return complete(name, map[string]any{"routine": name, "steps_run": len(steps)})
	default:
		return failed(fmt.Sprintf("browser: unknown routine action %q", action), capabilities.ErrInput)
	}
}
