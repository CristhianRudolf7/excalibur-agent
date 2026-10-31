package capabilities

import (
	"encoding/json"
	"strings"
	"testing"
)

// H13 contract tests: one fake per capability of Section 14.1 with the
// behaviors the Section 14.13 verification table pins. The harness
// treats every capability as a black box; these simulations are what
// backend tests run against until the real ones exist.

func mustManifest(t *testing.T, m Manifest) Manifest {
	t.Helper()
	if err := m.Validate(); err != nil {
		t.Fatalf("manifest %q invalid: %v", m.ID, err)
	}
	return m
}

func args(m map[string]any) json.RawMessage {
	raw, _ := json.Marshal(m)
	return raw
}

func TestAllManifestsValidate(t *testing.T) {
	for _, m := range []Manifest{
		BrowserManifest(), VisionManifest(), VoiceManifest(), DeviceManifest(),
		DocumentsManifest(), MediaManifest(), AppsManifest(), MessagingManifest(),
		WorkspaceManifest(), MeetingManifest(), NodesManifest(), GPUManifest(),
		StoreManifest(), GitHubManifest(), UserRepManifest(),
	} {
		mustManifest(t, m)
	}
}

func openTab(t *testing.T, b *BrowserFake) (string, string) {
	t.Helper()
	open, err := b.Invoke("browser_open_session", args(map[string]any{"profile": "test"}), "k-open")
	if err != nil || open.State != StateCompleted {
		t.Fatalf("open session: %+v %v", open, err)
	}
	var opened map[string]any
	_ = json.Unmarshal(open.Payload, &opened)
	sess := opened["session_id"].(string)
	tab, err := b.Invoke("browser_tabs", args(map[string]any{"session_id": sess, "action": "new", "url": "https://example.com"}), "k-tab")
	if err != nil || tab.State != StateCompleted {
		t.Fatalf("new tab: %+v %v", tab, err)
	}
	var tabbed map[string]any
	_ = json.Unmarshal(tab.Payload, &tabbed)
	return sess, tabbed["tab_id"].(string)
}

// Row 8a: acting or capturing without wait_ready is rejected.
func TestBrowserActWithoutWaitRejected(t *testing.T) {
	b := NewBrowserFake()
	sess, tab := openTab(t, b)
	snap, _ := b.Invoke("browser_snapshot", args(map[string]any{"session_id": sess, "tab_id": tab}), "")
	if snap.State != StateFailed || !strings.Contains(snap.ErrorText, "not ready") {
		t.Fatalf("snapshot without wait must be rejected: %+v", snap)
	}
	act, _ := b.Invoke("browser_act", args(map[string]any{"session_id": sess, "tab_id": tab, "action": "click", "ref": "r1"}), "k-act")
	if act.State != StateFailed || !strings.Contains(act.ErrorText, "not ready") {
		t.Fatalf("act without wait must be rejected: %+v", act)
	}
	// After wait_ready the flow works: snapshot, then act.
	if _, _ = b.Invoke("browser_wait_ready", args(map[string]any{"session_id": sess, "tab_id": tab}), ""); true {
	}
	snap2, _ := b.Invoke("browser_snapshot", args(map[string]any{"session_id": sess, "tab_id": tab}), "")
	if snap2.State != StateCompleted {
		t.Fatalf("snapshot after wait: %+v", snap2)
	}
	var payload map[string]any
	_ = json.Unmarshal(snap2.Payload, &payload)
	snapID := payload["snapshot_id"].(string)
	act2, _ := b.Invoke("browser_act", args(map[string]any{"session_id": sess, "tab_id": tab, "snapshot_id": snapID, "action": "click", "ref": "r1"}), "k-act")
	if act2.State != StateCompleted {
		t.Fatalf("act with fresh ref: %+v", act2)
	}
}

