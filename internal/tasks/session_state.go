package tasks

import "strings"

// SessionState is a current process fact, independent of observation text.
type SessionState struct {
	Alive    bool
	ExitCode *int
}

// HistoryRunning uses the session authority when supplied. Text decoding is
// retained only for old/replayed records without a session manager.
func HistoryRunning(h HistoryTool) bool {
	if envBool("EXCALIBUR_SESSION_AUTHORITY", true) && h.SessionStates != nil {
		for _, id := range sessIDRe.FindAllString(h.Args+" "+h.Output, -1) {
			if h.SessionStates[id].Alive {
				return true
			}
		}
		return false
	}
	return isStillRunning(h.Output)
}

// A once-running observation needs a known successful terminal outcome before
// it can become passing evidence. A tombstone with unknown status is not a pass.
func HistorySucceeded(h HistoryTool) bool {
	if h.IsError {
		return false
	}
	if envBool("EXCALIBUR_SESSION_AUTHORITY", true) && h.SessionStates != nil &&
		(isStillRunning(h.Output) || strings.Contains(h.Output, "exit: running")) {
		ids := sessIDRe.FindAllString(h.Args+" "+h.Output, -1)
		if len(ids) == 0 {
			return false
		}
		for _, id := range ids {
			s, ok := h.SessionStates[id]
			if !ok || s.Alive || s.ExitCode == nil || *s.ExitCode != 0 {
				return false
			}
		}
	}
	return true
}

// HistorySessionIDs returns exact session identifiers from one observation.
func HistorySessionIDs(h HistoryTool) []string {
	return sessIDRe.FindAllString(h.Args+" "+h.Output, -1)
}
