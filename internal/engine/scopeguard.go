package engine

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/scopeguard"
)

func (e *Engine) startScopeGuard(run *Run) {
	if on, set := envSwitch("EXCALIBUR_SCOPE_GUARD"); set && !on {
		return
	}
	if run.scopeGuardStarted || run.Request == "" || run.Workdir == "" {
		return
	}
	run.scopeGuardStarted = true
	g, err := scopeguard.StartRun(run.Workdir, run.Request, run.ID)
	if err != nil {
		run.scopeGuardError = "scope guard snapshot failed: " + err.Error()
		run.InjectNote(run.scopeGuardError)
		return
	}
	run.scopeGuard = g
	run.InjectNote(g.Note())
}

func scopeRefusal(run *Run) string {
	if run.scopeGuardError != "" {
		return run.scopeGuardError
	}
	if run.scopeGuard != nil {
		if reason := run.scopeGuard.Check(); reason != "" {
			return "scope guard refused finish: " + reason
		}
	}
	return ""
}

// ScopeGuardRecord exposes guard evidence after execution stops.
func (run *Run) ScopeGuardRecord() map[string]any {
	if run.scopeGuard != nil {
		return run.scopeGuard.Report()
	}
	return map[string]any{"enabled": false, "error": run.scopeGuardError}
}