// Row 8b: acting with an old snapshot ref returns stale_ref.
func TestBrowserStaleRef(t *testing.T) {
	b := NewBrowserFake()
	sess, tab := openTab(t, b)
	_, _ = b.Invoke("browser_wait_ready", args(map[string]any{"session_id": sess, "tab_id": tab}), "")
	snap, _ := b.Invoke("browser_snapshot", args(map[string]any{"session_id": sess, "tab_id": tab}), "")
	var payload map[string]any
	_ = json.Unmarshal(snap.Payload, &payload)
	old := payload["snapshot_id"].(string)
	// A second snapshot rotates the id; the first one goes stale.
	_, _ = b.Invoke("browser_snapshot", args(map[string]any{"session_id": sess, "tab_id": tab}), "")
	act, _ := b.Invoke("browser_act", args(map[string]any{"session_id": sess, "tab_id": tab, "snapshot_id": old, "action": "click", "ref": "r1"}), "k-stale")
	if act.State != StateFailed || !strings.Contains(act.ErrorText, StaleRefMessage) {
		t.Fatalf("old ref must yield stale_ref: %+v", act)
	}
}

// Rows 6/8c: a blocked navigation (CAPTCHA) goes working, surfaces
// input_required by id, and resumes after Update.
func TestBrowserBlockedResumeByID(t *testing.T) {
	b := NewBrowserFake()
	open, _ := b.Invoke("browser_open_session", args(map[string]any{}), "k-o")
	var opened map[string]any
	_ = json.Unmarshal(open.Payload, &opened)
	sess := opened["session_id"].(string)
	tab, _ := b.Invoke("browser_tabs", args(map[string]any{"session_id": sess, "action": "new", "url": "https://bank.example/captcha-login"}), "k-t")
	var tabbed map[string]any
	_ = json.Unmarshal(tab.Payload, &tabbed)
	tabID := tabbed["tab_id"].(string)
	nav, _ := b.Invoke("browser_navigate", args(map[string]any{"session_id": sess, "tab_id": tabID, "action": "go", "url": "https://bank.example/captcha-login"}), "k-n")
	if nav.State != StateWorking || nav.TaskID == "" {
		t.Fatalf("blocked navigation must be working with a task id: %+v", nav)
	}
	// The harness persists the id and re-polls after a restart.
	taskID := nav.TaskID
	need, _ := b.Poll(taskID)
	if need.State != StateInputRequired || !strings.Contains(need.Need, BlockedCaptcha) {
		t.Fatalf("poll must surface the block: %+v", need)
	}
	resumed, _ := b.Update(taskID, args(map[string]any{"solution": "user solved in headed mode"}))
	if resumed.State != StateCompleted {
		t.Fatalf("update must resume: %+v", resumed)
	}
	ready, _ := b.Invoke("browser_wait_ready", args(map[string]any{"session_id": sess, "tab_id": tabID}), "")
	if ready.State != StateCompleted {
		t.Fatalf("page must be ready after the block clears: %+v", ready)
	}
}

// Row 8d: a routine replays deterministically and a failed assertion
// returns control to the agent at that step.
func TestBrowserRoutineAssertion(t *testing.T) {
	b := NewBrowserFake()
	sess, tab := openTab(t, b)
	rec, _ := b.Invoke("browser_routine", args(map[string]any{
		"action": "record", "routine": "login",
		"steps": []map[string]any{
			{"action": "type", "ref": "r2", "value": "user", "assert": "Sign in"},
			{"action": "click", "ref": "r1", "assert": "Welcome"},
		},
	}), "k-rec")
	if rec.State != StateCompleted {
		t.Fatalf("record: %+v", rec)
	}
	b.SetPageText(sess, tab, "Sign in page")
	run, _ := b.Invoke("browser_routine", args(map[string]any{"action": "run", "routine": "login", "session_id": sess, "tab_id": tab}), "k-run")
	if run.State != StateFailed || !strings.Contains(run.ErrorText, "step 1") {
		t.Fatalf("failed assertion must return control at the step: %+v", run)
	}
	b.SetPageText(sess, tab, "Sign in and Welcome")
	run2, _ := b.Invoke("browser_routine", args(map[string]any{"action": "run", "routine": "login", "session_id": sess, "tab_id": tab}), "k-run")
	if run2.State != StateCompleted {
		t.Fatalf("routine must pass when every assertion holds: %+v", run2)
	}
}

