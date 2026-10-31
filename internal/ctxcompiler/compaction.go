package ctxcompiler

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// This file implements Section 11.4: the compaction trigger with the
// 500 000-token ceiling, the deterministic structured state, the
// incremental model summary with a deterministic check, the 20K
// literal tail without split call/result pairs, the compaction
// record, and the long master-conversation history.

// CompactionThreshold returns the trigger for a model window:
// min(ceiling, 0.85*window-reserve). Windows of 500 000 tokens and
// above compact at the configured ceiling; smaller windows compact at
// the window fraction. Degenerate windows clamp to 1024.
func CompactionThreshold(windowTokens, reserveTokens int, cfg Config) int {
	return CompactionThresholdForModel(windowTokens, reserveTokens, cfg, 0)
}

// CompactionThresholdForModel is CompactionThreshold with an optional
// per-model ceiling: modelCeiling lowers the trigger when evaluation
// shows degradation before the default (11.4). A non-positive
// modelCeiling selects the configured ceiling.
func CompactionThresholdForModel(windowTokens, reserveTokens int, cfg Config, modelCeiling int) int {
	ceiling := cfg.CompactionCeilingTokens
	if modelCeiling > 0 && modelCeiling < ceiling {
		ceiling = modelCeiling
	}
	frac := int(0.85*float64(windowTokens)) - reserveTokens
	if frac < 1024 {
		frac = 1024
	}
	if frac > ceiling {
		return ceiling
	}
	return frac
}

// ShouldCompact reports whether estimatedTokens trips the trigger.
// The estimate carries the configured safety margin (11.7).
func ShouldCompact(estimatedTokens, windowTokens, reserveTokens int, cfg Config) bool {
	return ShouldCompactForModel(estimatedTokens, windowTokens, reserveTokens, cfg, 0)
}

// ShouldCompactForModel is ShouldCompact with a per-model ceiling.
func ShouldCompactForModel(estimatedTokens, windowTokens, reserveTokens int, cfg Config, modelCeiling int) bool {
	return WithMargin(estimatedTokens, cfg.EstimateMargin) >=
		CompactionThresholdForModel(windowTokens, reserveTokens, cfg, modelCeiling)
}

// Artifact is one entry of the artifact trail: files changed (path,
// operation, hash) from the edit history. The trail is what
// summarizers preserve worst, so the code generates it (11.4.1).
type Artifact struct {
	Path      string
	Operation string
	Hash      string
}

// CommandRecord is one recent command with its exit code.
type CommandRecord struct {
	Command  string
	ExitCode int
}

// SubagentView is the compact state of one subagent.
type SubagentView struct {
	ID      string
	Profile string
	State   string
}

// StructuredState is the deterministic compaction state built from
// the database and the journal, never from a summary (11.4.1).
type StructuredState struct {
	Objective         string
	OriginalRequest   string
	FixedInstructions []string
	Requirements      []RequirementView
	Criteria          []CriterionView
	Plan              []PlanItemView
	NextSteps         []string
	Artifacts         []Artifact
	RecentCommands    []CommandRecord
	OpenErrors        []string
	Subagents         []SubagentView
	Decisions         []string
}

// closedState marks lifecycle states that are no longer open. Open
// identifiers must appear in the model summary; see OpenIDs.
var closedState = map[string]bool{
	"done": true, "satisfied": true, "retired": true,
	"canceled": true, "cancelled": true, "not_applicable": true,
}

// OpenIDs returns every open identifier: requirements, criteria and
// plan items whose state is not closed, in code order.
func (s StructuredState) OpenIDs() []string {
	var ids []string
	for _, r := range s.Requirements {
		if !closedState[strings.ToLower(r.State)] {
			ids = append(ids, r.Code)
		}
	}
	for _, c := range s.Criteria {
		if !closedState[strings.ToLower(c.State)] {
			ids = append(ids, c.Code)
		}
	}
	for _, p := range s.Plan {
		if !closedState[strings.ToLower(p.State)] {
			ids = append(ids, p.Code)
		}
	}
	sort.Strings(ids)
	return ids
}

// openTexts maps every open identifier to its one-line text for the
// mechanical repair step.
func (s StructuredState) openTexts() map[string]string {
	out := make(map[string]string)
	for _, r := range s.Requirements {
		if !closedState[strings.ToLower(r.State)] {
			out[r.Code] = r.Code + " [" + r.State + "] " + r.Text
		}
	}
	for _, c := range s.Criteria {
		if !closedState[strings.ToLower(c.State)] {
			out[c.Code] = c.Code + " [" + c.State + "] " + c.Statement
		}
	}
	for _, p := range s.Plan {
		if !closedState[strings.ToLower(p.State)] {
			out[p.Code] = p.Code + " [" + p.State + "] " + p.Title
		}
	}
	return out
}

