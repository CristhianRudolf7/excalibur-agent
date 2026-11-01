package ctxcompiler_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	ctxcompiler "github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// 11.4 trigger (owner decision 0051): maxInput - (reserve + safety),
// optionally capped. The old fixed 500 000 ceiling survives only as
// the optional cap (0 = model maximum).
func TestCompactionThreshold(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	// gpt-6-luna max input 922 000, reserve 8192, safety 10%:
	// 922000 - 8192 - 92200 = 821608.
	if got := ctxcompiler.CompactionThreshold(922000, 8192, cfg); got != 821608 {
		t.Fatalf("922K max input threshold = %d, want 821608", got)
	}
	// Conservative 128K max input, reserve 4000:
	// 128000 - 4000 - 12800 = 111200.
	if got := ctxcompiler.CompactionThreshold(128000, 4000, cfg); got != 111200 {
		t.Fatalf("128K max input threshold = %d, want 111200", got)
	}
	if got := ctxcompiler.CompactionThresholdForModel(922000, 8192, cfg, 200000); got != 200000 {
		t.Fatalf("per-model ceiling must lower the trigger, got %d", got)
	}
	if got := ctxcompiler.CompactionThreshold(100, 100000, cfg); got != 1024 {
		t.Fatalf("degenerate max input must clamp to 1024, got %d", got)
	}
	// Margin: an estimate just under the trigger still trips it.
	// Threshold 111200 for 128K/4000; 101092*1.1 = 111201 >= trigger.
	if !ctxcompiler.ShouldCompact(101092, 128000, 4000, cfg) {
		t.Fatal("margined estimate must trip the trigger")
	}
	if ctxcompiler.ShouldCompact(1000, 128000, 4000, cfg) {
		t.Fatal("tiny estimate must not compact")
	}
}

func testState() ctxcompiler.StructuredState {
	return ctxcompiler.StructuredState{
		Objective:         "Fix the crash on empty input.",
		OriginalRequest:   "Fix the crash on empty input.",
		FixedInstructions: []string{"Do not touch unrelated files."},
		Requirements:      []ctxcompiler.RequirementView{{Code: "R9", Text: "No crash on empty input", State: "accepted"}},
		Criteria:          []ctxcompiler.CriterionView{{Code: "C9.1", Statement: "repro test passes", State: "unverified"}},
		Plan: []ctxcompiler.PlanItemView{
			{Code: "P1", Title: "Reproduce", State: "done"},
			{Code: "P2", Title: "Fix parser", State: "in_progress"},
		},
		NextSteps: []string{"Run the repro test."},
		Artifacts: []ctxcompiler.Artifact{
			{Path: "parser.go", Operation: "edit", Hash: "abc123"},
			{Path: "repro_test.go", Operation: "create", Hash: "def456"},
		},
		RecentCommands: []ctxcompiler.CommandRecord{{Command: "go test ./...", ExitCode: 1}},
		OpenErrors:     []string{"panic: nil dereference in parser.go:41"},
		Subagents:      []ctxcompiler.SubagentView{{ID: "agt_1", Profile: "child", State: "running"}},
		Decisions:      []string{"Chosen: guard clause over refactor."},
	}
}

// 11.4.1: the structured state is deterministic and the artifact
// trail is exact.
func TestStructuredStateDeterministic(t *testing.T) {
	a := testState().Render()
	b := testState().Render()
	if a != b {
		t.Fatal("structured state must render deterministically")
	}
	for _, want := range []string{
		"parser.go edit abc123", "repro_test.go create def456",
		"exit 1: go test ./...", "panic: nil dereference in parser.go:41",
		"Fix the crash on empty input.", "Do not touch unrelated files.",
		"R9 [accepted]", "C9.1 [unverified]", "P2 [in_progress]",
	} {
		if !strings.Contains(a, want) {
			t.Fatalf("structured state misses %q", want)
		}
	}
}

// 11.4.3: every open ID must appear in the summary; missing ones are
// appended mechanically.
func TestSummaryCheckAndRepair(t *testing.T) {
	state := testState()
	summary := "R9 analyzed. P2 underway."
	miss := state.MissingFromSummary(summary)
	if len(miss) == 0 {
		t.Fatal("C9.1 must be reported missing")
	}
	found := false
	for _, id := range miss {
		if id == "C9.1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing IDs must include C9.1: %v", miss)
	}
	repaired := state.RepairSummary(summary, miss)
	for _, id := range state.OpenIDs() {
		if !strings.Contains(repaired, id) {
			t.Fatalf("repaired summary misses open ID %s", id)
		}
	}
	// Closed states are not open: P1 (done) must not be required.
	for _, id := range state.OpenIDs() {
		if id == "P1" {
			t.Fatal("done plan item P1 must not count as open")
		}
	}
}