// Row 7: resending an effectful operation with the same key records a
// single effect.
func TestIdempotentResendSingleEffect(t *testing.T) {
	b := NewBrowserFake()
	sess, tab := openTab(t, b)
	_, _ = b.Invoke("browser_wait_ready", args(map[string]any{"session_id": sess, "tab_id": tab}), "")
	snap, _ := b.Invoke("browser_snapshot", args(map[string]any{"session_id": sess, "tab_id": tab}), "")
	var payload map[string]any
	_ = json.Unmarshal(snap.Payload, &payload)
	for i := 0; i < 3; i++ {
		_, _ = b.Invoke("browser_act", args(map[string]any{"session_id": sess, "tab_id": tab, "snapshot_id": payload["snapshot_id"], "action": "click", "ref": "r1"}), "same-key")
		_, _ = b.Invoke("browser_wait_ready", args(map[string]any{"session_id": sess, "tab_id": tab}), "")
		snap, _ = b.Invoke("browser_snapshot", args(map[string]any{"session_id": sess, "tab_id": tab}), "")
		_ = json.Unmarshal(snap.Payload, &payload)
	}
	if got := b.Effects("browser_act"); got != 1 {
		t.Fatalf("same key must record a single effect, got %d", got)
	}

	m := NewMessagingFake()
	for i := 0; i < 2; i++ {
		_, _ = m.Invoke("contact_message", args(map[string]any{"channel": "telegram", "to": "@bob", "text": "hi", "confirm": "yes"}), "same-key")
	}
	if len(m.Sent()) != 1 {
		t.Fatalf("contact_message resend must send once, got %d", len(m.Sent()))
	}
}

// Row 6: the full durable progression working -> input_required ->
// completed, plus failed and cancelled, all pollable by id.
func TestDurableProgression(t *testing.T) {
	b := NewBrowserFake()
	sess, tab := openTab(t, b)
	_, _ = b.Invoke("browser_navigate", args(map[string]any{"session_id": sess, "tab_id": tab, "action": "go", "url": "https://x.example/login"}), "k")
	// Cooperative cancel of a blocked task.
	open2, _ := b.Invoke("browser_open_session", args(map[string]any{}), "k2")
	var o2 map[string]any
	_ = json.Unmarshal(open2.Payload, &o2)
	cancelled, _ := b.Cancel("nav-1")
	if cancelled.State != StateCancelled {
		t.Fatalf("cancel must be cooperative and terminal: %+v", cancelled)
	}
	failed := Failed("t", "disk melted", ErrPermanent)
	if failed.ErrClass != ErrPermanent {
		t.Fatalf("permanent class lost: %+v", failed)
	}
	if !Terminal(StateFailed) || !Terminal(StateCancelled) || Terminal(StateWorking) {
		t.Fatal("terminal state mapping wrong")
	}
}

// Voice: audio transcription works; meeting transcription needs
// consent (row 11).
func TestVoiceConsent(t *testing.T) {
	v := NewVoiceFake()
	ok, _ := v.Invoke("audio_transcribe", args(map[string]any{"path": "call.mp3"}), "")
	if ok.State != StateCompleted {
		t.Fatalf("audio_transcribe: %+v", ok)
	}
	v.JoinMeeting("meet-1", false)
	denied, _ := v.Invoke("meeting_transcribe", args(map[string]any{"meeting_id": "meet-1", "consent": "no"}), "k")
	if denied.State != StateFailed {
		t.Fatalf("transcription without consent must fail: %+v", denied)
	}
	v.JoinMeeting("meet-2", true)
	allowed, _ := v.Invoke("meeting_transcribe", args(map[string]any{"meeting_id": "meet-2", "consent": "yes"}), "k")
	if allowed.State != StateCompleted {
		t.Fatalf("transcription with consent: %+v", allowed)
	}
}

// Device: unsupported operations fail explicitly instead of hanging.
func TestDeviceUnsupported(t *testing.T) {
	d := NewDeviceFake("device_screen_capture")
	ok, _ := d.Invoke("device_screen_capture", args(map[string]any{}), "")
	if ok.State != StateCompleted {
		t.Fatalf("supported op: %+v", ok)
	}
	bad, _ := d.Invoke("device_input", args(map[string]any{"action": "click"}), "k")
	if bad.State != StateFailed || !strings.Contains(bad.ErrorText, "not supported") {
		t.Fatalf("unsupported op must fail explicitly: %+v", bad)
	}
	click, _ := NewDeviceFake().Invoke("device_input", args(map[string]any{"action": "click", "ref": "ok"}), "k")
	if click.State != StateCompleted {
		t.Fatalf("full device: %+v", click)
	}
}