// Render serializes the structured state deterministically.
func (s StructuredState) Render() string {
	var b strings.Builder
	b.WriteString("objective: " + s.Objective + "\n")
	b.WriteString("original request:\n" + s.OriginalRequest)
	if !strings.HasSuffix(s.OriginalRequest, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("fixed instructions:\n")
	for _, f := range s.FixedInstructions {
		b.WriteString("- " + f + "\n")
	}
	reqs := append([]RequirementView(nil), s.Requirements...)
	sort.SliceStable(reqs, func(i, j int) bool { return reqs[i].Code < reqs[j].Code })
	b.WriteString("requirements:\n")
	for _, r := range reqs {
		b.WriteString("- " + r.Code + " [" + r.State + "] " + r.Text + "\n")
	}
	crits := append([]CriterionView(nil), s.Criteria...)
	sort.SliceStable(crits, func(i, j int) bool { return crits[i].Code < crits[j].Code })
	b.WriteString("criteria:\n")
	for _, c := range crits {
		b.WriteString("- " + c.Code + " [" + c.State + "] " + c.Statement + "\n")
	}
	plan := append([]PlanItemView(nil), s.Plan...)
	sort.SliceStable(plan, func(i, j int) bool { return plan[i].Code < plan[j].Code })
	b.WriteString("plan:\n")
	for _, p := range plan {
		b.WriteString("- " + p.Code + " [" + p.State + "] " + p.Title + "\n")
	}
	b.WriteString("next steps:\n")
	for _, n := range s.NextSteps {
		b.WriteString("- " + n + "\n")
	}
	arts := append([]Artifact(nil), s.Artifacts...)
	sort.SliceStable(arts, func(i, j int) bool {
		if arts[i].Path != arts[j].Path {
			return arts[i].Path < arts[j].Path
		}
		return arts[i].Operation < arts[j].Operation
	})
	b.WriteString("artifacts:\n")
	for _, a := range arts {
		b.WriteString("- " + a.Path + " " + a.Operation + " " + a.Hash + "\n")
	}
	b.WriteString("recent commands:\n")
	for _, c := range s.RecentCommands {
		fmt.Fprintf(&b, "- exit %d: %s\n", c.ExitCode, c.Command)
	}
	b.WriteString("open errors:\n")
	for _, e := range s.OpenErrors {
		b.WriteString("- " + e + "\n")
	}
	subs := append([]SubagentView(nil), s.Subagents...)
	sort.SliceStable(subs, func(i, j int) bool { return subs[i].ID < subs[j].ID })
	b.WriteString("subagents:\n")
	for _, s := range subs {
		b.WriteString("- " + s.ID + " [" + s.Profile + "/" + s.State + "]\n")
	}
	b.WriteString("decisions:\n")
	for _, d := range s.Decisions {
		b.WriteString("- " + d + "\n")
	}
	return b.String()
}

// MissingFromSummary returns the open identifiers absent from
// summary. The deterministic check of 11.4.3 requires this list to
// be empty.
func (s StructuredState) MissingFromSummary(summary string) []string {
	var missing []string
	for _, id := range s.OpenIDs() {
		if !strings.Contains(summary, id) {
			missing = append(missing, id)
		}
	}
	return missing
}

// RepairSummary appends the missing open identifiers mechanically so
// the block always carries every open ID even when the model drops
// one (11.4.3).
func (s StructuredState) RepairSummary(summary string, missing []string) string {
	if len(missing) == 0 {
		return summary
	}
	texts := s.openTexts()
	var b strings.Builder
	b.WriteString(summary)
	if !strings.HasSuffix(summary, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("Open items missing from the summary (appended mechanically):\n")
	for _, id := range missing {
		if t, ok := texts[id]; ok {
			b.WriteString("- " + t + "\n")
		} else {
			b.WriteString("- " + id + "\n")
		}
	}
	return b.String()
}

// Summarizer produces the model half of a compaction: a summary of
// what the code cannot know (findings, discarded hypotheses and why,
// reasoning conclusions, pertinent user context). It only ever sees
// the discarded segment plus the previous summary, never the whole
// context (11.4.2). The fixed template lives with the caller; the
// extractor role (a cheap model) fills it.
type Summarizer interface {
	// Summarize condenses the discarded segment, fusing it with the
	// previous incremental summary.
	Summarize(ctx context.Context, discardedSegment, prevSummary string) (string, error)
}

// CompactionRecord points at the discarded span, which stays
// queryable through history_search (11.4.5).
type CompactionRecord struct {
	Revision        int
	DiscardedFrom   int64
	DiscardedTo     int64
	DiscardedEvents int
	DiscardedTokens int
	SummaryTokens   int
	KeptTailTokens  int
	BlockHash       string
}

// CompactInput is one compaction pass.
type CompactInput struct {
	// State is the deterministic structured state.
	State StructuredState
	// Revision numbers the compaction.
	Revision int
	// PrevSummary is the previous incremental summary, "" on first.
	PrevSummary string
	// Discarded is the activity segment being dropped.
	Discarded []ActivityEvent
	// Tail is the recent activity the 20K literal window selects
	// from.
	Tail []ActivityEvent
	// TailBudgetTokens overrides the post-compaction literal budget;
	// non-positive selects Config.PostCompactionLiteralTokens.
	TailBudgetTokens int
	// Summarizer may be nil: without a summary the compaction still
	// works on the structured state alone (11.4 fallback).
	Summarizer Summarizer
	// Counter may be nil for a default Estimator.
	Counter Counter
}

// CompactOutput is the new context middle: the renderable block plus
// its record and the kept literal tail.
type CompactOutput struct {
	// Block is L0-L3 re-render material: structured state plus the
	// (possibly repaired) summary.
	Block string
	// Summary is the repaired incremental summary alone.
	Summary string
	// SummaryErr records a summarizer failure; Block is still valid.
	SummaryErr error
	// KeptTail is the literal tail, pairs intact.
	KeptTail []ActivityEvent
	// Record points at the discarded span.
	Record CompactionRecord
}

// Compact runs one compaction pass (11.4.1-11.4.5).
func Compact(ctx context.Context, cfg Config, in CompactInput) (CompactOutput, error) {
	if err := cfg.Validate(); err != nil {
		return CompactOutput{}, err
	}
	counter := in.Counter
	if counter == nil {
		counter = NewEstimator(0)
	}
	var out CompactOutput
	segmentText := renderEvents(in.Discarded)
	out.Record = CompactionRecord{
		Revision:        in.Revision,
		DiscardedEvents: len(in.Discarded),
		DiscardedTokens: counter.Count(segmentText),
	}
	if len(in.Discarded) > 0 {
		seqs := make([]int64, 0, len(in.Discarded))
		for _, e := range in.Discarded {
			seqs = append(seqs, e.Seq)
		}
		sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
		out.Record.DiscardedFrom = seqs[0]
		out.Record.DiscardedTo = seqs[len(seqs)-1]
	}
	summary := ""
	if in.Summarizer != nil && strings.TrimSpace(segmentText) != "" {
		s, err := in.Summarizer.Summarize(ctx, segmentText, in.PrevSummary)
		if err != nil {
			out.SummaryErr = err
		} else {
			summary = s
		}
	} else if strings.TrimSpace(segmentText) == "" {
		summary = in.PrevSummary
	}
	summary = in.State.RepairSummary(summary, in.State.MissingFromSummary(summary))
	out.Summary = summary
	out.Record.SummaryTokens = counter.Count(summary)
	var b strings.Builder
	fmt.Fprintf(&b, "## Compaction v%d\n", in.Revision)
	b.WriteString("### Structured state\n")
	b.WriteString(in.State.Render())
	b.WriteString("### Incremental summary\n")
	b.WriteString(summary)
	if !strings.HasSuffix(summary, "\n") {
		b.WriteString("\n")
	}
	out.Block = b.String()
	out.Record.BlockHash = HashLayer(out.Block)
	budget := in.TailBudgetTokens
	if budget <= 0 {
		budget = cfg.PostCompactionLiteralTokens
	}
	out.KeptTail = SelectTail(in.Tail, budget, counter)
	tailText := renderEvents(out.KeptTail)
	out.Record.KeptTailTokens = counter.Count(tailText)
	return out, nil
}

// eventCost prices one activity event exactly as it renders on the
// wire, framing included, so tails hold against their budget.
func eventCost(e ActivityEvent, counter Counter) int {
	return counter.Count(renderEvents([]ActivityEvent{e}))
}

// SelectTail keeps the newest events within budget tokens without
// splitting call/result pairs: whole events only, so the tail may
// undershoot the budget by less than one event. When the oldest kept
// event is a tool result whose call is the immediate predecessor,
// the call rides along even past the budget (11.4.4).
func SelectTail(events []ActivityEvent, budgetTokens int, counter Counter) []ActivityEvent {
	if counter == nil {
		counter = NewEstimator(0)
	}
	if len(events) == 0 || budgetTokens <= 0 {
		return nil
	}
	sorted := append([]ActivityEvent(nil), events...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Seq != sorted[j].Seq {
			return sorted[i].Seq < sorted[j].Seq
		}
		return sorted[i].Content < sorted[j].Content
	})
	total := 0
	start := len(sorted)
	for i := len(sorted) - 1; i >= 0; i-- {
		cost := eventCost(sorted[i], counter)
		if total+cost > budgetTokens && start < len(sorted) {
			break // keep at least the newest event
		}
		total += cost
		start = i
	}
	kept := sorted[start:]
	// Re-attach a split pair head: a leading tool result whose call
	// is the immediate predecessor joins the tail.
	for start > 0 {
		head := sorted[start]
		prev := sorted[start-1]
		if head.ToolResult == nil || len(prev.ToolCalls) == 0 {
			break
		}
		match := false
		for _, tc := range prev.ToolCalls {
			if tc.ID == head.ToolResult.CallID {
				match = true
				break
			}
		}
		if !match {
			break
		}
		start--
		kept = sorted[start:]
	}
	out := make([]ActivityEvent, len(kept))
	copy(out, kept)
	return out
}

// HistoryStore is the queryable past: compacted-away and condensed
// turns stay retrievable through history_search (11.4.5, 11.4 master
// history).
type HistoryStore interface {
	// Append stores turns that left the literal context.
	Append(turns []ActivityEvent)
	// Search returns stored turns matching query, newest first.
	Search(query string) []ActivityEvent
}

// ConversationCompactor manages a long master history (11.4 master
// rules): the last MasterHistoryTokens run literal in L4, older turns
// condense into an incremental summary recalculated only at session
// boundaries — resuming past the cache TTL, or literal history past
// MasterHistoryRecalcTokens — and everything omitted stays
// retrievable through the HistoryStore while durable facts go to
// memory (10.5).
type ConversationCompactor struct {
	literalBudget int
	recalcTrigger int
	counter       Counter
	summary       string
	throughSeq    int64
	condensations int
}

// NewConversationCompactor builds the master history manager from
// cfg. A nil counter selects a default Estimator.
func NewConversationCompactor(cfg Config, counter Counter) *ConversationCompactor {
	if counter == nil {
		counter = NewEstimator(0)
	}
	return &ConversationCompactor{
		literalBudget: cfg.MasterHistoryTokens,
		recalcTrigger: cfg.MasterHistoryRecalcTokens,
		counter:       counter,
	}
}

// Summary returns the current incremental conversation summary.
func (c *ConversationCompactor) Summary() string { return c.summary }

// ThroughSeq returns the newest condensed sequence number.
func (c *ConversationCompactor) ThroughSeq() int64 { return c.throughSeq }

// Condensations counts recalculations performed.
func (c *ConversationCompactor) Condensations() int { return c.condensations }

// ShouldRecalculate reports whether the summary must be recalculated:
// literal history past the recalc trigger, or a session boundary
// (resume past the cache TTL) with history over the literal budget.
func (c *ConversationCompactor) ShouldRecalculate(literalTokens int, idlePastTTL bool) bool {
	if literalTokens > c.recalcTrigger {
		return true
	}
	return idlePastTTL && literalTokens > c.literalBudget
}

// LiteralTotal prices one turn list.
func (c *ConversationCompactor) LiteralTotal(turns []ActivityEvent) int {
	return counterCount(renderEvents(turns), c.counter)
}

// MaybeCondense recalculates the summary only at session boundaries,
// moves the older turns into store, and returns the literal tail. It
// returns recalculated=false with turns untouched when no boundary
// applies. A summarizer failure keeps the previous summary: the
// turns are still archived, so nothing is lost.
func (c *ConversationCompactor) MaybeCondense(ctx context.Context, turns []ActivityEvent, idlePastTTL bool, store HistoryStore, s Summarizer) (kept []ActivityEvent, recalculated bool, err error) {
	literal := counterCount(renderEvents(turns), c.counter)
	if !c.ShouldRecalculate(literal, idlePastTTL) {
		return turns, false, nil
	}
	kept = SelectTail(turns, c.literalBudget, c.counter)
	keptSet := make(map[int64]bool, len(kept))
	for _, e := range kept {
		keptSet[e.Seq] = true
	}
	var older []ActivityEvent
	var newestOlder int64 = -1
	for _, e := range turns {
		if !keptSet[e.Seq] {
			older = append(older, e)
			if e.Seq > newestOlder {
				newestOlder = e.Seq
			}
		}
	}
	if store != nil && len(older) > 0 {
		store.Append(older)
	}
	if s != nil && len(older) > 0 {
		if err := ctx.Err(); err != nil {
			return kept, false, err
		}
		merged, serr := s.Summarize(ctx, renderEvents(older), c.summary)
		if serr == nil {
			c.summary = merged
		} else {
			err = serr
		}
	}
	if newestOlder >= 0 {
		c.throughSeq = newestOlder
	}
	c.condensations++
	return kept, true, err
}

func counterCount(text string, counter Counter) int {
	if counter == nil {
		return NewEstimator(0).Count(text)
	}
	return counter.Count(text)
}
