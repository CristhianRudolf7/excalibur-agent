package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

// RefusalItem identifies missing evidence and one check to produce it.
type RefusalItem struct {
	ID      string `json:"id"`
	Missing string `json:"missing"`
	Check   string `json:"check"`
}

// RefusalRecord preserves every reason, including extraction reviews.
type RefusalRecord struct {
	Attempt          int         `json:"attempt"`
	Item             RefusalItem `json:"item"`
	Review           string      `json:"review,omitempty"`
	InstructionQuote string      `json:"instruction_quote,omitempty"`
	Dropped          bool        `json:"dropped,omitempty"`
}

type refusalAudit struct {
	InstructionHash string          `json:"instruction_hash"`
	Attempt         int             `json:"attempt"`
	Items           []RefusalItem   `json:"items"`
	Reviews         []RefusalRecord `json:"reviews,omitempty"`
}

const finishRefusalEntry journal.EntryType = "finish_refusal"

func instructionHash(request string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(request))) }

type refusalState struct {
	loaded   bool
	request  string
	counts   map[string]int
	previous map[string]RefusalItem
	dropped  map[string]string
	reviewed map[string]bool
	records  []RefusalRecord
}

// ActionableRefusalsEnabled is the default-on ablation switch.
func ActionableRefusalsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("EXCALIBUR_ACTIONABLE_REFUSALS"))) {
	case "off", "0", "false", "no":
		return false
	default:
		return true
	}
}

func (r *Run) refusalStateLocked() *refusalState {
	if r.refusals == nil || r.refusals.request != r.Request {
		old := []RefusalRecord(nil)
		if r.refusals != nil {
			old = r.refusals.records
		}
		r.refusals = &refusalState{request: r.Request, counts: map[string]int{}, previous: map[string]RefusalItem{}, dropped: map[string]string{}, reviewed: map[string]bool{}, records: old}
	}
	return r.refusals
}

// RefusalRecords returns a copy for the evaluation and auditing records.
func (r *Run) RefusalRecords() []RefusalRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refusals == nil {
		return nil
	}
	return append([]RefusalRecord(nil), r.refusals.records...)
}

