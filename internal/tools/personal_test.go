package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
)

// Personal-assistant deferred tools (Section 13.3): every tool runs
// against its Section 14.1 capability contract with a contract fake,
// through the standard 13.4 pipeline.

func personalSetup() (*MapRegistry, *DeferredCatalog, *Executor, *PersonalDeps) {
	reg := NewMapRegistry()
	_ = RegisterBuiltins(reg, nil)
	cat := NewDeferredCatalog(reg)
	exec := NewExecutor(reg)
	WireSearchCall(reg, cat, exec)
	deps := PersonalFakes()
	if _, err := RegisterPersonalDeferred(reg, cat, deps); err != nil {
		panic(err)
	}
	WirePersonalCore(reg, NewSettingsStore(), NewRemindersStore())
	return reg, cat, exec, deps
}

func call(t *testing.T, exec *Executor, tool string, args map[string]any, key string) Result {
	t.Helper()
	raw, _ := json.Marshal(args)
	res, err := exec.Execute(context.Background(), tool, raw, ExecuteOptions{IdempotencyKey: key})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return res
}

func TestPersonalToolsRegisteredDeferred(t *testing.T) {
	reg, cat, exec, _ := personalSetup()
	_ = exec
	for _, name := range []string{"email", "calendar", "contact_message", "meeting", "github", "userdb", "gpu_training", "nodes"} {
		if _, err := reg.Get(name); err != nil {
			t.Fatalf("personal tool %q not registered: %v", name, err)
		}
		if !cat.IsDeferred(name) {
			t.Fatalf("personal tool %q must be deferred", name)
		}
	}
	// Core settings and reminders stay wired through their providers.
	for _, name := range []string{"settings", "reminders"} {
		tool, err := reg.Get(name)
		if err != nil {
			t.Fatalf("core tool %q: %v", name, err)
		}
		if stub, ok := tool.(*StubTool); !ok || stub.Provider == nil {
			t.Fatalf("core tool %q must have a provider", name)
		}
	}
}

func TestPersonalPrefixStableAndSearchable(t *testing.T) {
	_, cat, exec, _ := personalSetup()
	before := cat.PrefixHash()
	_ = before
	hits := cat.Search("send email message", 5)
	found := false
	for _, h := range hits {
		if h.Definition.Name == "email" || h.Definition.Name == "contact_message" {
			found = true
		}
	}
	if !found {
		t.Fatalf("email family must be discoverable: %+v", hits)
	}
	hits = cat.Search("calendar events availability", 5)
	found = false
	for _, h := range hits {
		if h.Definition.Name == "calendar" {
			found = true
		}
	}
	if !found {
		t.Fatal("calendar must be discoverable")
	}
	_ = exec
}

func TestEmailConfirmGate(t *testing.T) {
	_, _, exec, _ := personalSetup()
	denied := call(t, exec, "email", map[string]any{"action": "send", "to": "a@b.c", "subject": "s"}, "k1")
	if denied.Status != StatusInputError {
		t.Fatalf("unconfirmed send must be rejected: %+v", denied)
	}
	sent := call(t, exec, "email", map[string]any{"action": "send", "to": "a@b.c", "subject": "s", "body": "b", "confirm": "yes"}, "k1")
	if sent.Status != StatusOK {
		t.Fatalf("confirmed send: %+v", sent)
	}
	listed := call(t, exec, "email", map[string]any{"action": "list"}, "")
	if listed.Status != StatusOK || !strings.Contains(listed.Output, "Q3 plan") {
		t.Fatalf("list must show the seed mail: %+v", listed)
	}
}

func TestCalendarInverse(t *testing.T) {
	_, _, exec, _ := personalSetup()
	created := call(t, exec, "calendar", map[string]any{"action": "create", "title": "demo", "confirm": "yes"}, "k-cal")
	if created.Status != StatusOK || !strings.Contains(created.Output, "inverse") {
		t.Fatalf("create must declare its inverse: %+v", created)
	}
	cancelled := call(t, exec, "calendar", map[string]any{"action": "cancel", "id": "e1"}, "k-cancel")
	if cancelled.Status != StatusOK {
		t.Fatalf("cancel: %+v", cancelled)
	}
}

func TestContactMessageConfirm(t *testing.T) {
	_, _, exec, deps := personalSetup()
	denied := call(t, exec, "contact_message", map[string]any{"channel": "telegram", "to": "@x", "text": "hi"}, "k")
	if denied.Status != StatusInputError {
		t.Fatalf("unconfirmed third-party message: %+v", denied)
	}
	ok := call(t, exec, "contact_message", map[string]any{"channel": "telegram", "to": "@x", "text": "hi", "confirm": "yes"}, "k")
	if ok.Status != StatusOK {
		t.Fatalf("confirmed: %+v", ok)
	}
	again := call(t, exec, "contact_message", map[string]any{"channel": "telegram", "to": "@x", "text": "hi", "confirm": "yes"}, "k")
	if again.Status != StatusOK {
		t.Fatalf("resend: %+v", again)
	}
	mf, isFake := deps.Messaging.(*capabilities.MessagingFake)
	if !isFake {
		t.Fatal("messaging backend must be the contract fake")
	}
	if got := len(mf.Sent()); got != 1 {
		t.Fatalf("same key must send once, got %d", got)
	}
}

