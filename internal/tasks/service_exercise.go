package tasks

import (
	"path/filepath"
	"regexp"
	"strings"
)

// ServiceExerciseEnabled accepts live programs exercised by a later client.
func ServiceExerciseEnabled() bool { return envBool("EXCALIBUR_SERVICE_EXERCISE", true) }

var localClientRe = regexp.MustCompile(`(?i)localhost|127\.0\.0\.1|\[?::1\]?`)
var servicePortRe = regexp.MustCompile(`(?i)(?:--?port[= ]+|:)([0-9]{2,5})\b`)
var serviceExitRe = regexp.MustCompile(`(?m)^exit:\s*([0-9]+)\b`)

// ServiceExercised checks ordered tool evidence, never the network. A live
// session running the named program plus a successful later local client is
// a behavioural check; requiring that server to exit would destroy delivery.
func ServiceExercised(history []HistoryTool, key string) bool {
	if !ServiceExerciseEnabled() || key == "" {
		return false
	}
	live := map[string]bool{}
	for _, id := range LiveSessions(history) {
		live[id] = true
	}
	for i, h := range history {
		if h.Name != "exec" || h.IsError {
			continue
		}
		command, ok := historyCommand(h.Args)
		if !ok || !strings.Contains(command, filepath.Base(key)) {
			continue
		}
		session := sessRe.FindStringSubmatch(h.Output)
		if len(session) != 2 || !live[session[1]] {
			continue
		}
		ports := servicePortRe.FindAllStringSubmatch(command+" "+h.Output, -1)
		for _, client := range history[i+1:] {
			if client.Name != "exec" || client.IsError || isStillRunning(client.Output) {
				continue
			}
			if code := serviceExitRe.FindStringSubmatch(client.Output); len(code) == 2 && code[1] != "0" {
				continue
			}
			cmd, ok := historyCommand(client.Args)
			if !ok || strings.TrimSpace(client.Output) == "" {
				continue
			}
			if localClientRe.MatchString(cmd) && !nonExercisingSegment(cmd) {
				return true
			}
			// Clients may load their host from configuration: accept explicit
			// connection evidence for the server's port as well.
			for _, port := range ports {
				if strings.Contains(cmd+" "+client.Output, port[1]) && IsExercisingCall(client) {
					return true
				}
			}
		}
	}
	return false
}