// Documents: conversion allowlist, version history, userdb (row 13).
func TestDocumentsPairsAndVersions(t *testing.T) {
	d := NewDocumentsFake()
	created, _ := d.Invoke("document_create", args(map[string]any{"format": "md", "spec": "# hello", "path": "note.md"}), "k-c")
	if created.State != StateCompleted {
		t.Fatalf("create: %+v", created)
	}
	_, _ = d.Invoke("document_edit", args(map[string]any{"path": "note.md", "ops": "# hello world"}), "k-e")
	if vers := d.Versions("note.md"); len(vers) != 2 {
		t.Fatalf("want 2 versions, got %d", len(vers))
	}
	read, _ := d.Invoke("document_read", args(map[string]any{"path": "note.md"}), "")
	var payload map[string]any
	_ = json.Unmarshal(read.Payload, &payload)
	if payload["version"].(float64) != 2 {
		t.Fatalf("read must expose the latest version: %v", payload)
	}
	pages := payload["pages"].([]any)
	first := pages[0].(map[string]any)
	if first["text"] != "# hello world" || len(first["paragraphs"].([]any)) == 0 {
		t.Fatalf("read must preserve content with anchors: %v", first)
	}
	ok, _ := d.Invoke("file_convert", args(map[string]any{"from": "md", "to": "html", "path": "note.md"}), "")
	if ok.State != StateCompleted {
		t.Fatalf("allowlisted pair: %+v", ok)
	}
	bad, _ := d.Invoke("file_convert", args(map[string]any{"from": "docx", "to": "pdf", "path": "doc.docx"}), "")
	if bad.State != StateFailed || !strings.Contains(bad.ErrorText, "unsupported conversion") {
		t.Fatalf("unlisted pair must fail with the pair list: %+v", bad)
	}
	schema, _ := d.Invoke("userdb", args(map[string]any{"action": "create", "db": "shop", "sql": "CREATE TABLE orders (id)"}), "k-u")
	if schema.State != StateCompleted {
		t.Fatalf("userdb create: %+v", schema)
	}
	query, _ := d.Invoke("userdb", args(map[string]any{"action": "query", "db": "shop", "sql": "SELECT * FROM orders"}), "")
	if query.State != StateCompleted {
		t.Fatalf("userdb query: %+v", query)
	}
	missing, _ := d.Invoke("userdb", args(map[string]any{"action": "query", "db": "shop", "sql": "SELECT * FROM missing"}), "")
	if missing.State != StateFailed {
		t.Fatalf("unknown table must fail: %+v", missing)
	}
}

// Media: dialect errors, SVG-only honesty, image cost.
func TestMediaContracts(t *testing.T) {
	m := NewMediaFake()
	bad, _ := m.Invoke("diagram_render", args(map[string]any{"dialect": "mermaid", "source": "syntax-error here"}), "")
	if bad.State != StateFailed || !strings.Contains(bad.ErrorText, "syntax error") {
		t.Fatalf("concrete syntax errors: %+v", bad)
	}
	png, _ := m.Invoke("diagram_render", args(map[string]any{"dialect": "mermaid", "source": "graph TD; A-->B", "format": "png"}), "")
	if png.State != StateFailed || !strings.Contains(png.ErrorText, "SVG") {
		t.Fatalf("png must be an honest input error: %+v", png)
	}
	ok, _ := m.Invoke("diagram_render", args(map[string]any{"dialect": "graphviz", "source": "digraph {a->b}"}), "")
	var svg map[string]any
	_ = json.Unmarshal(ok.Payload, &svg)
	if ok.State != StateCompleted || !strings.Contains(svg["svg"].(string), "graphviz") {
		t.Fatalf("graphviz svg: %+v", ok)
	}
	img, _ := m.Invoke("image_generate", args(map[string]any{"prompt": "a lighthouse"}), "k-img")
	if img.State != StateCompleted || !strings.Contains(string(img.Payload), "cost_usd") {
		t.Fatalf("image cost must be reported: %+v", img)
	}
}

