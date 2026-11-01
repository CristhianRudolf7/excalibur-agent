package scopeguard

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
)

var checkValue = regexp.MustCompile(`(?:"[^"\n]*"|'[^'\n]*'|\b\d+(?:\.\d+)?\b)`)
var checkPath = regexp.MustCompile(`(?i)(?:^|[/_.-])(?:tests?|checks?|verify|verification|spec)(?:[/_.-]|$)`)

// ObserveCall catches inline checks as well as file-backed checks. A changed
// literal in an otherwise identical failed oracle is never evidence of repair.
func (g *Guard) ObserveCall(name, args, output string, failed bool) {
	defer func() {
		if err := g.save(); err != nil {
			g.stateError = "scope snapshot persistence failed: " + err.Error()
		}
	}()
	if regexp.MustCompile(`(?m)^exit:\s*[1-9][0-9]*\b`).MatchString(output) {
		failed = true
	}
	if name != "exec" && name != "exec_io" {
		return
	}
	var body struct {
		Command string `json:"command"`
		Cmd     string `json:"cmd"`
	}
	_ = json.Unmarshal([]byte(args), &body)
	cmd := body.Command
	if cmd == "" {
		cmd = body.Cmd
	}
	if cmd == "" {
		return
	}
	lines := expectation.FindAllString(cmd, -1)
	for _, line := range lines {
		key := checkValue.ReplaceAllString(line, "<value>")
		if old, ok := g.inlineChecks[key]; ok && old != line {
			g.violations = append(g.violations, "test-tampering: inline expected value changed after failed check: "+old+" -> "+line)
		}
		if failed {
			g.inlineChecks[key] = line
		}
	}
	// File expectations are sampled after executed checks, including check
	// failures signaled in output even when a shell swallowed the exit status.
	if failed && (strings.Contains(cmd, "test") || strings.Contains(cmd, "python") || strings.Contains(cmd, "check") || strings.Contains(cmd, "assert")) {
		g.observe(true, func(p string) bool {
			if checkPath.MatchString(p) {
				return true
			}
			rel, _ := filepath.Rel(g.Root, p)
			for _, arg := range strings.Fields(cmd) {
				arg = strings.Trim(arg, "\"';")
				if arg == p || arg == rel || arg == "./"+rel {
					return true
				}
			}
			return false
		})
	} else {
		g.Observe(false)
	}
}
