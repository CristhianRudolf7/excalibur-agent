package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

func TestActionableRefusalSequences(t *testing.T) {
	// Replay the refusal streak lengths observed in v9, using invented artifacts.
	// No task instructions, checks, or solutions are fixture data.
	for _, n := range []int{20, 9, 9} {
		t.Run(fmt.Sprintf("streak-%d", n), func(t *testing.T) {
			model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp(`{"false_requirement":false,"reason":"Explicit deliverable remains required"}`)}}
			e, _ := testEngine(model, scriptedRunner(), passingGate())
			r := explicitRun()
			r.Request = "Create /tmp/report."
			last := ""
			for i := 0; i < n; i++ {
				note := e.refuseFinish(context.Background(), r, []string{"required artifact missing: /tmp/report"})
				if note == last {
					t.Fatal("consecutive duplicate refusal")
				}
				last = note
				var p struct {
					Attempt   int           `json:"attempt"`
					Checklist []RefusalItem `json:"checklist"`
					Next      string        `json:"next_action"`
				}
				if err := json.Unmarshal([]byte(strings.TrimPrefix(note, "finish refused: ")), &p); err != nil {
					t.Fatal(err)
				}
				if i == 0 && (len(p.Checklist) != 1 || p.Checklist[0].ID != "A1" || p.Checklist[0].Check != "test -s '/tmp/report'") {
					t.Fatalf("checklist: %+v", p)
				}
				if i > 0 && (len(p.Checklist) != 0 || !strings.Contains(p.Next, "test -s")) {
					t.Fatalf("repeat must contain delta and action: %+v", p)
				}
				r.recordFinishRefusal(note)
			}
			if model.calls() != 1 || r.requirementDropped("A1") {
				t.Fatalf("reviews=%d dropped=%v", model.calls(), r.requirementDropped("A1"))
			}
			if len(r.RefusalRecords()) != n+1 {
				t.Fatalf("records=%d", len(r.RefusalRecords()))
			}
		})
	}
}

func TestFalseRequirementReview(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		fail, drop  bool
	}{
		{"false", `{"false_requirement":true,"reason":"Example file is optional","instruction_quote":"Optionally create /tmp/example."}`, false, true},
		{"real", `{"false_requirement":false,"reason":"Keep explicit requirement"}`, false, false},
		{"bad quote", `{"false_requirement":true,"reason":"Not required","instruction_quote":"invented"}`, false, false},
		{"malformed", `not json`, false, false},
		{"error", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp(tc.reply)}}
			if tc.fail {
				model.err = errors.New("unavailable")
			}
			e, _ := testEngine(model, scriptedRunner(), passingGate())
			r := explicitRun()
			r.Request = "Optionally create /tmp/example."
			for i := 0; i < 3; i++ {
				note := e.refuseFinish(context.Background(), r, []string{"required artifact missing: /tmp/example"})
				r.recordFinishRefusal(note)
			}
			if r.requirementDropped("A1") != tc.drop {
				t.Fatalf("drop=%v", r.requirementDropped("A1"))
			}
			rec := r.RefusalRecords()
			if len(rec) != 4 || rec[3].Review == "" || rec[3].Dropped != tc.drop {
				t.Fatalf("audit: %+v", rec)
			}
			gate := e.applyArtifacts(r, tasks.GateReport{OK: true}, nil, r.Request, "", t.TempDir())
			if gate.OK != tc.drop {
				t.Fatalf("artifact gate %+v", gate)
			}
			r.Request = "Create /tmp/example."
			if r.requirementDropped("A1") {
				t.Fatal("instruction change must invalidate withdrawal")
			}
		})
	}
}

func TestActionableAllItemsAndDeltas(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := explicitRun()
	reasons := []string{"criterion C1 lacks evidence", "criterion C2 failed", "criterion C3 stale", "child pending", "project tests not run", "instruction stale"}
	note := e.refuseFinish(context.Background(), r, reasons)
	r.recordFinishRefusal(note)
	if strings.Count(note, `"missing"`) != len(reasons) {
		t.Fatalf("lost items: %s", note)
	}
	note = e.refuseFinish(context.Background(), r, reasons[1:])
	if !strings.Contains(note, `"resolved"`) || strings.Contains(note, `"missing"`) {
		t.Fatalf("bad delta: %s", note)
	}
}

