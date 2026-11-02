package engine

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// Refresh every verification path from the session authority. Historical
// observations remain audit evidence; they cannot keep a finished job alive.
func (e *Engine) currentSessionHistory(history []tasks.HistoryTool) []tasks.HistoryTool {
	if !switchOn("EXCALIBUR_SESSION_AUTHORITY") || e.deps.ActiveSessions == nil {
		return history
	}
	states := map[string]tasks.SessionState{}
	for _, s := range e.deps.ActiveSessions() {
		states[string(s.ID)] = tasks.SessionState{Alive: s.Alive, ExitCode: s.ExitCode}
	}
	out := append([]tasks.HistoryTool(nil), history...)
	for i := range out {
		out[i].SessionStates = states
		for _, id := range tasks.HistorySessionIDs(out[i]) {
			s := states[id]
			if s.ExitCode != nil && *s.ExitCode != 0 {
				out[i].IsError = true
			}
		}
	}
	return out
}

// Loop and recheck guidance consult the same authority as completion checks.
func (r *Run) sessionRunning(tc providers.ToolCall, res tools.Result) bool {
	r.mu.Lock()
	facts := r.sessionFacts
	r.mu.Unlock()
	if !switchOn("EXCALIBUR_SESSION_AUTHORITY") || facts == nil {
		return tasks.HistoryRunning(tasks.HistoryTool{Name: tc.Name, Args: string(tc.Arguments), Output: res.Output, Sessions: res.Sessions})
	}
	states := map[string]tasks.SessionState{}
	for _, s := range facts() {
		states[string(s.ID)] = tasks.SessionState{Alive: s.Alive, ExitCode: s.ExitCode}
	}
	return tasks.HistoryRunning(tasks.HistoryTool{Name: tc.Name, Args: string(tc.Arguments), Output: res.Output, Sessions: res.Sessions, SessionStates: states})
}