func refusalID(reason string) string { return fmt.Sprintf("G-%x", sha256.Sum256([]byte(reason)))[:14] }
func tokenInReason(reason, token string) bool {
	return regexp.MustCompile(`(^|[^A-Za-z0-9_./-])` + regexp.QuoteMeta(token) + `($|[^A-Za-z0-9_./-])`).MatchString(reason)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

func artifactCheck(a tasks.ArtifactRequirement) string {
	switch a.Kind {
	case tasks.ArtifactExecutable:
		return "test -x " + shellQuote(a.Path)
	case tasks.ArtifactOutput:
		if len(a.Expect) > 0 {
			return "grep -F -- " + shellQuote(a.Expect[0]) + " " + shellQuote(a.Path)
		}
		return "test -s " + shellQuote(a.Path)
	case tasks.ArtifactPort:
		return "curl --fail --silent --show-error http://localhost:" + strconv.Itoa(a.Port) + "/"
	case tasks.ArtifactURL:
		return "curl --fail --silent --show-error " + shellQuote(a.URL)
	default:
		return "test -e " + shellQuote(a.Path)
	}
}

// refusalItems expands combined gate notes without discarding unmet items.
func (e *Engine) refusalItems(run *Run, reasons []string) []RefusalItem {
	run.mu.Lock()
	request := run.Request
	drafts := append([]tasks.DraftCriterion(nil), run.drafts...)
	history := historyTools(run.History)
	run.mu.Unlock()
	artifacts := tasks.ExtractArtifactRequirements(request, e.artifactConfig().MaxArtifacts)
	var items []RefusalItem
	for _, reason := range reasons {
		matched := false
		for _, a := range artifacts {
			token := a.Path
			if token == "" {
				token = a.URL
			}
			if token == "" && a.Port > 0 {
				token = "port " + strconv.Itoa(a.Port)
			}
			if token != "" && tokenInReason(reason, token) && (strings.HasPrefix(reason, "required ") || strings.HasPrefix(reason, "no evidence that ")) {
				items = append(items, RefusalItem{a.ID, reason, artifactCheck(a)})
				matched = true
			}
		}
		for _, d := range drafts {
			if (d.Key != "" && tokenInReason(strings.ToLower(reason), strings.ToLower(d.Key))) || strings.Contains(reason, d.ID+" ") {
				items = append(items, RefusalItem{d.ID, reason, d.Check})
				matched = true
			}
		}
		if !matched {
			check := "Re-read the current instruction and execute the check for: " + reason
			if strings.Contains(reason, "last command errored") || strings.Contains(reason, "verification") || strings.Contains(reason, "changes since") {
				for i := len(history) - 1; i >= 0; i-- {
					if history[i].Name == "exec" {
						var a struct {
							Command string `json:"command"`
						}
						if json.Unmarshal([]byte(history[i].Args), &a) == nil && a.Command != "" {
							check = a.Command
							break
						}
					}
				}
			}
			items = append(items, RefusalItem{refusalID(reason), reason, check})
		}
	}
	// One item can have multiple evidence failures; retain them in a single entry.
	var out []RefusalItem
	index := map[string]int{}
	for _, item := range items {
		if i, ok := index[item.ID]; ok {
			if !strings.Contains(out[i].Missing, item.Missing) {
				out[i].Missing += "; " + item.Missing
			}
		} else {
			index[item.ID] = len(out)
			out = append(out, item)
		}
	}
	return out
}

func (e *Engine) refuseFinish(ctx context.Context, run *Run, reasons []string) string {
	if !ActionableRefusalsEnabled() {
		return "finish refused: " + strings.Join(reasons, "; ")
	}
	e.restoreRefusals(ctx, run)
	items := e.refusalItems(run, reasons)
	run.mu.Lock()
	s := run.refusalStateLocked()
	attempt := run.FinishRefusals + 1
	changed := []RefusalItem{}
	resolved := []string{}
	next := map[string]RefusalItem{}
	for _, item := range items {
		s.counts[item.ID]++
		next[item.ID] = item
		if old, ok := s.previous[item.ID]; !ok || old != item {
			changed = append(changed, item)
		}
		s.records = append(s.records, RefusalRecord{Attempt: attempt, Item: item})
	}
	for id := range s.previous {
		if _, ok := next[id]; !ok {
			resolved = append(resolved, id)
		}
	}
	sort.Strings(resolved)
	repeated := len(s.previous) > 0
	s.previous = next
	run.mu.Unlock()
	// Only extracted requirements can be withdrawn. Core gate obligations remain strict.
	for _, item := range items {
		e.reviewFalseRequirement(ctx, run, item, attempt)
	}
	payload := struct {
		Attempt  int             `json:"attempt"`
		Changed  []RefusalItem   `json:"checklist"`
		Resolved []string        `json:"resolved,omitempty"`
		Next     string          `json:"next_action,omitempty"`
		Reviews  []RefusalRecord `json:"reviews,omitempty"`
	}{Attempt: attempt, Changed: changed, Resolved: resolved}
	// A withdrawn item must not remain the recommended next action.
	active := make([]RefusalItem, 0, len(changed))
	for _, item := range changed {
		if !run.requirementDropped(item.ID) {
			active = append(active, item)
		}
	}
	payload.Changed = active
	if repeated {
		payload.Next = "Re-check the remaining instruction requirements, then call finish with executed evidence."
		for _, item := range items {
			if !run.requirementDropped(item.ID) {
				payload.Next = "Execute " + item.Check + "; inspect its result before calling finish again."
				break
			}
		}
	}
	for _, rec := range run.RefusalRecords() {
		if rec.Attempt == attempt && rec.Review != "" {
			payload.Reviews = append(payload.Reviews, rec)
		}
	}
	raw, _ := json.Marshal(payload)
	note := "finish refused: " + string(raw)
	// The model receives the refusal as its tool result as well as the volatile note.
	run.mu.Lock()
	for i := len(run.History) - 1; i >= 0; i-- {
		tr := run.History[i].ToolResult
		if tr != nil && tr.Name == FinishToolName {
			cp := *tr
			cp.Output = note
			cp.IsError = true
			run.History[i].ToolResult = &cp
			run.History[i].Content = note
			break
		}
	}
	run.mu.Unlock()
	if rec := e.recorder(run.ID); rec != nil {
		audit, _ := json.Marshal(refusalAudit{instructionHash(run.Request), attempt, items, payload.Reviews})
		_, _ = rec.Record(ctx, finishRefusalEntry, audit, "", "", "")
	}
	return note
}

func (e *Engine) reviewFalseRequirement(ctx context.Context, run *Run, item RefusalItem, attempt int) {
	run.mu.Lock()
	s := run.refusalStateLocked()
	if s.counts[item.ID] < 3 || s.reviewed[item.ID] || e.deps.Models == nil {
		run.mu.Unlock()
		return
	}
	request := run.Request
	statement := ""
	for _, d := range run.drafts {
		if d.ID == item.ID {
			statement = d.Statement + " (source: " + d.Source + ")"
		}
	}
	for _, a := range tasks.ExtractArtifactRequirements(request, e.artifactConfig().MaxArtifacts) {
		if a.ID == item.ID {
			statement = a.Detail + " " + a.Path + " " + a.URL + " (source: " + a.Source + ")"
		}
	}
	if statement == "" {
		run.mu.Unlock()
		return
	}
	s.reviewed[item.ID] = true
	run.mu.Unlock()
	input, _ := json.Marshal(map[string]string{"instruction": request, "id": item.ID, "extracted_requirement": statement})
	prompt := "Review only whether this extracted requirement misinterprets the instruction. Missing evidence and repeated refusals are NEVER reasons to drop a real requirement. Keep ambiguous requirements. Return JSON {\"false_requirement\":bool,\"reason\":string,\"instruction_quote\":string}. To drop, quote the exact instruction text establishing that this is an input, example, optional item, or otherwise not required.\n" + string(input)
	resp, _, err := e.verifierChat(ctx, run, "verifier.requirement_review", prompt, "Return only JSON {\"false_requirement\":bool,\"reason\":string,\"instruction_quote\":string}. Missing evidence is never grounds to drop a requirement. Keep ambiguous requirements. A false requirement needs an exact instruction quote establishing it is not required.\n"+string(input), 384, func(reply string) bool {
		var verdict struct {
			False  *bool  `json:"false_requirement"`
			Reason string `json:"reason"`
		}
		return json.Unmarshal([]byte(reply), &verdict) == nil && verdict.False != nil && strings.TrimSpace(verdict.Reason) != ""
	})
	reason := "review unavailable; requirement retained"
	drop := false
	quote := ""
	if err == nil {
		var verdict struct {
			False  bool   `json:"false_requirement"`
			Reason string `json:"reason"`
			Quote  string `json:"instruction_quote"`
		}
		if json.Unmarshal([]byte(resp.Message.Content), &verdict) == nil && strings.TrimSpace(verdict.Reason) != "" {
			reason = verdict.Reason
			quote = verdict.Quote
			drop = verdict.False && strings.TrimSpace(verdict.Quote) != "" && strings.Contains(request, verdict.Quote)
			if verdict.False && !drop {
				reason = "invalid instruction quote; requirement retained: " + reason
			}
		}
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	s = run.refusalStateLocked()
	if run.Request != request {
		return
	}
	if drop {
		s.dropped[item.ID] = reason
	}
	s.records = append(s.records, RefusalRecord{Attempt: attempt, Item: item, Review: reason, InstructionQuote: quote, Dropped: drop})
}

func (r *Run) requirementDropped(id string) bool {
	if !ActionableRefusalsEnabled() {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.refusalStateLocked().dropped[id] != ""
}

// restoreRefusals reconstructs counters and withdrawals from the append-only
// audit trail before validating a resumed run. Older instruction revisions do
// not contribute to the current decision.
func (e *Engine) restoreRefusals(ctx context.Context, run *Run) {
	if !ActionableRefusalsEnabled() {
		return
	}
	run.mu.Lock()
	s := run.refusalStateLocked()
	if s.loaded {
		run.mu.Unlock()
		return
	}
	request := run.Request
	run.mu.Unlock()
	rec := e.recorder(run.ID)
	if rec == nil {
		return
	}
	entries, err := rec.Store.List(ctx, run.ID)
	if err != nil {
		return
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.Request != request {
		return
	}
	s = run.refusalStateLocked()
	if s.loaded {
		return
	}
	s.loaded = true
	for _, entry := range entries {
		if entry.Type != finishRefusalEntry {
			continue
		}
		var audit refusalAudit
		if json.Unmarshal(entry.Payload, &audit) != nil || audit.InstructionHash != instructionHash(request) {
			continue
		}
		s.previous = map[string]RefusalItem{}
		for _, item := range audit.Items {
			s.counts[item.ID]++
			s.previous[item.ID] = item
			s.records = append(s.records, RefusalRecord{Attempt: audit.Attempt, Item: item})
		}
		for _, review := range audit.Reviews {
			s.reviewed[review.Item.ID] = true
			if review.Dropped {
				s.dropped[review.Item.ID] = review.Review
			}
			s.records = append(s.records, review)
		}
		if run.FinishRefusals < audit.Attempt {
			run.FinishRefusals = audit.Attempt
		}
	}
}