func tailFixture(n int, per int) []ctxcompiler.ActivityEvent {
	var out []ctxcompiler.ActivityEvent
	for i := 0; i < n; i++ {
		out = append(out, ctxcompiler.ActivityEvent{
			Seq:     int64(i + 1),
			Role:    providers.RoleUser,
			Content: strings.Repeat("m", per),
		})
	}
	return out
}

// 11.8 row 4 (part 1): forcing the threshold compacts with an
// incremental summary, a 20K literal tail and a queryable record.
func TestCompactIncremental(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	counter := fixedCounter{per: 4}
	discarded := tailFixture(40, 4000) // 40 x 1000 tokens
	kept := tailFixture(8, 4000)
	for i := range kept {
		kept[i].Seq = int64(41 + i)
		kept[i].Content = strings.Repeat("k", 4000)
	}
	s := &fake.FakeSummarizer{Summaries: []string{"segment findings: nil guard works."}}
	out, err := ctxcompiler.Compact(context.Background(), cfg, ctxcompiler.CompactInput{
		State:       testState(),
		Revision:    3,
		PrevSummary: "earlier: crash reproduced.",
		Discarded:   discarded,
		Tail:        kept,
		Summarizer:  s,
		Counter:     counter,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.SummaryErr != nil {
		t.Fatalf("summary must succeed: %v", out.SummaryErr)
	}
	if len(s.Segments) != 1 {
		t.Fatalf("summarizer must see exactly one call, got %d", len(s.Segments))
	}
	// Only the discarded segment reaches the model, fused with the
	// previous summary — never a full rewrite.
	for _, k := range kept {
		if strings.Contains(s.Segments[0], k.Content[:64]) {
			t.Fatal("kept tail must not reach the summarizer")
		}
	}
	if !strings.Contains(out.Summary, "earlier: crash reproduced.") ||
		!strings.Contains(out.Summary, "nil guard works") {
		t.Fatalf("summary must fuse prev + segment: %q", out.Summary)
	}
	for _, id := range testState().OpenIDs() {
		if !strings.Contains(out.Block, id) {
			t.Fatalf("block misses open ID %s", id)
		}
	}
	for _, want := range []string{"## Compaction v3", "### Structured state", "### Incremental summary",
		"parser.go edit abc123", "panic: nil dereference"} {
		if !strings.Contains(out.Block, want) {
			t.Fatalf("block misses %q", want)
		}
	}
	if len(out.KeptTail) != len(kept) {
		t.Fatalf("8K tail fits 20K: kept %d events", len(out.KeptTail))
	}
	if out.Record.DiscardedEvents != 40 || out.Record.DiscardedFrom != 1 || out.Record.DiscardedTo != 40 {
		t.Fatalf("record must point at the discarded span: %+v", out.Record)
	}
	if out.Record.BlockHash == "" {
		t.Fatal("record needs the block hash")
	}
}

// 11.8 row 4 (part 2): when the summary fails the compaction still
// works on the structured state alone.
func TestCompactSurvivesSummaryFailure(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	s := &fake.FakeSummarizer{Fail: errors.New("provider down")}
	out, err := ctxcompiler.Compact(context.Background(), cfg, ctxcompiler.CompactInput{
		State:      testState(),
		Revision:   1,
		Discarded:  tailFixture(4, 4000),
		Tail:       tailFixture(2, 100),
		Summarizer: s,
		Counter:    fixedCounter{per: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.SummaryErr == nil {
		t.Fatal("summary error must be recorded")
	}
	for _, id := range testState().OpenIDs() {
		if !strings.Contains(out.Block, id) {
			t.Fatalf("state-only block misses open ID %s", id)
		}
	}
	// A nil summarizer is the same fallback without an error.
	out2, err := ctxcompiler.Compact(context.Background(), cfg, ctxcompiler.CompactInput{
		State:     testState(),
		Revision:  1,
		Discarded: tailFixture(4, 4000),
		Tail:      tailFixture(2, 100),
		Counter:   fixedCounter{per: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out2.SummaryErr != nil || !strings.Contains(out2.Block, "parser.go edit abc123") {
		t.Fatalf("nil summarizer must compact on state alone: %+v", out2)
	}
}

// 11.4.4: the literal tail never splits call/result pairs.
func TestSelectTailKeepsPairs(t *testing.T) {
	counter := fixedCounter{per: 4}
	call := ctxcompiler.ActivityEvent{
		Seq: 9, Role: providers.RoleAssistant, Content: "reading\n",
		ToolCalls: []providers.ToolCall{{ID: "tc_9", Name: "read_file", Arguments: []byte(`{}`)}},
	}
	result := ctxcompiler.ActivityEvent{
		Seq: 10, Role: providers.RoleTool, Content: strings.Repeat("r", 3900),
		ToolResult: &providers.ToolResult{CallID: "tc_9", Name: "read_file", Output: strings.Repeat("r", 3900)},
	}
	head := ctxcompiler.ActivityEvent{Seq: 8, Role: providers.RoleUser, Content: strings.Repeat("h", 100)}
	// Budget fits the result but not the call: the call rides along.
	tail := ctxcompiler.SelectTail([]ctxcompiler.ActivityEvent{head, call, result}, 1000, counter)
	if len(tail) != 2 || tail[0].Seq != 9 || tail[1].Seq != 10 {
		t.Fatalf("pair must stay together, got %d events", len(tail))
	}
}

// 11.8 row 6: long master conversation. Literal history caps at 12K,
// the summary recalculates only at session boundaries, and omitted
// turns stay retrievable.
func TestMasterConversationHistory(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	counter := fixedCounter{per: 4}
	cc := ctxcompiler.NewConversationCompactor(cfg, counter)
	store := &fake.MemHistoryStore{}
	s := &fake.FakeSummarizer{Summaries: []string{"turns 1-20: project setup."}}
	ctx := context.Background()

	var turns []ctxcompiler.ActivityEvent
	for i := 0; i < 30; i++ {
		// Exactly 4000 chars = 1000 tokens each with the test
		// counter, so the 12K tail holds exactly 12 turns.
		prefix := fmt.Sprintf("turn body %02d ", i+1)
		turns = append(turns, ctxcompiler.ActivityEvent{
			Seq:     int64(i + 1),
			Role:    providers.RoleUser,
			Content: prefix + strings.Repeat("t", 4000-len(prefix)),
		})
	}
	// Mid-session below the trigger: nothing recalculates.
	if cc.ShouldRecalculate(cc.LiteralTotal(turns[:6]), false) {
		t.Fatal("6K literal mid-session must not recalculate")
	}
	kept, recalc, err := cc.MaybeCondense(ctx, turns[:6], false, store, s)
	if err != nil || recalc || len(kept) != 6 {
		t.Fatalf("mid-session turns pass through: recalc=%v err=%v", recalc, err)
	}
	// Past 24K literal: condense at the boundary.
	if !cc.ShouldRecalculate(cc.LiteralTotal(turns), false) {
		t.Fatal("30K literal history must recalculate")
	}
	kept, recalc, err = cc.MaybeCondense(ctx, turns, false, store, s)
	if err != nil || !recalc {
		t.Fatalf("boundary must condense: recalc=%v err=%v", recalc, err)
	}
	if total := cc.LiteralTotal(kept); total > 12000 {
		t.Fatalf("literal tail capped at 12K, got %d", total)
	}
	if !strings.Contains(cc.Summary(), "project setup") {
		t.Fatalf("summary must carry the condensed turns: %q", cc.Summary())
	}
	if got := store.Search("turn body 01 "); len(got) == 0 {
		t.Fatal("history_search must retrieve omitted turns")
	}
	if n := len(store.Stored()); n != 19 {
		t.Fatalf("older turns archived, newest kept literal: stored=%d, want 19", n)
	}
	// Session boundary with small history: also condenses (cache is
	// already lost), but a summarizer failure keeps the old summary
	// while still archiving.
	s.Fail = errors.New("provider down")
	more := append(append([]ctxcompiler.ActivityEvent(nil), kept...),
		ctxcompiler.ActivityEvent{Seq: 31, Role: providers.RoleUser, Content: "small follow-up " + strings.Repeat("u", 3990)})
	before := cc.Summary()
	_, recalc, err = cc.MaybeCondense(ctx, more, true, store, s)
	if !recalc {
		t.Fatal("idle-past-TTL with 13K literal must condense")
	}
	if err == nil {
		t.Fatal("summarizer failure must surface")
	}
	if cc.Summary() != before {
		t.Fatal("failed summary must keep the previous text")
	}
	// Incremental: the summarizer saw only archived turns, fused
	// with the running summary.
	if !strings.Contains(s.Segments[0], "turn body 01 ") {
		t.Fatal("first condensation must summarize from the oldest turn")
	}
	for _, seg := range s.Segments {
		if strings.Contains(seg, "turn body 30 ") {
			t.Fatal("summary input must be the discarded span, never the literal newest turn")
		}
	}
	if len(s.Prev) < 2 || s.Prev[1] == "" {
		t.Fatal("second condensation must fuse with the previous summary")
	}
}