func TestActionableSwitchOff(t *testing.T) {
	t.Setenv("EXCALIBUR_ACTIONABLE_REFUSALS", "off")
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := explicitRun()
	note := e.refuseFinish(context.Background(), r, []string{"missing evidence"})
	if note != "finish refused: missing evidence" || len(r.RefusalRecords()) != 0 {
		t.Fatalf("off: %s", note)
	}
}

func TestActionableFinishReturnsChecklistInHistory(t *testing.T) {
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("f1", "finish", finishArgs("done", "checked"))),
		toolResp(toolCall("f2", "finish", finishArgs("done", "checked again"))),
	}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	cfg := tasks.DefaultSelfVerifyConfig()
	cfg.EnableRefine = false
	cfg.EnableStrong = false
	e.deps.SelfVerify = &cfg
	r := explicitRun()
	r.Request = "Create /tmp/uncreated"
	r.Workdir = t.TempDir()
	startRun(t, e, r, js)
	for i := 0; i < 2; i++ {
		if _, err := e.step(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	if r.FinishRefusals != 2 || len(r.RefusalRecords()) < 2 {
		t.Fatalf("refusals=%d", r.FinishRefusals)
	}
	found := 0
	for _, ev := range r.History {
		if ev.ToolResult != nil && ev.ToolResult.Name == "finish" {
			found++
			if !ev.ToolResult.IsError || !strings.Contains(ev.ToolResult.Output, `"attempt"`) {
				t.Fatalf("tool result: %+v", ev.ToolResult)
			}
		}
	}
	if found != 2 {
		t.Fatalf("finish results=%d", found)
	}
	entries, err := js.List(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	recorded := 0
	for _, entry := range entries {
		if entry.Type == "finish_refusal" {
			recorded++
		}
	}
	if recorded != 2 {
		t.Fatalf("durable refusal records=%d", recorded)
	}
}

func TestExerciseRefusalUsesExecutableCheck(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := explicitRun()
	r.Request = "Create /tmp/report.py."
	r.drafts = []tasks.DraftCriterion{{ID: "D1", Key: "/tmp/report.py", Check: "python3 /tmp/report.py"}}
	items := e.refusalItems(r, []string{`deliverable not exercised: no passing run of "/tmp/report.py"`})
	if len(items) != 1 || items[0].ID != "D1" || items[0].Check != "python3 /tmp/report.py" {
		t.Fatalf("exercise must not suggest existence probe: %+v", items)
	}
	items = e.refusalItems(r, []string{"required artifact missing: /tmp/report.py"})
	for _, item := range items {
		if item.ID == "A2" {
			t.Fatalf("basename matched inside absolute path: %+v", items)
		}
	}
}

func TestRefusalReplayPreservesThresholdAndWithdrawal(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp(`{"false_requirement":true,"reason":"Example is optional","instruction_quote":"Optionally create /tmp/example."}`)}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	r := explicitRun()
	r.Request = "Optionally create /tmp/example."
	startRun(t, e, r, js)
	for i := 0; i < 2; i++ {
		note := e.refuseFinish(ctx, r, []string{"required artifact missing: /tmp/example"})
		r.recordFinishRefusal(note)
	}
	resumed := explicitRun()
	resumed.ID = r.ID
	resumed.Request = r.Request
	e.restoreRefusals(ctx, resumed)
	if resumed.FinishRefusals != 2 || len(resumed.RefusalRecords()) != 2 {
		t.Fatalf("replay count: %d %+v", resumed.FinishRefusals, resumed.RefusalRecords())
	}
	note := e.refuseFinish(ctx, resumed, []string{"required artifact missing: /tmp/example"})
	resumed.recordFinishRefusal(note)
	if !resumed.requirementDropped("A1") || model.calls() != 1 {
		t.Fatal("third refusal after restart must review extraction")
	}
	again := explicitRun()
	again.ID = r.ID
	again.Request = r.Request
	e.restoreRefusals(ctx, again)
	if !again.requirementDropped("A1") || again.FinishRefusals != 3 {
		t.Fatal("withdrawal must survive restart")
	}
	again.Request = "Create /tmp/example."
	e.restoreRefusals(ctx, again)
	if again.requirementDropped("A1") {
		t.Fatal("old instruction withdrawal replayed")
	}
}
