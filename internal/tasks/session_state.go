package tasks

import (
	"encoding/json"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// SessionState is a current process fact, independent of observation text.
type SessionState struct {
	Alive    bool
	ExitCode *int
}

// TypedSessionEvidenceEnabled gates process-owned receipt interpretation.
func TypedSessionEvidenceEnabled() bool { return envBool("EXCALIBUR_TYPED_SESSION_EVIDENCE", true) }

// HistoryRunning uses current authority when available and receipts otherwise.
func HistoryRunning(h HistoryTool) bool {
	if TypedSessionEvidenceEnabled() {
		if h.Sessions == nil && h.Name == "exec" && len(ownedSessions(h)) == 0 {
			body := strings.TrimSpace(h.Output)
			return body == "alive: true" || strings.HasPrefix(body, "exit: running")
		}
		for _, receipt := range ownedSessions(h) {
			if envBool("EXCALIBUR_SESSION_AUTHORITY", true) && h.SessionStates != nil {
				if h.SessionStates[receipt.ID].Alive {
					return true
				}
			} else if receipt.Alive {
				return true
			}
		}
		return false
	}
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
	if TypedSessionEvidenceEnabled() {
		if h.IsError {
			return false
		}
		for _, receipt := range ownedSessions(h) {
			alive, code := receipt.Alive, receipt.ExitCode
			if envBool("EXCALIBUR_SESSION_AUTHORITY", true) && h.SessionStates != nil {
				state, ok := h.SessionStates[receipt.ID]
				if !ok {
					return false
				}
				alive, code = state.Alive, state.ExitCode
			}
			if alive || code == nil || *code != 0 {
				return false
			}
		}
		return true
	}
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
	if !TypedSessionEvidenceEnabled() {
		return sessIDRe.FindAllString(h.Args+" "+h.Output, -1)
	}
	var ids []string
	for _, receipt := range ownedSessions(h) {
		ids = append(ids, receipt.ID)
	}
	return ids
}

// Compatibility decoding is restricted to process tools and their own headers
// or explicit session arguments. Ordinary prose cannot acquire process ownership.
func ownedSessions(h HistoryTool) []tools.SessionReceipt {
	if h.Sessions != nil {
		return h.Sessions
	}
	name, args := h.Name, h.Args
	if name == "call_tool" {
		var wrapped struct {
			Name      string          `json:"name"`
			Tool      string          `json:"tool"`
			Arguments json.RawMessage `json:"arguments"`
		}
		_ = json.Unmarshal([]byte(args), &wrapped)
		name = wrapped.Name
		if name == "" {
			name = wrapped.Tool
		}
		args = string(wrapped.Arguments)
	}
	if name != "exec" && name != "exec_io" {
		return nil
	}
	var ids []string
	listingLines := map[string]string{}
	if name == "exec" {
		parts := strings.SplitN(h.Output, "\n", 3)
		header := parts[0]
		if strings.HasPrefix(header, "exit:") && len(parts) > 1 && strings.HasPrefix(parts[1], "session:") {
			header += " " + parts[1]
		}
		// Some older recordings placed their exit status on a second header line.
		if m := sessRe.FindStringSubmatch(header); len(m) == 2 {
			ids = []string{m[1]}
		}
	} else {
		var a struct {
			Session string `json:"session"`
			Action  string `json:"action"`
		}
		_ = json.Unmarshal([]byte(args), &a)
		if a.Session != "" {
			ids = []string{a.Session}
		} else if a.Action == "list" || args == "" {
			for _, line := range strings.Split(h.Output, "\n") {
				if strings.HasPrefix(line, "  |") {
					continue
				}
				fields := strings.Fields(line)
				if len(fields) > 0 && (fields[0] == "-" || fields[0] == "session:") {
					fields = fields[1:]
				}
				if len(fields) < 2 || sessIDRe.FindString(fields[0]) != fields[0] {
					continue
				}
				if strings.HasPrefix(fields[1], "alive=") || fields[1] == "alive:" {
					id := fields[0]
					ids = append(ids, id)
					listingLines[id] = line
				}
			}
		}
	}
	var receipts []tools.SessionReceipt
	for _, id := range ids {
		stateOutput := h.Output
		if line, listed := listingLines[id]; listed {
			stateOutput = line
		}
		alive, _ := sessionState(stateOutput, args)
		receipt := tools.SessionReceipt{ID: id, Alive: alive}
		if match := serviceExitRe.FindStringSubmatch(h.Output); len(match) == 2 && match[1] == "0" {
			zero := 0
			receipt.ExitCode = &zero
		}
		receipts = append(receipts, receipt)
	}
	return receipts
}
