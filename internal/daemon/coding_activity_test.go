package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

type codingFixtureTool struct{ invoke func() (tools.Result, error) }

func (t codingFixtureTool) Definition() tools.Definition {
	return tools.Definition{Name: "fixture", Description: "local fixture", Effect: types.EffectReadOnly, Idempotency: types.IdemPure, Cancel: types.CancelImmediate, InputSchema: json.RawMessage(`{"type":"object"}`), OutputBudget: 1 << 20}
}
func (t codingFixtureTool) ResourceKeys(json.RawMessage) ([]string, error) { return nil, nil }
func (t codingFixtureTool) Invoke(context.Context, tools.Invocation) (tools.Result, error) {
	return t.invoke()
}

func TestCodingActivityBoundsRedactionOrdering(t *testing.T) {
	for _, mode := range []string{"ok", "failure", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			bus := telemetry.NewBus(nil)
			reg := tools.NewMapRegistry()
			secret := "fixture-credential-value"
			redactor := vault.BuildRedactor([][]byte{[]byte(secret)}, []string{"fixture"})
			c := &codingActivity{bus: bus, redact: redactor.Redact, conversation: "conversation", run: "child", parent: "parent", turn: "turn"}
			code := 1
			if err := reg.Register(codingFixtureTool{invoke: func() (tools.Result, error) {
				events := bus.Replay(types.Cursor{})
				if len(events) != 1 || events[0].Type != types.EventToolCalled {
					t.Fatal("start must be visible before invocation")
				}
				result := tools.Result{Status: tools.StatusOK, Output: strings.Repeat("界", 4000) + secret + "\nsk-" + strings.Repeat("a", 24) + "\nlast line", ExitCode: &code}
				if mode == "failure" {
					return result, errors.New(secret)
				}
				if mode == "cancel" {
					return result, context.Canceled
				}
				return result, nil
			}}); err != nil {
				t.Fatal(err)
			}
			runner := &daemonToolRunner{reg: reg, ex: tools.NewExecutor(reg), coding: c}
			_, _ = runner.Execute(context.Background(), "fixture", json.RawMessage(`{}`), tools.ExecuteOptions{CallID: "call"})
			events := bus.Replay(types.Cursor{})
			if len(events) != 2 || events[1].Type != types.EventToolCompleted || events[1].Seq <= events[0].Seq {
				t.Fatalf("order: %+v", events)
			}
			for _, event := range events {
				if event.RunID == nil || *event.RunID != "child" || strings.Contains(string(event.Payload), secret) || strings.Contains(string(event.Payload), "sk-"+strings.Repeat("a", 24)) {
					t.Fatal("association or redaction")
				}
			}
			var end api.ToolCallEvent
			_ = json.Unmarshal(events[1].Payload, &end)
			if len(end.Output) > codingTextLimit || !utf8.ValidString(end.Output) || end.CallID != "call" || end.ParentRunID != "parent" || end.TurnID != "turn" || end.ConversationID != "conversation" {
				t.Fatalf("bounded outcome: %+v", end)
			}
			if mode == "ok" && (!end.Truncated || end.ExitCode == nil || *end.ExitCode != 1 || !strings.Contains(end.Output, "last line")) {
				t.Fatal("missing bounded command tail")
			}
			want := "completed"
			if mode != "ok" {
				want = "failed"
			}
			if end.Status != want {
				t.Fatal(end.Status)
			}
		})
	}
}

func TestCodingActivityRecordedPerEditDiffs(t *testing.T) {
	root := t.TempDir()
	_, dt, err := buildEngine(engineWiring{Workdir: root})
	if err != nil {
		t.Fatal(err)
	}
	bus := telemetry.NewBus(nil)
	c := &codingActivity{bus: bus, history: dt.History, redact: vault.NewRedactor().Redact, conversation: "conversation", run: "child", parent: "parent", turn: "turn", root: root}
	runner := &daemonToolRunner{reg: dt.Registry, ex: dt.Exec, coding: c}
	ctx := tools.WithSourceRun(context.Background(), "child")
	inputs := []struct{ name, args string }{
		{"write_file", `{"path":"file.txt","content":"old\n"}`},
		{"read_file", `{"path":"file.txt"}`},
		{"edit_file", `{"path":"file.txt","old_text":"old","new_text":"new"}`},
		{"edit_file", `{"path":"file.txt","old_text":"new","new_text":"old"}`},
	}
	for _, in := range inputs {
		res, err := runner.Execute(ctx, in.name, json.RawMessage(in.args), tools.ExecuteOptions{})
		if err != nil || res.Status != tools.StatusOK {
			t.Fatalf("%s: %+v %v", in.name, res, err)
		}
	}
	var edits []api.FileEditEvent
	for _, event := range bus.Replay(types.Cursor{}) {
		if event.Type == types.EventFileEdited {
			var edit api.FileEditEvent
			_ = json.Unmarshal(event.Payload, &edit)
			edits = append(edits, edit)
		}
	}
	if len(edits) != 3 {
		t.Fatalf("edits: %+v", edits)
	}
	if edits[1].Path != "file.txt" || edits[1].Added != 1 || edits[1].Removed != 1 || !strings.Contains(edits[1].Diff, "-old\n+new") || !strings.Contains(edits[2].Diff, "-new\n+old") {
		t.Fatalf("per-edit diffs: %+v", edits)
	}
	if edits[0].EditID == edits[1].EditID || edits[0].CallID == edits[1].CallID {
		t.Fatal("identities must differ")
	}
	// A concurrent run's version is excluded even when it uses the same path.
	before := c.versions(dt.Registry, "write_file", json.RawMessage(`{"path":"file.txt"}`))
	dt.History.Record(filepath.Join(root, "file.txt"), []byte("old"), []byte("other"), "unrelated", "")
	length := bus.Len()
	c.edits(c.call("write_file", nil, tools.ExecuteOptions{}), before)
	if bus.Len() != length {
		t.Fatal("unrelated edit leaked")
	}
}

