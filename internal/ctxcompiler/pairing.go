package ctxcompiler

import (
	"fmt"
	"sort"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// Call/result pairing invariant (Section 19.2 strict wires).
//
// Both provider wires share the internal message format: assistant
// messages carry tool calls (function_call items) and tool messages
// carry results (function_call_output items) keyed by call ID. Strict
// wires reject a request whose pairs are split: Responses answers
// HTTP 400 "No function call found for function_call_output with
// call_id ..." when a result has no call in the same request, and
// Chat Completions rejects the mirror image (assistant tool_calls
// with no following tool message). Long runs cross masking and
// compaction thresholds, and any cut that drops one half of a pair
// trips the rejection and fails the run.
//
// The invariant therefore holds at two levels:
//
//   - Compaction tails keep pairs atomically (see SelectTail): a cut
//     that would start the kept window at a tool result extends back
//     to its call so the whole batch rides along; a result whose call
//     is gone entirely falls into the discarded span, which the
//     compaction summary still covers. Masking never breaks pairs: it
//     replaces outputs in place and keeps every call identity.
//   - Every built request is validated before it reaches the wire and
//     orphans are converted to plain-text summaries (this file). When
//     a strict wire still rejects a request, the engine repairs the
//     run history the same way and retries once.
//
// Every mechanism here has a default-ON switch so it can be measured
// on and off; see the EXCALIBUR_CONTEXT_PAIR_* variables below.

// PairAtomicEnabled reports whether compaction tails keep call/result
// pairs atomically (default ON). EXCALIBUR_CONTEXT_PAIR_ATOMIC=0
// restores the legacy single-pair re-attachment for ablations.
func PairAtomicEnabled(getenv func(string) string) bool {
	return envFlagDefaultOn(getenv, "EXCALIBUR_CONTEXT_PAIR_ATOMIC")
}

// PairCheckEnabled reports whether every built request is validated
// for complete call/result pairs and repaired to plain-text summaries
// before it reaches the wire (default ON).
// EXCALIBUR_CONTEXT_PAIR_CHECK=0 disables the check for ablations.
func PairCheckEnabled(getenv func(string) string) bool {
	return envFlagDefaultOn(getenv, "EXCALIBUR_CONTEXT_PAIR_CHECK")
}

// PairRetryEnabled reports whether the engine repairs the run history
// and retries once after a strict wire rejects an orphan pair
// (default ON). EXCALIBUR_CONTEXT_PAIR_RETRY=0 disables the retry for
// ablations.
func PairRetryEnabled(getenv func(string) string) bool {
	return envFlagDefaultOn(getenv, "EXCALIBUR_CONTEXT_PAIR_RETRY")
}

// PairingReport describes the orphan halves of a message or activity
// list: results whose call is missing and calls whose result is
// missing. Repaired counts the orphans converted to plain text.
type PairingReport struct {
	// OrphanResults holds result call IDs with no matching call.
	OrphanResults []string
	// OrphanCalls holds call IDs with no matching result.
	OrphanCalls []string
	// Repaired counts orphans converted to plain-text summaries.
	Repaired int
}

// Valid reports whether every pair is complete.
func (r PairingReport) Valid() bool {
	return len(r.OrphanResults) == 0 && len(r.OrphanCalls) == 0
}

// Note renders the one-line record for the trial recorder.
func (r PairingReport) Note() string {
	if r.Valid() && r.Repaired == 0 {
		return "pairing intact"
	}
	var parts []string
	if len(r.OrphanResults) > 0 {
		parts = append(parts, "results without calls: ["+strings.Join(r.OrphanResults, " ")+"]")
	}
	if len(r.OrphanCalls) > 0 {
		parts = append(parts, "calls without results: ["+strings.Join(r.OrphanCalls, " ")+"]")
	}
	if r.Repaired > 0 {
		parts = append(parts, fmt.Sprintf("repaired %d", r.Repaired))
	}
	return "pairing repair: " + strings.Join(parts, "; ")
}

// ValidateMessages checks the pairing invariant on one provider
// request: every tool message must answer a call present in the same
// request, and every assistant call must have its result present.
// The check is wire-agnostic because both wires share the internal
// format (Responses function_call/function_call_output and Chat
// Completions tool_calls/tool messages carry the same IDs).
func ValidateMessages(msgs []providers.Message) PairingReport {
	calls := map[string]bool{}
	results := map[string]bool{}
	for _, m := range msgs {
		if m.Role == providers.RoleAssistant {
			for _, tc := range m.ToolCalls {
				calls[tc.ID] = true
			}
		}
		if m.Role == providers.RoleTool {
			if m.ToolResult != nil {
				results[m.ToolResult.CallID] = true
			} else {
				results[""] = true
			}
		}
	}
	var rep PairingReport
	for id := range results {
		if !calls[id] {
			rep.OrphanResults = append(rep.OrphanResults, id)
		}
	}
	for id := range calls {
		if !results[id] {
			rep.OrphanCalls = append(rep.OrphanCalls, id)
		}
	}
	sort.Strings(rep.OrphanResults)
	sort.Strings(rep.OrphanCalls)
	return rep
}

// ValidateActivity checks the pairing invariant on one activity list:
// every tool-result event must have its call event, and every tool
// call must have its result event.
func ValidateActivity(events []ActivityEvent) PairingReport {
	calls := map[string]bool{}
	results := map[string]bool{}
	for _, e := range events {
		for _, tc := range e.ToolCalls {
			calls[tc.ID] = true
		}
		if e.ToolResult != nil {
			results[e.ToolResult.CallID] = true
		}
	}
	var rep PairingReport
	for id := range results {
		if !calls[id] {
			rep.OrphanResults = append(rep.OrphanResults, id)
		}
	}
	for id := range calls {
		if !results[id] {
			rep.OrphanCalls = append(rep.OrphanCalls, id)
		}
	}
	sort.Strings(rep.OrphanResults)
	sort.Strings(rep.OrphanCalls)
	return rep
}

// summaryBound caps converted orphan bodies so a repair never
// inflates the context it is trying to save.
const summaryBound = 1000

// truncBound shortens s to at most n characters, marking the cut.
func truncBound(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...[truncated]"
}

// orphanResultText renders an orphan tool result as plain text.
func orphanResultText(name, callID, output string) string {
	if name == "" {
		name = "tool"
	}
	return fmt.Sprintf("[context pairing repair] orphan %s result (call %s) kept as text; its call left the context:\n%s",
		name, callID, truncBound(output, summaryBound))
}

// orphanCallText renders an orphan tool call as plain text.
func orphanCallText(name, callID, args string) string {
	if name == "" {
		name = "tool"
	}
	return fmt.Sprintf("[context pairing repair] orphan %s call (%s) kept as text; its result left the context (args %s)",
		name, callID, truncBound(args, 300))
}

// RepairMessages converts every orphan half of one provider request to
// a plain-text summary, preserving order: orphan results become user
// text (the model never claimed them), and orphan calls are stripped
// from their assistant message with a summary appended to its content.
// The repaired request always satisfies ValidateMessages.
func RepairMessages(msgs []providers.Message) ([]providers.Message, PairingReport) {
	rep := ValidateMessages(msgs)
	if rep.Valid() {
		return msgs, rep
	}
	orphanResult := map[string]bool{}
	for _, id := range rep.OrphanResults {
		orphanResult[id] = true
	}
	orphanCall := map[string]bool{}
	for _, id := range rep.OrphanCalls {
		orphanCall[id] = true
	}
	out := make([]providers.Message, 0, len(msgs))
	repaired := 0
	for _, m := range msgs {
		switch m.Role {
		case providers.RoleTool:
			id := ""
			if m.ToolResult != nil {
				id = m.ToolResult.CallID
			}
			if !orphanResult[id] {
				out = append(out, m)
				continue
			}
			name, output := "", m.Content
			if m.ToolResult != nil {
				name, output = m.ToolResult.Name, m.ToolResult.Output
				if output == "" {
					output = m.Content
				}
			}
			out = append(out, providers.Message{
				Role:    providers.RoleUser,
				Content: orphanResultText(name, id, output),
			})
			repaired++
		case providers.RoleAssistant:
			if len(m.ToolCalls) == 0 {
				out = append(out, m)
				continue
			}
			kept := make([]providers.ToolCall, 0, len(m.ToolCalls))
			var notes []string
			for _, tc := range m.ToolCalls {
				if !orphanCall[tc.ID] {
					kept = append(kept, tc)
					continue
				}
				notes = append(notes, orphanCallText(tc.Name, tc.ID, string(tc.Arguments)))
				repaired++
			}
			if len(kept) == len(m.ToolCalls) {
				out = append(out, m)
				continue
			}
			content := m.Content
			for _, n := range notes {
				if content == "" {
					content = n
				} else {
					content += "\n" + n
				}
			}
			m.ToolCalls = kept
			if len(m.ToolCalls) == 0 {
				m.ToolCalls = nil
			}
			m.Content = content
			out = append(out, m)
		default:
			out = append(out, m)
		}
	}
	rep.Repaired = repaired
	return out, rep
}

// RepairActivity converts every orphan half of one activity list to a
// plain-text summary in place order: orphan tool-result events become
// user text carrying the output, and orphan calls are stripped from
// their assistant event with a summary appended to its content. Seq
// numbers ride along unchanged. The repaired list always satisfies
// ValidateActivity.
func RepairActivity(events []ActivityEvent) ([]ActivityEvent, PairingReport) {
	rep := ValidateActivity(events)
	if rep.Valid() {
		return events, rep
	}
	orphanResult := map[string]bool{}
	for _, id := range rep.OrphanResults {
		orphanResult[id] = true
	}
	orphanCall := map[string]bool{}
	for _, id := range rep.OrphanCalls {
		orphanCall[id] = true
	}
	out := make([]ActivityEvent, 0, len(events))
	repaired := 0
	for _, e := range events {
		if e.ToolResult != nil && orphanResult[e.ToolResult.CallID] {
			body := e.ToolResult.Output
			if body == "" {
				body = e.Content
			}
			out = append(out, ActivityEvent{
				Seq:     e.Seq,
				Role:    providers.RoleUser,
				Content: orphanResultText(e.ToolResult.Name, e.ToolResult.CallID, body),
			})
			repaired++
			continue
		}
		if len(e.ToolCalls) > 0 {
			kept := make([]providers.ToolCall, 0, len(e.ToolCalls))
			var notes []string
			for _, tc := range e.ToolCalls {
				if !orphanCall[tc.ID] {
					kept = append(kept, tc)
					continue
				}
				notes = append(notes, orphanCallText(tc.Name, tc.ID, string(tc.Arguments)))
				repaired++
			}
			if len(kept) != len(e.ToolCalls) {
				content := e.Content
				for _, n := range notes {
					if content == "" {
						content = n
					} else {
						content += "\n" + n
					}
				}
				e.ToolCalls = kept
				if len(e.ToolCalls) == 0 {
					e.ToolCalls = nil
				}
				e.Content = content
			}
		}
		out = append(out, e)
	}
	rep.Repaired = repaired
	return out, rep
}

// IsOrphanPairError reports whether err is a strict-wire rejection of
// a split call/result pair: the Responses 400 ("No function call
// found for function call output with call_id ...") or the Chat
// Completions mirror (assistant tool_calls with no following tool
// message). Either direction warrants one history repair and retry.
func IsOrphanPairError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	if !strings.Contains(s, "http 400") {
		return false
	}
	if strings.Contains(s, "no function call found") {
		return true
	}
	if strings.Contains(s, "function_call_output") && strings.Contains(s, "call_id") &&
		(strings.Contains(s, "not found") || strings.Contains(s, "missing")) {
		return true
	}
	if strings.Contains(s, "must be followed by tool messages") ||
		(strings.Contains(s, "tool_calls") && strings.Contains(s, "must be a response to")) {
		return true
	}
	return false
}
