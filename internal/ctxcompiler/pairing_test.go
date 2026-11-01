package ctxcompiler_test

import (
	"context"
	"encoding/json"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/openai"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ctxcompiler "github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// balancedMessages builds n complete assistant-call/tool-result pairs.
func balancedMessages(n int) []providers.Message {
	var out []providers.Message
	for i := 0; i < n; i++ {
		id := "call_" + itoaPair(i)
		out = append(out, providers.Message{
			Role: providers.RoleAssistant, Content: "working",
			ToolCalls: []providers.ToolCall{{ID: id, Name: "exec", Arguments: []byte(`{"cmd":"x"}`)}},
		})
		out = append(out, providers.Message{
			Role: providers.RoleTool, Content: "ok",
			ToolResult: &providers.ToolResult{CallID: id, Name: "exec", Output: "ok"},
		})
	}
	return out
}

func itoaPair(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// Balanced histories validate clean.
func TestValidateMessagesBalanced(t *testing.T) {
	msgs := append([]providers.Message{
		{Role: providers.RoleSystem, Content: "sys"},
		{Role: providers.RoleUser, Content: "do it"},
	}, balancedMessages(3)...)
	if rep := ctxcompiler.ValidateMessages(msgs); !rep.Valid() {
		t.Fatalf("balanced request must validate: %+v", rep)
	}
}

// An orphan result (no call in the request) is the Responses 400:
// repair converts it to user text and the request validates.
func TestRepairMessagesOrphanResult(t *testing.T) {
	msgs := balancedMessages(1)
	msgs = append(msgs, providers.Message{
		Role: providers.RoleTool, Content: "late output",
		ToolResult: &providers.ToolResult{CallID: "call_orphan", Name: "exec", Output: "late output"},
	})
	rep := ctxcompiler.ValidateMessages(msgs)
	if rep.Valid() || len(rep.OrphanResults) != 1 || rep.OrphanResults[0] != "call_orphan" {
		t.Fatalf("must flag the orphan result: %+v", rep)
	}
	fixed, rep := ctxcompiler.RepairMessages(msgs)
	// The repair report still names the orphan; the fixed list must
	// validate on its own.
	if rep.Repaired != 1 {
		t.Fatalf("must repair exactly one orphan, got %+v", rep)
	}
	if got := ctxcompiler.ValidateMessages(fixed); !got.Valid() {
		t.Fatalf("repaired request must validate: %+v", got)
	}
	if len(fixed) != len(msgs) {
		t.Fatalf("repair preserves order/length: %d -> %d", len(msgs), len(fixed))
	}
	last := fixed[len(fixed)-1]
	if last.Role != providers.RoleUser || last.ToolResult != nil {
		t.Fatalf("orphan result must become plain user text: %+v", last)
	}
	if !strings.Contains(last.Content, "call_orphan") || !strings.Contains(last.Content, "late output") {
		t.Fatalf("summary must carry the identity and output: %q", last.Content)
	}
}

// An orphan call (no result in the request) is the Chat Completions
// mirror: repair strips the call and appends a text summary while
// keeping the complete pairs of the same message.
func TestRepairMessagesOrphanCall(t *testing.T) {
	msgs := []providers.Message{
		{Role: providers.RoleAssistant, Content: "batch", ToolCalls: []providers.ToolCall{
			{ID: "call_keep", Name: "exec", Arguments: []byte(`{"cmd":"a"}`)},
			{ID: "call_drop", Name: "exec", Arguments: []byte(`{"cmd":"b"}`)},
		}},
		{Role: providers.RoleTool, Content: "a-ok",
			ToolResult: &providers.ToolResult{CallID: "call_keep", Name: "exec", Output: "a-ok"}},
	}
	rep := ctxcompiler.ValidateMessages(msgs)
	if rep.Valid() || len(rep.OrphanCalls) != 1 || rep.OrphanCalls[0] != "call_drop" {
		t.Fatalf("must flag the orphan call: %+v", rep)
	}
	fixed, rep := ctxcompiler.RepairMessages(msgs)
	if rep.Repaired != 1 {
		t.Fatalf("must repair exactly one orphan, got %+v", rep)
	}
	if got := ctxcompiler.ValidateMessages(fixed); !got.Valid() {
		t.Fatalf("repaired request must validate: %+v", got)
	}
	if len(fixed[0].ToolCalls) != 1 || fixed[0].ToolCalls[0].ID != "call_keep" {
		t.Fatalf("complete pair must survive: %+v", fixed[0].ToolCalls)
	}
	if !strings.Contains(fixed[0].Content, "call_drop") {
		t.Fatalf("assistant content must carry the dropped call summary: %q", fixed[0].Content)
	}
}

// Activity-level repair preserves Seq order and converts both
// directions to text.
func TestRepairActivityOrphans(t *testing.T) {
	events := []ctxcompiler.ActivityEvent{
		{Seq: 1, Role: providers.RoleAssistant, Content: "go",
			ToolCalls: []providers.ToolCall{{ID: "c1", Name: "exec", Arguments: []byte(`{}`)}}},
		{Seq: 2, Role: providers.RoleTool, Content: "r1",
			ToolResult: &providers.ToolResult{CallID: "c1", Name: "exec", Output: "r1"}},
		{Seq: 3, Role: providers.RoleTool, Content: "orphan-out",
			ToolResult: &providers.ToolResult{CallID: "cX", Name: "exec", Output: "orphan-out"}},
		{Seq: 4, Role: providers.RoleAssistant, Content: "lost",
			ToolCalls: []providers.ToolCall{{ID: "cY", Name: "exec", Arguments: []byte(`{}`)}}},
	}
	rep := ctxcompiler.ValidateActivity(events)
	if rep.Valid() || len(rep.OrphanResults) != 1 || len(rep.OrphanCalls) != 1 {
		t.Fatalf("must flag both orphans: %+v", rep)
	}
	fixed, rep := ctxcompiler.RepairActivity(events)
	if rep.Repaired != 2 {
		t.Fatalf("must repair both orphans, got %+v", rep)
	}
	if got := ctxcompiler.ValidateActivity(fixed); !got.Valid() {
		t.Fatalf("repaired activity must validate: %+v", got)
	}
	for i, e := range fixed {
		if e.Seq != int64(i+1) {
			t.Fatalf("repair must preserve order/seq: %+v", fixed)
		}
	}
	if fixed[2].Role != providers.RoleUser || fixed[2].ToolResult != nil {
		t.Fatalf("orphan result must become user text: %+v", fixed[2])
	}
	if len(fixed[3].ToolCalls) != 0 || !strings.Contains(fixed[3].Content, "cY") {
		t.Fatalf("orphan call must be stripped to text: %+v", fixed[3])
	}
}

// The 400 matcher recognizes both wire directions and nothing else.
func TestIsOrphanPairError(t *testing.T) {
	responses := "providers: openai/muse-spark-1.3 input: http 400: No function call found for function call output with call_id 'call_abc123'"
	chat := "providers: openai/gpt-6-luna input: http 400: An assistant message with 'tool_calls' must be followed by tool messages responding to each 'tool_call_id'."
	for _, err := range []error{errString(responses), errString(chat)} {
		if !ctxcompiler.IsOrphanPairError(err) {
			t.Fatalf("must match orphan rejection: %v", err)
		}
	}
	for _, err := range []error{
		nil,
		errString("providers: openai/gpt-6-luna transient: http 500: server error"),
		errString("providers: openai/gpt-6-luna rate_limited: http 429: slow down"),
		errString("message 3 tool result misses call id"),
		errString("context limit exceeded"),
		errString("http 500: No function call found"),
		errString("http 400: invalid function_call_output schema; call_id must be a string"),
	} {
		if ctxcompiler.IsOrphanPairError(err) {
			t.Fatalf("must not match non-pairing error: %v", err)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// All three switches default ON and honor the off values.
func TestPairSwitches(t *testing.T) {
	if !ctxcompiler.PairAtomicEnabled(nil) || !ctxcompiler.PairCheckEnabled(nil) || !ctxcompiler.PairRetryEnabled(nil) {
		t.Fatal("pairing switches must default ON")
	}
	t.Setenv("EXCALIBUR_CONTEXT_PAIR_ATOMIC", "0")
	t.Setenv("EXCALIBUR_CONTEXT_PAIR_CHECK", "off")
	t.Setenv("EXCALIBUR_CONTEXT_PAIR_RETRY", "false")
	if ctxcompiler.PairAtomicEnabled(nil) || ctxcompiler.PairCheckEnabled(nil) || ctxcompiler.PairRetryEnabled(nil) {
		t.Fatal("pairing switches must honor off values")
	}
}

// A budget cut inside a multi-call batch keeps the whole batch: one
// assistant event with two calls plus both results.
func TestSelectTailMultiCallBatchAtomic(t *testing.T) {
	counter := fixedCounter{per: 4}
	head := ctxcompiler.ActivityEvent{Seq: 8, Role: providers.RoleUser, Content: "start"}
	call := ctxcompiler.ActivityEvent{
		Seq: 9, Role: providers.RoleAssistant, Content: "batch\n",
		ToolCalls: []providers.ToolCall{
			{ID: "m1", Name: "exec", Arguments: []byte(`{}`)},
			{ID: "m2", Name: "exec", Arguments: []byte(`{}`)},
		},
	}
	res1 := ctxcompiler.ActivityEvent{
		Seq: 10, Role: providers.RoleTool, Content: strings.Repeat("r", 3900),
		ToolResult: &providers.ToolResult{CallID: "m1", Name: "exec", Output: strings.Repeat("r", 3900)},
	}
	res2 := ctxcompiler.ActivityEvent{
		Seq: 11, Role: providers.RoleTool, Content: strings.Repeat("s", 3900),
		ToolResult: &providers.ToolResult{CallID: "m2", Name: "exec", Output: strings.Repeat("s", 3900)},
	}
	events := []ctxcompiler.ActivityEvent{head, call, res1, res2}
	// Budget fits only the newest result: the atomic tail must pull
	// the whole batch (call + both results) along.
	tail := ctxcompiler.SelectTail(events, 1000, counter)
	if rep := ctxcompiler.ValidateActivity(tail); !rep.Valid() {
		t.Fatalf("atomic tail must keep pairs complete: %+v", rep)
	}
	if len(tail) != 3 {
		t.Fatalf("whole batch must ride along, kept %d events", len(tail))
	}
	// With the switch off the legacy single-pair behavior applies:
	// the cut orphans the newest result (documented ablation gap).
	t.Setenv("EXCALIBUR_CONTEXT_PAIR_ATOMIC", "0")
	legacy := ctxcompiler.SelectTail(events, 1000, counter)
	if rep := ctxcompiler.ValidateActivity(legacy); rep.Valid() {
		t.Fatalf("legacy tail is expected to split the batch (ablation): %+v", legacy)
	}
}

// Long synthetic run crossing masking and compaction thresholds, with
// multi-call batches mixed in: masking keeps pairs, the compacted
// tail keeps pairs, and the projected request validates for both
// wire shapes.
func TestLongSyntheticRunPairingAcrossMaskAndCompact(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	cfg.ModelLimits = map[string]ctxcompiler.ModelLimits{
		"test-pair": {ContextWindow: 12000, MaxInput: 9000, MaxOutput: 1000},
	}
	cfg.CompactionReserveTokens = 500
	cfg.MaskTriggerTokens = 2000
	cfg.MaskTargetTokens = 1000
	cfg.LiteralObservations = 2
	cfg.PostCompactionLiteralTokens = 1500
	counter := ctxcompiler.NewEstimator(4)
	ctrl := ctxcompiler.NewController(cfg, counter, "test-pair")

	var activity []ctxcompiler.ActivityEvent
	seq := int64(1)
	appendPair := func(tool string, body string) {
		id := "lp_" + itoaPair(int(seq))
		activity = append(activity, ctxcompiler.ActivityEvent{
			Seq: seq, Role: providers.RoleAssistant, Content: "step",
			ToolCalls: []providers.ToolCall{{ID: id, Name: tool, Arguments: []byte(`{"cmd":"x"}`)}},
		})
		seq++
		activity = append(activity, ctxcompiler.ActivityEvent{
			Seq: seq, Role: providers.RoleTool, Content: body,
			ToolResult: &providers.ToolResult{CallID: id, Name: tool, Output: body},
		})
		seq++
	}
	for i := 0; i < 100; i++ {
		appendPair("exec", strings.Repeat("x", 400))
		if i%10 == 9 {
			// Multi-call batch: one assistant event, two results.
			id1, id2 := "batch1_"+itoaPair(i), "batch2_"+itoaPair(i)
			activity = append(activity, ctxcompiler.ActivityEvent{
				Seq: seq, Role: providers.RoleAssistant, Content: "batch",
				ToolCalls: []providers.ToolCall{
					{ID: id1, Name: "exec", Arguments: []byte(`{}`)},
					{ID: id2, Name: "exec", Arguments: []byte(`{}`)},
				},
			})
			seq++
			for _, id := range []string{id1, id2} {
				activity = append(activity, ctxcompiler.ActivityEvent{
					Seq: seq, Role: providers.RoleTool, Content: strings.Repeat("y", 400),
					ToolResult: &providers.ToolResult{CallID: id, Name: "exec", Output: strings.Repeat("y", 400)},
				})
				seq++
			}
		}
	}
	if rep := ctxcompiler.ValidateActivity(activity); !rep.Valid() {
		t.Fatalf("synthetic history must start paired: %+v", rep)
	}
	// Masking replaces outputs in place: pairs stay complete.
	maskOut := ctrl.ApplyMask(activity, nil, false)
	if !maskOut.Applied {
		t.Fatal("masking must fire on the synthetic history")
	}
	if rep := ctxcompiler.ValidateActivity(activity); !rep.Valid() {
		t.Fatalf("masking must keep pairs complete: %+v", rep)
	}
	// Compaction must fire past the trigger and keep a paired tail.
	snap := snapWithActivity(ctxcompiler.Snapshot{
		Role: "child", HarnessVersion: "test",
		Task: ctxcompiler.TaskView{OriginalRequest: "synthetic pairing run"},
	}, activity)
	compiled, err := ctxcompiler.Compile(cfg, snap, counter)
	if err != nil {
		t.Fatal(err)
	}
	needTotal := compiled.TotalTokens
	if !ctrl.NeedsCompaction(needTotal + 6000) {
		t.Fatalf("synthetic total %d must trip the trigger", needTotal)
	}
	state := ctxcompiler.StateFromTask(snap.Task)
	outcome, kept, err := ctrl.ApplyCompact(context.Background(), state, activity, needTotal+6000)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Applied {
		t.Fatal("compaction must fire past the trigger")
	}
	if rep := ctxcompiler.ValidateActivity(kept); !rep.Valid() {
		t.Fatalf("compacted tail must keep pairs complete: %+v", rep)
	}
	// The projected request must validate for both wire shapes:
	// Responses (function_call per function_call_output) and Chat
	// Completions (tool message per tool_call).
	finalSnap := snapWithActivity(snap, kept)
	finalSnap.CompactionBlock = outcome.Block
	final, err := ctxcompiler.Compile(cfg, finalSnap, counter)
	if err != nil {
		t.Fatal(err)
	}
	req := final.ToChatRequest("test-pair", nil, 0, "", "")
	final.AppendActivityMessages(&req, finalSnap.Activity)
	if rep := ctxcompiler.ValidateMessages(req.Messages); !rep.Valid() {
		t.Fatalf("projected request must validate: %+v", rep)
	}
	assertResponsesPairs(t, req.Messages)
	assertChatCompletionsPairs(t, req.Messages)
	assertPairingOnWires(t, req)
}

// assertResponsesPairs mirrors the Responses projection rule: every
// function_call_output item needs its function_call in the same input.
func assertResponsesPairs(t *testing.T, msgs []providers.Message) {
	t.Helper()
	calls := map[string]bool{}
	var outputs []string
	for _, m := range msgs {
		if m.Role == providers.RoleAssistant {
			for _, tc := range m.ToolCalls {
				calls[tc.ID] = true // function_call item
			}
		}
		if m.Role == providers.RoleTool && m.ToolResult != nil {
			outputs = append(outputs, m.ToolResult.CallID) // function_call_output item
		}
	}
	for _, id := range outputs {
		if !calls[id] {
			t.Fatalf("Responses input misses function_call for output %q", id)
		}
	}
}

// assertChatCompletionsPairs mirrors the Chat Completions rule: every
// assistant tool_call needs its tool message in the same request.
func assertChatCompletionsPairs(t *testing.T, msgs []providers.Message) {
	t.Helper()
	results := map[string]bool{}
	var calls []string
	for _, m := range msgs {
		if m.Role == providers.RoleAssistant {
			for _, tc := range m.ToolCalls {
				calls = append(calls, tc.ID)
			}
		}
		if m.Role == providers.RoleTool && m.ToolResult != nil {
			results[m.ToolResult.CallID] = true
		}
	}
	for _, id := range calls {
		if !results[id] {
			t.Fatalf("Chat Completions input misses tool message for call %q", id)
		}
	}
}

// Exercise the actual adapters against a strict simulated server. This checks
// serialization as well as the compiler's shared message representation.
func assertPairingOnWires(t *testing.T, req providers.ChatRequest) {
	t.Helper()
	for _, responses := range []bool{false, true} {
		name := "chat"
		if responses {
			name = "responses"
		}
		t.Run(name, func(t *testing.T) {
			seen := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen = true
				var body struct {
					Input []struct {
						Type   string
						CallID string `json:"call_id"`
					} `json:"input"`
					Messages []struct {
						Role       string
						ToolCallID string                `json:"tool_call_id"`
						ToolCalls  []struct{ ID string } `json:"tool_calls"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				calls, results := map[string]int{}, map[string]int{}
				if responses {
					for _, item := range body.Input {
						if item.Type == "function_call" {
							calls[item.CallID]++
						}
						if item.Type == "function_call_output" {
							results[item.CallID]++
						}
					}
				} else {
					for _, m := range body.Messages {
						for _, c := range m.ToolCalls {
							calls[c.ID]++
						}
						if m.Role == "tool" {
							results[m.ToolCallID]++
						}
					}
				}
				if len(calls) == 0 {
					t.Error("wire regression must retain literal tool pairs")
				}
				for id, n := range calls {
					if n != 1 || results[id] != 1 {
						t.Errorf("call %s: calls=%d results=%d", id, n, results[id])
					}
				}
				for id, n := range results {
					if n != 1 || calls[id] != 1 {
						t.Errorf("result %s: calls=%d results=%d", id, calls[id], n)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				if responses {
					_, _ = w.Write([]byte(`{"id":"resp-test","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`))
				} else {
					_, _ = w.Write([]byte(`{"id":"chat-test","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
				}
			}))
			defer srv.Close()
			adapter, err := openai.New(openai.Options{APIKey: "test", BaseURL: srv.URL, UseResponses: responses})
			if err != nil {
				t.Fatal(err)
			}
			defer adapter.Close()
			if _, err := adapter.Chat(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			if !seen {
				t.Fatal("adapter did not send request")
			}
		})
	}
}

func TestAtomicTailWithInterveningPinnedNote(t *testing.T) {
	events := []ctxcompiler.ActivityEvent{
		{Seq: 1, Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "pinned-call", Name: "exec", Arguments: []byte("{}")}}},
		{Seq: 2, Role: providers.RoleUser, Content: strings.Repeat("note ", 100)},
		{Seq: 3, Role: providers.RoleTool, Content: "ok", ToolResult: &providers.ToolResult{CallID: "pinned-call", Output: "ok"}},
	}
	// The budget includes the note and result, placing the edge on plain
	// text rather than a result. The call still has to ride along.
	counter := ctxcompiler.NewEstimator(0)
	kept := ctxcompiler.SelectTail(events, counter.Count(events[1].Content)+counter.Count("ok")+20, counter)
	if len(kept) != 3 {
		t.Fatalf("split through note: kept %d events", len(kept))
	}
	if rep := ctxcompiler.ValidateActivity(kept); !rep.Valid() {
		t.Fatalf("split pair: %+v", rep)
	}
}
