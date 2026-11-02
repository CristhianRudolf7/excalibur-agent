package engine

import (
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
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
		for id, s := range states {
			if s.ExitCode != nil && *s.ExitCode != 0 && (strings.Contains(out[i].Output, id) || strings.Contains(out[i].Args, id)) {
				out[i].IsError = true
			}
		}
	}
	return out
}