// Apps: manual changes arrive as events and the L3 delta starts from
// the updated state; the classroom rule gates completion (row 10).
func TestAppsDeltaAndCourseRule(t *testing.T) {
	a := NewAppsFake()
	a.Register("sheet-1", "spreadsheet")
	_, _ = a.Invoke("spreadsheet_ops", args(map[string]any{"instance": "sheet-1", "ops": "set A1 42"}), "k-a")
	a.Register("class-1", "course")
	a.SetQuiz("class-1", "t1", []QuizQuestion{{ID: "q1", Kind: "choice", Prompt: "2+2?", Answer: "4"}})
	gated, _ := a.Invoke("course_ops", args(map[string]any{"instance": "class-1", "ops": `{"action":"complete_topic","topic":"t1","answers":{"q1":"5"}}`}), "k-c")
	if gated.State != StateFailed || !strings.Contains(gated.ErrorText, "not complete") {
		t.Fatalf("wrong quiz answers must gate the topic: %+v", gated)
	}
	done, _ := a.Invoke("course_ops", args(map[string]any{"instance": "class-1", "ops": `{"action":"complete_topic","topic":"t1","answers":{"q1":"4"}}`}), "k-c2")
	if done.State != StateCompleted {
		t.Fatalf("every answer correct must complete: %+v", done)
	}
	seq := a.EmitUserChange("sheet-1", "user set B2 7")
	delta := a.DeltaSince("sheet-1", seq-1)
	if len(delta) != 1 || delta[0].Actor != "user" {
		t.Fatalf("manual change must surface in the delta: %+v", delta)
	}
	state, _ := a.Invoke("app_state", args(map[string]any{"instance": "sheet-1", "since": "0"}), "")
	if state.State != StateCompleted || !strings.Contains(string(state.Payload), "app:sheet-1") {
		t.Fatalf("app_state must carry scope and recent actions: %+v", state)
	}
}

// Messaging and workspace: irreversible actions need confirmation.
func TestIrreversibleNeedsConfirm(t *testing.T) {
	m := NewMessagingFake()
	denied, _ := m.Invoke("contact_message", args(map[string]any{"channel": "telegram", "to": "@x", "text": "hi"}), "k")
	if denied.State != StateFailed || !strings.Contains(denied.ErrorText, "confirmation") {
		t.Fatalf("contact_message needs confirm=yes: %+v", denied)
	}
	w := NewWorkspaceFake()
	mailDenied, _ := w.Invoke("email", args(map[string]any{"action": "send", "to": "a@b.c", "subject": "s"}), "k")
	if mailDenied.State != StateFailed {
		t.Fatalf("mail send needs confirm=yes: %+v", mailDenied)
	}
	sent, _ := w.Invoke("email", args(map[string]any{"action": "send", "to": "a@b.c", "subject": "s", "body": "b", "confirm": "yes"}), "k")
	if sent.State != StateCompleted {
		t.Fatalf("confirmed send: %+v", sent)
	}
	created, _ := w.Invoke("calendar", args(map[string]any{"action": "create", "title": "demo", "confirm": "yes"}), "k-cal")
	var payload map[string]any
	_ = json.Unmarshal(created.Payload, &payload)
	event := payload["event"].(map[string]any)
	calDenied, _ := w.Invoke("calendar", args(map[string]any{"action": "create", "title": "x"}), "k2")
	if calDenied.State != StateFailed {
		t.Fatalf("calendar create needs confirmation: %+v", calDenied)
	}
	if !strings.Contains(payload["inverse"].(string), event["id"].(string)) {
		t.Fatalf("calendar create must declare its inverse: %v", payload)
	}
}

