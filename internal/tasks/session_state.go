package tasks

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