func TestMeetingConsentDisclosure(t *testing.T) {
	_, _, exec, _ := personalSetup()
	asUser := call(t, exec, "meeting", map[string]any{"action": "join", "meeting_id": "m1", "identify": "user"}, "k")
	if asUser.Status != StatusInputError {
		t.Fatalf("must join as assistant: %+v", asUser)
	}
	join := call(t, exec, "meeting", map[string]any{"action": "join", "meeting_id": "m1", "identify": "assistant"}, "k")
	if join.Status != StatusOK {
		t.Fatalf("join: %+v", join)
	}
	silent := call(t, exec, "meeting", map[string]any{"action": "listen", "meeting_id": "m1"}, "k2")
	if silent.Status != StatusInputError {
		t.Fatalf("listen without consent: %+v", silent)
	}
}

func TestGitHubTool(t *testing.T) {
	_, _, exec, _ := personalSetup()
	created := call(t, exec, "github", map[string]any{"action": "issue_create", "repo": "org/repo", "title": "bug"}, "k-gh")
	if created.Status != StatusOK || !strings.Contains(created.Output, "org/repo#") {
		t.Fatalf("issue create: %+v", created)
	}
	runs := call(t, exec, "github", map[string]any{"action": "ci_runs", "repo": "org/repo"}, "")
	if runs.Status != StatusOK {
		t.Fatalf("ci runs: %+v", runs)
	}
}

func TestUserDBTool(t *testing.T) {
	_, _, exec, _ := personalSetup()
	schema := call(t, exec, "userdb", map[string]any{"action": "create", "db": "shop", "sql": "CREATE TABLE orders (id)"}, "k-u")
	if schema.Status != StatusOK {
		t.Fatalf("create: %+v", schema)
	}
	query := call(t, exec, "userdb", map[string]any{"action": "query", "db": "shop", "sql": "SELECT * FROM orders"}, "")
	if query.Status != StatusOK {
		t.Fatalf("query: %+v", query)
	}
}

func TestGPUTrainingTool(t *testing.T) {
	_, _, exec, _ := personalSetup()
	launch := call(t, exec, "gpu_training", map[string]any{"action": "launch", "model": "tiny"}, "k-gpu")
	if launch.Status != StatusOK {
		t.Fatalf("launch: %+v", launch)
	}
	metrics := call(t, exec, "gpu_training", map[string]any{"action": "metrics"}, "")
	if metrics.Status != StatusOK {
		t.Fatalf("metrics: %+v", metrics)
	}
}

func TestNodesTool(t *testing.T) {
	reg, _, exec, deps := personalSetup()
	_ = reg
	deps.Nodes.Pair("laptop", "linux", []string{"terminal"}, true)
	listed := call(t, exec, "nodes", map[string]any{}, "")
	if listed.Status != StatusOK || !strings.Contains(listed.Output, "laptop") {
		t.Fatalf("nodes list: %+v", listed)
	}
}

func TestSettingsCore(t *testing.T) {
	_, _, exec, _ := personalSetup()
	got := call(t, exec, "settings", map[string]any{"action": "get", "key": "timezone"}, "")
	if got.Status != StatusOK || got.Output != "UTC" {
		t.Fatalf("get timezone: %+v", got)
	}
	set := call(t, exec, "settings", map[string]any{"action": "set", "key": "timezone", "value": "Europe/Madrid"}, "")
	if set.Status != StatusOK {
		t.Fatalf("set: %+v", set)
	}
	again := call(t, exec, "settings", map[string]any{"action": "get", "key": "timezone"}, "")
	if again.Output != "Europe/Madrid" {
		t.Fatalf("timezone not changed: %+v", again)
	}
}

func TestRemindersCore(t *testing.T) {
	_, _, exec, _ := personalSetup()
	created := call(t, exec, "reminders", map[string]any{"action": "create", "text": "water plants", "when": "tomorrow 08:00"}, "")
	if created.Status != StatusOK {
		t.Fatalf("create: %+v", created)
	}
	var rec map[string]any
	_ = json.Unmarshal([]byte(created.Output), &rec)
	id := rec["id"].(string)
	snoozed := call(t, exec, "reminders", map[string]any{"action": "snooze", "id": id, "when": "tomorrow 09:00"}, "")
	if snoozed.Status != StatusOK {
		t.Fatalf("snooze: %+v", snoozed)
	}
	done := call(t, exec, "reminders", map[string]any{"action": "complete", "id": id}, "")
	if done.Status != StatusOK {
		t.Fatalf("complete: %+v", done)
	}
	empty := call(t, exec, "reminders", map[string]any{"action": "list"}, "")
	if !strings.Contains(empty.Output, `"reminders":[]`) {
		t.Fatalf("completed reminders hide by default: %+v", empty)
	}
}
