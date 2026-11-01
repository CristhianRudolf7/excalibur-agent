package scopeguard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var checkValue = regexp.MustCompile(`(?:"[^"\n]*"|'[^'\n]*'|\b\d+(?:\.\d+)?\b)`)
var pythonAssertion = regexp.MustCompile(`^assert(?:\s|\()`)
var toleranceAssignment = regexp.MustCompile(`(?i)^\s*(?:atol|rtol|tol|tolerance|epsilon|eps)\s*[:=]`)
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
	// Shell snippets have no provided-file provenance. Only legacy mode
	// treats agent-authored inline scripts as immutable task checks.
	var lines []string
	if !providedChecksEnabled() {
		lines = expectation.FindAllString(cmd, -1)
	}
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

// EXCALIBUR_SCOPE_PROVIDED_CHECKS defaults on. Disabling restores the
// legacy inline-oracle and verbatim expectation comparison.
func providedChecksEnabled() bool {
	v := strings.ToLower(os.Getenv("EXCALIBUR_SCOPE_PROVIDED_CHECKS"))
	return v != "0" && v != "off" && v != "false"
}

func (g *Guard) providedCheck(p string) bool {
	if _, ok := g.Initial[p]; ok {
		return true
	}
	for _, dir := range g.CheckDirs {
		if strings.HasPrefix(p, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// checkFingerprint preserves literals and operators but removes comments and
// insignificant spacing. A Python assert's top-level comma introduces its
// diagnostic message, not its condition. Nested commas remain significant.
func checkFingerprint(source string) string {
	if !providedChecksEnabled() {
		return strings.Join(expectation.FindAllString(source, -1), "\n")
	}
	var lines []string
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimSpace(line)
		var b, semantic strings.Builder
		var quote rune
		escaped, depth := false, 0
		assertion := pythonAssertion.MatchString(line)
		for _, r := range line {
			if quote != 0 {
				b.WriteRune(r)
				semantic.WriteRune(r)
				if escaped {
					escaped = false
				} else if r == '\\' {
					escaped = true
				} else if r == quote {
					quote = 0
				}
				continue
			}
			if r == '#' {
				break
			}
			if r == '\'' || r == '"' {
				quote = r
				b.WriteRune(r)
				semantic.WriteRune(r)
				continue
			}
			if assertion && r == ',' && depth == 0 {
				break
			}
			if r == '(' || r == '[' || r == '{' {
				depth++
			}
			if r == ')' || r == ']' || r == '}' {
				depth--
			}
			semantic.WriteRune(r)
			if r != ' ' && r != '\t' && r != '\r' {
				b.WriteRune(r)
			}
		}
		// Match the source before stripping spaces so keyword boundaries survive.
		if (expectation.MatchString(semantic.String()) || toleranceAssignment.MatchString(semantic.String())) && b.Len() > 0 {
			lines = append(lines, b.String())
		}
	}
	return strings.Join(lines, "\n")
}
