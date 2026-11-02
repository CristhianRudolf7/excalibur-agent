package engine

import (
	"encoding/json"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// Resolve read/wait receipts to their own original exec command. Session writes
// invalidate this association: interactive input changes what the process does.
// Tool execution and its journal always keep the original calls unchanged.
func (r *Run) trialCalls(calls []providers.ToolCall, results []tools.Result) []providers.ToolCall {
	if !r.trialAdviceEnabled(probeSyncSwitch) && !r.trialAdviceEnabled(manualTrialsSwitch) {
		return calls
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	normalized := append([]providers.ToolCall(nil), calls...)
	if r.trialSessions == nil {
		r.trialSessions = map[string]providers.ToolCall{}
	}
	for i, tc := range normalized {
		tc.Name, tc.Arguments = unwrapCallTool(tc.Name, tc.Arguments)
		if tc.Name == "exec_io" {
			var args struct {
				Session string `json:"session"`
				Action  string `json:"action"`
			}
			_ = json.Unmarshal(tc.Arguments, &args)
			if args.Action != "read" && args.Action != "wait_for" {
				if results != nil {
					delete(r.trialSessions, args.Session)
				}
			} else if original, ok := r.trialSessions[args.Session]; ok {
				tc.Name, tc.Arguments = original.Name, original.Arguments
				if results != nil && !tasks.HistoryRunning(tasks.HistoryTool{Name: "exec_io", Args: string(calls[i].Arguments), Output: results[i].Output, Sessions: results[i].Sessions}) {
					delete(r.trialSessions, args.Session)
				}
			}
		} else if tc.Name == "exec" && results != nil && len(tc.Arguments) <= 64<<10 {
			h := tasks.HistoryTool{Name: tc.Name, Args: string(tc.Arguments), Output: results[i].Output, Sessions: results[i].Sessions}
			if tasks.HistoryRunning(h) && len(r.trialSessions) < trialFileLimit {
				for _, id := range tasks.HistorySessionIDs(h) {
					r.trialSessions[id] = tc
				}
			}
		}
		normalized[i] = tc
	}
	return normalized
}
