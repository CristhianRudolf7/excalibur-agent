package tools

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"mvdan.cc/sh/v3/syntax"
)

// guardSignals checks signal utilities before any shell effects occur. It is an
// accident guard, not a security sandbox: arbitrary programs can send signals.
func guardSignals(command string) error {
	if off("EXCALIBUR_SELF_KILL_GUARD") {
		return nil
	}
	// Avoid parsing unrelated commands, including scripts in other languages.
	if !strings.Contains(command, "kill") && !strings.Contains(command, `\`) {
		return nil
	}
	tree, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return fmt.Errorf("self-kill guard: cannot inspect shell command: %w", err)
	}
	var targets []platform.SignalTarget
	var refusal error
	syntax.Walk(tree, func(node syntax.Node) bool {
		if refusal != nil {
			return false
		}
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}
		if len(call.Args) >= 2 {
			head, _ := literalShellWord(call.Args[0])
			base := filepath.Base(head)
			if base == "eval" || base == "sh" || base == "bash" || base == "dash" {
				start := 1
				if base != "eval" {
					start = -1
					for i, arg := range call.Args[1:] {
						if arg.Lit() == "-c" {
							start = i + 2
							break
						}
					}
				}
				if start >= 0 && start < len(call.Args) {
					inner, ok := literalShellWord(call.Args[start])
					if !ok {
						refusal = fmt.Errorf("dynamic nested shell cannot be checked")
						return false
					}
					refusal = guardSignals(inner)
					if refusal != nil {
						return false
					}
				}
			}
		}
		for i, word := range call.Args {
			commandName, _ := literalShellWord(word)
			name := filepath.Base(commandName)
			if name != "pkill" && name != "killall" && name != "kill" {
				continue
			}
			// Arguments belonging to echo/printf are data, not commands. Known
			// command wrappers are inspected by looking through their prefixes.
			if i > 0 {
				wrapperName, _ := literalShellWord(call.Args[0])
				wrapper := filepath.Base(wrapperName)
				if wrapper != "command" && wrapper != "exec" && wrapper != "sudo" && wrapper != "env" && wrapper != "busybox" && wrapper != "xargs" {
					continue
				}
			}
			args := []string{}
			for _, a := range call.Args[i+1:] {
				v, ok := literalShellWord(a)
				if !ok {
					refusal = fmt.Errorf("dynamic signal arguments cannot be checked")
					return false
				}
				args = append(args, v)
			}
			if targets == nil {
				targets, refusal = platform.ProtectedSignalTargets()
				if refusal != nil {
					return false
				}
			}
			refusal = checkSignalArgs(name, args, targets)
			break
		}
		return refusal == nil
	})
	if refusal != nil {
		return fmt.Errorf("self-kill guard refused command: %v; use explicit PIDs outside the harness and parent process groups", refusal)
	}
	return nil
}

func literalShellWord(w *syntax.Word) (string, bool) {
	return literalShellParts(w.Parts, false)
}

func literalShellParts(parts []syntax.WordPart, quoted bool) (string, bool) {
	var out strings.Builder
	for _, part := range parts {
		switch p := part.(type) {
		case *syntax.Lit:
			for i := 0; i < len(p.Value); i++ {
				c := p.Value[i]
				if c == '\\' && i+1 < len(p.Value) {
					next := p.Value[i+1]
					if !quoted || strings.ContainsRune("$`\"\\\n", rune(next)) {
						i++
						if next != '\n' {
							out.WriteByte(next)
						}
						continue
					}
				}
				out.WriteByte(c)
			}
		case *syntax.SglQuoted:
			out.WriteString(p.Value)
		case *syntax.DblQuoted:
			v, ok := literalShellParts(p.Parts, true)
			if !ok {
				return "", false
			}
			out.WriteString(v)
		default:
			return "", false
		}
	}
	return out.String(), true
}

func checkSignalArgs(name string, args []string, targets []platform.SignalTarget) error {
	if name == "kill" {
		operands := args
		if len(operands) > 0 && (operands[0] == "-s" || operands[0] == "--signal" || operands[0] == "-n") {
			if len(operands) < 2 {
				return fmt.Errorf("missing signal")
			}
			operands = operands[2:]
		} else if len(operands) > 0 && strings.HasPrefix(operands[0], "-") && operands[0] != "--" {
			operands = operands[1:]
		}
		if len(operands) > 0 && operands[0] == "--" {
			operands = operands[1:]
		}
		if len(operands) == 0 {
			return fmt.Errorf("missing PID")
		}
		for _, arg := range operands {
			pid, e := strconv.Atoi(arg)
			if e != nil {
				return fmt.Errorf("nonliteral PID %q", arg)
			}
			if pid == 0 || pid == -1 {
				return fmt.Errorf("broadcast signal targets harness")
			}
			for _, p := range targets {
				if pid == p.PID || pid == -p.Group {
					return fmt.Errorf("PID/group %d targets protected process %d", pid, p.PID)
				}
			}
		}
		return nil
	}
	full, exact, insensitive := false, false, false
	var patterns []string
	options := true
	for _, arg := range args {
		if options && arg == "--" {
			options = false
			continue
		}
		if options && strings.HasPrefix(arg, "-") {
			switch arg {
			case "--full":
				full = true
			case "--exact":
				exact = true
			case "--ignore-case":
				insensitive = true
			default:
				if regexp.MustCompile(`^-(TERM|KILL|INT|HUP|QUIT|STOP|CONT|[0-9]+)$`).MatchString(arg) {
					continue
				}
				for _, flag := range strings.TrimPrefix(arg, "-") {
					switch flag {
					case 'f':
						full = true
					case 'x':
						exact = true
					case 'i':
						insensitive = true
					default:
						return fmt.Errorf("unsupported signal selector %q", arg)
					}
				}
			}
			continue
		}
		patterns = append(patterns, arg)
	}
	if len(patterns) == 0 {
		return fmt.Errorf("missing process pattern")
	}
	if name == "pkill" && len(patterns) != 1 {
		return fmt.Errorf("expected one process pattern")
	}
	for _, pattern := range patterns {
		if name == "killall" {
			if strings.Contains(pattern, "/") {
				return fmt.Errorf("killall executable-path selectors cannot be checked")
			}
			// Linux comm is limited to 15 bytes. killall may match a long
			// operand by this truncated name unless exact-name checks apply.
			for _, p := range targets {
				candidate, processName := pattern, p.Name
				if insensitive {
					candidate, processName = strings.ToLower(candidate), strings.ToLower(processName)
				}
				if len(processName) == 15 && strings.HasPrefix(candidate, processName) {
					return fmt.Errorf("long process name matches protected PID %d", p.PID)
				}
			}
		}
		if name == "killall" {
			pattern = regexp.QuoteMeta(pattern)
			exact = true
		}
		if exact {
			pattern = "^(" + pattern + ")$"
		}
		if insensitive {
			pattern = "(?i)" + pattern
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("invalid process pattern: %w", err)
		}
		for _, p := range targets {
			text := p.Name
			if full {
				text = p.Command
			}
			if re.MatchString(text) {
				return fmt.Errorf("process pattern matches protected PID %d", p.PID)
			}
		}
	}
	return nil
}