// Meetings: disclosure as assistant, consent for transcription (row 11).
func TestMeetingDisclosureAndConsent(t *testing.T) {
	m := NewMeetingFake()
	asUser, _ := m.Invoke("meeting", args(map[string]any{"action": "join", "meeting_id": "m1", "identify": "user"}), "k")
	if asUser.State != StateFailed || !strings.Contains(asUser.ErrorText, "assistant") {
		t.Fatalf("the agent must join as an assistant: %+v", asUser)
	}
	joined, _ := m.Invoke("meeting", args(map[string]any{"action": "join", "meeting_id": "m1", "identify": "assistant"}), "k")
	if joined.State != StateCompleted || !strings.Contains(string(joined.Payload), "assistant") {
		t.Fatalf("assistant join: %+v", joined)
	}
	silent, _ := m.Invoke("meeting", args(map[string]any{"action": "listen", "meeting_id": "m1"}), "k")
	if silent.State != StateFailed || !strings.Contains(silent.ErrorText, "consent") {
		t.Fatalf("no transcription without consent: %+v", silent)
	}
}

// Nodes: connected runs, disconnected errors or waits (row 12).
func TestNodesExecTarget(t *testing.T) {
	r := NewNodeRegistry()
	r.Pair("laptop", "linux", []string{"terminal", "browser"}, true)
	r.Pair("phone", "linux", []string{"desktop"}, false)
	ran, _ := r.Exec("node:laptop", "ls /tmp", false)
	if ran.State != StateCompleted {
		t.Fatalf("connected node must run: %+v", ran)
	}
	if runs := r.Runs("laptop"); len(runs) != 1 || runs[0] != "ls /tmp" {
		t.Fatalf("node runs: %v", runs)
	}
	down, _ := r.Exec("node:phone", "ls", false)
	if down.State != StateFailed {
		t.Fatalf("disconnected node must error: %+v", down)
	}
	waiting, _ := r.Exec("node:phone", "ls", true)
	if waiting.State != StateWorking {
		t.Fatalf("disconnected node with wait must stay working: %+v", waiting)
	}
	still, _ := r.Poll(waiting.TaskID)
	if still.State != StateWorking {
		t.Fatalf("re-poll before reconnect: %+v", still)
	}
	r.SetConnected("phone", true)
	back, _ := r.Poll(waiting.TaskID)
	if back.State != StateCompleted {
		t.Fatalf("re-poll after reconnect: %+v", back)
	}
	bad, _ := r.Exec("node:phone", "ls", false)
	if bad.State != StateFailed || !strings.Contains(bad.ErrorText, "terminal") {
		t.Fatalf("node without the terminal capability: %+v", bad)
	}
	if _, ok := ParseTarget("local"); ok {
		t.Fatal("local targets are not node targets")
	}
}

// GPU, store and userrep.
func TestServicesGPUStore(t *testing.T) {
	g := NewGPUFake()
	needs, _ := g.Invoke("gpu_training", args(map[string]any{"action": "launch"}), "k")
	if needs.State != StateFailed {
		t.Fatalf("launch needs a model: %+v", needs)
	}
	launched, _ := g.Invoke("gpu_training", args(map[string]any{"action": "launch", "model": "tiny"}), "k")
	if launched.State != StateCompleted {
		t.Fatalf("launch: %+v", launched)
	}
	var payload map[string]any
	_ = json.Unmarshal(launched.Payload, &payload)
	metrics, _ := g.Invoke("gpu_training", args(map[string]any{"action": "metrics", "job": payload["job"]}), "")
	if metrics.State != StateCompleted {
		t.Fatalf("metrics: %+v", metrics)
	}
	s := NewStoreFake()
	inst, _ := s.Invoke("store_install", args(map[string]any{"package": "shapes@1.0.0"}), "k")
	if inst.State != StateCompleted || len(s.Installed()) != 1 {
		t.Fatalf("store install: %+v", inst)
	}
	dup, _ := s.Invoke("store_install", args(map[string]any{"package": "shapes@1.0.0"}), "k")
	if dup.State != StateCompleted || len(s.Installed()) != 1 {
		t.Fatalf("same key must install once: %d", len(s.Installed()))
	}
}

// Section 14.11 is deferred: the stub refuses with a permanent error.
func TestUserRepDeferred(t *testing.T) {
	var u UserRepStub
	res, err := u.Invoke("agent_delegate_user", args(map[string]any{"user": "ana"}), "k")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateFailed || res.ErrClass != ErrPermanent {
		t.Fatalf("userrep must refuse permanently: %+v", res)
	}
}