func TestCodingActivityDiffBoundsAndProgress(t *testing.T) {
	root := t.TempDir()
	_, dt, err := buildEngine(engineWiring{Workdir: root})
	if err != nil {
		t.Fatal(err)
	}
	bus := telemetry.NewBus(nil)
	redactor := vault.BuildRedactor([][]byte{[]byte("fixture-password")}, []string{"test"})
	c := &codingActivity{bus: bus, history: dt.History, redact: redactor.Redact, conversation: "conversation", run: "child", parent: "parent", turn: "turn", root: root, steps: []string{"Read", "Edit", "Test"}, started: time.Now()}
	c.progress(types.RunRunning)
	path := filepath.Join(root, "large.txt")
	before := map[string]map[string]bool{path: {}}
	dt.History.Record(path, nil, []byte("fixture-password\n"+strings.Repeat("界\n", 10000)), "child", "")
	c.edits(c.call("write_file", nil, tools.ExecuteOptions{}), before)
	c.progress(types.RunCompleted)
	events := bus.Replay(types.Cursor{})
	var edit api.FileEditEvent
	_ = json.Unmarshal(events[1].Payload, &edit)
	if !edit.Truncated || len(edit.Diff) > codingTextLimit || !utf8.ValidString(edit.Diff) || strings.Contains(edit.Diff, "fixture-password") {
		t.Fatal("unsafe diff")
	}
	var progress api.RunProgressEvent
	_ = json.Unmarshal(events[2].Payload, &progress)
	if progress.PlanDone != 3 || progress.PlanTotal != 3 || progress.ParentRunID != "parent" || progress.ConversationID != "conversation" {
		t.Fatalf("progress: %+v", progress)
	}
}

func TestCodingActivityInputRedactionAndBounds(t *testing.T) {
	secret := "fixture-credential-value"
	c := &codingActivity{redact: vault.BuildRedactor([][]byte{[]byte(secret)}, []string{"fixture"}).Redact}
	args, _ := json.Marshal(map[string]string{"command": "echo " + secret + strings.Repeat("a", 3000), "path": secret + strings.Repeat("界", 500), "session": secret})
	call := c.call("exec", args, tools.ExecuteOptions{CallID: secret})
	raw, _ := json.Marshal(call)
	if strings.Contains(string(raw), secret) || len(call.Command) > 2048 || len(call.Path) > codingPathLimit || !utf8.ValidString(call.Path) || !call.Truncated {
		t.Fatal("unsafe tool start")
	}
}

func TestCodingActivityMultilineSecretDiff(t *testing.T) {
	_, dt, err := buildEngine(engineWiring{})
	if err != nil {
		t.Fatal(err)
	}
	secret := "fixture-secret-first\nfixture-secret-second"
	redactor := vault.BuildRedactor([][]byte{[]byte(secret)}, []string{"multiline"})
	bus := telemetry.NewBus(nil)
	c := &codingActivity{bus: bus, history: dt.History, redact: redactor.Redact, run: "child"}
	dt.History.Record("file", nil, []byte(secret+"\n"), "child", "")
	c.edits(c.call("write_file", nil, tools.ExecuteOptions{}), map[string]map[string]bool{"file": {}})
	events := bus.Replay(types.Cursor{})
	if len(events) != 1 || strings.Contains(string(events[0].Payload), "fixture-secret-first") || strings.Contains(string(events[0].Payload), "fixture-secret-second") {
		t.Fatal("multiline secret leaked in diff")
	}
}

func TestCodingActivityMultiFileOrderAndLimit(t *testing.T) {
	_, dt, err := buildEngine(engineWiring{})
	if err != nil {
		t.Fatal(err)
	}
	bus := telemetry.NewBus(nil)
	c := &codingActivity{bus: bus, history: dt.History, redact: vault.NewRedactor().Redact, run: "child"}
	var ops []map[string]string
	for i := 0; i < 60; i++ {
		ops = append(ops, map[string]string{"path": fmt.Sprintf("file-%02d", i)})
	}
	args, _ := json.Marshal(map[string]any{"ops": ops})
	before := c.versions(dt.Registry, "apply_patch", args)
	if len(before) != codingFileLimit {
		t.Fatalf("observed %d paths", len(before))
	}
	for path := range before {
		dt.History.Record(path, nil, []byte("new\n"), "child", "")
	}
	c.edits(c.call("apply_patch", nil, tools.ExecuteOptions{}), before)
	events := bus.Replay(types.Cursor{})
	if len(events) != codingFileLimit {
		t.Fatalf("events %d", len(events))
	}
	last := ""
	for _, event := range events {
		var edit api.FileEditEvent
		_ = json.Unmarshal(event.Payload, &edit)
		if edit.Path < last {
			t.Fatal("unstable multi-file order")
		}
		last = edit.Path
	}
}
