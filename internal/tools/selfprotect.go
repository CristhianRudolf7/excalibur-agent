package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"mvdan.cc/sh/v3/syntax"
)

// errSignalParse marks a command the shell parser cannot read. It is
// reported as such, not as a refusal, so the message names the real cause.
var errSignalParse = errors.New("self-kill guard: cannot inspect shell command")

// guardSignals checks signal utilities before any shell effects occur and
// reports refusals only. It is an accident guard, not a security sandbox:
// arbitrary programs can send signals.
func guardSignals(command string) error {
	_, err := signalPreflight(nil, command, "")
	return err
}

// preflight runs the checks every session start shares, before any shell
// effect: the task boundary (exec scope only), the self-kill guard and the
// warnings about kill patterns that match this run's own sessions. A
// refusal ends the session with the reason as its output; warnings are
// written ahead of the command's own output.
func (m *SessionManager) preflight(s *Session, cmd string) error {
	if err := CheckCommandBoundary(cmd); err != nil {
		s.appendOut([]byte(err.Error() + "\n"))
		return err
	}
	warnings, err := signalPreflight(m, cmd, s.ID)
	for _, w := range warnings {
		s.appendOut([]byte(w + "\n"))
	}
	if err != nil {
		s.appendOut([]byte(err.Error() + "\n"))
		return err
	}
	return nil
}

// signalPreflight refuses a command that would signal the harness, its
// ancestors or its process groups, and returns warnings for kill patterns
// that match sessions of this run other than exclude. A nil manager skips
// the session warnings.
func signalPreflight(m *SessionManager, command string, exclude SessionID) ([]string, error) {
	if off("EXCALIBUR_SELF_KILL_GUARD") || !mayInspectSignals(command) {
		return nil, nil
	}
	var uses []signalUse
	if err := inspectSignalCommand(command, &uses); err != nil {
		if errors.Is(err, errSignalParse) {
			return nil, err
		}
		return nil, refusalError(err)
	}
	if len(uses) == 0 {
		return nil, nil
	}
	targets, err := harnessSignalTargets()
	if err != nil {
		return nil, refusalError(err)
	}
	if err := refuseSignalUses(uses, targets); err != nil {
		return nil, refusalError(err)
	}
	return m.signalWarnings(uses, command, exclude), nil
}

func refusalError(err error) error {
	return fmt.Errorf("self-kill guard refused command: %v; use explicit PIDs outside the harness and parent process groups", err)
}

// mayInspectSignals keeps the shell parser away from commands that cannot
// send signals.
func mayInspectSignals(command string) bool {
	return strings.Contains(command, "kill") || strings.Contains(command, "pgrep") || strings.Contains(command, `\`)
}

// signalUse is one signal-related invocation found in a command.
type signalUse struct {
	name string   // kill, pkill, killall or pgrep
	args []string // operands after the command name
	// xargs is set for a kill whose PIDs arrive on standard input.
	xargs bool
}

// signalWrappers are commands that run another command given as their
// argument, so a signal command behind them still sends a signal.
var signalWrappers = map[string]bool{"command": true, "exec": true, "sudo": true, "env": true, "busybox": true, "xargs": true}

// inspectSignalCommand collects every signal invocation in command into
// uses, following nested shells. Commands whose targets cannot be read
// statically are refused with a reason.
func inspectSignalCommand(command string, uses *[]signalUse) error {
	tree, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return fmt.Errorf("%w: %v", errSignalParse, err)
	}
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
					if refusal = inspectNested(inner, uses); refusal != nil {
						return false
					}
				}
			}
		}
		for i, word := range call.Args {
			commandName, _ := literalShellWord(word)
			name := filepath.Base(commandName)
			if name != "pkill" && name != "killall" && name != "kill" && name != "pgrep" {
				continue
			}
			// Arguments belonging to echo/printf are data, not commands. Known
			// command wrappers are inspected by looking through their prefixes.
			use := signalUse{name: name}
			if i > 0 {
				wrapperName, _ := literalShellWord(call.Args[0])
				wrapper := filepath.Base(wrapperName)
				if !signalWrappers[wrapper] {
					continue
				}
				use.xargs = wrapper == "xargs"
			}
			for _, a := range call.Args[i+1:] {
				v, ok := literalShellWord(a)
				if !ok {
					refusal = fmt.Errorf("dynamic signal arguments cannot be checked")
					return false
				}
				use.args = append(use.args, v)
			}
			*uses = append(*uses, use)
			break
		}
		return refusal == nil
	})
	return refusal
}

// inspectNested inspects the text given to a nested shell or eval. Nested
// text is a command in its own right, so it gets the same parser.
func inspectNested(inner string, uses *[]signalUse) error {
	if !mayInspectSignals(inner) {
		return nil
	}
	return inspectSignalCommand(inner, uses)
}

// refuseSignalUses applies the refusal rules to every invocation. A pgrep
// is read-only, so it is checked only when its output can reach a signal
// sender in the same command.
func refuseSignalUses(uses []signalUse, targets []platform.SignalTarget) error {
	sender := false
	for _, u := range uses {
		if u.name != "pgrep" {
			sender = true
		}
	}
	for _, u := range uses {
		var err error
		switch {
		case u.name == "pgrep" && !sender:
			continue
		case u.name == "kill" && u.xargs:
			// PIDs come from standard input; check literal operands only.
			operands, opErr := killOperands(u.args)
			if opErr != nil {
				return opErr
			}
			if len(operands) == 0 {
				continue
			}
			err = checkSignalArgs("kill", u.args, targets)
		default:
			err = checkSignalArgs(u.name, u.args, targets)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// harnessSignalTargets returns the processes a command must not signal: the
// harness, its process groups (platform) and every ancestor of the harness,
// so a kill of the launching shell is refused as well.
func harnessSignalTargets() ([]platform.SignalTarget, error) {
	targets, err := platform.ProtectedSignalTargets()
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	for _, t := range targets {
		seen[t.PID] = true
	}
	for _, p := range readProcessTable().ancestors(os.Getpid()) {
		if !seen[p.PID] {
			seen[p.PID] = true
			targets = append(targets, platform.SignalTarget{PID: p.PID, Group: p.PGID, Name: p.Comm, Command: p.Argv})
		}
	}
	return targets, nil
}

// signalWarnings reports pkill and killall patterns that match live sessions
// of this run, and patterns that match the command's own shell. The signal
// still goes out; the warning names what else it reaches.
func (m *SessionManager) signalWarnings(uses []signalUse, command string, exclude SessionID) []string {
	var table processTable
	type liveSession struct {
		id  SessionID
		cmd string
		pid int
	}
	var live []liveSession
	if m != nil {
		m.mu.Lock()
		for _, sid := range m.order {
			s, ok := m.sessions[sid]
			if !ok || sid == exclude {
				continue
			}
			s.mu.Lock()
			pid := s.pidLocked()
			alive := !s.exited
			s.mu.Unlock()
			if alive && pid > 0 {
				live = append(live, liveSession{id: sid, cmd: s.Cmd, pid: pid})
			}
		}
		m.mu.Unlock()
	}
	var warnings []string
	for _, u := range uses {
		if u.name != "pkill" && u.name != "killall" {
			continue
		}
		sel, err := parseSelector(u.name, u.args)
		if err != nil {
			continue
		}
		label := strings.TrimSpace(u.name + " " + strings.Join(u.args, " "))
		if sel.full && sel.matches(command) {
			warnings = append(warnings, fmt.Sprintf("warning: %s matches this command's own shell; that shell will end with it", label))
		}
		if len(live) > 0 && table == nil {
			table = readProcessTable()
		}
		for _, ls := range live {
			for _, p := range table.tree(ls.pid) {
				text := p.Comm
				if sel.full {
					text = p.Argv
				}
				if sel.matches(text) {
					warnings = append(warnings, fmt.Sprintf("warning: %s matches active session %s (%s); its processes are signalled too", label, ls.id, ls.cmd))
					break
				}
			}
		}
	}
	return warnings
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

// signalNumberFlag matches the signal selectors kill and pkill accept as
// a leading dash argument, such as -9, -TERM or -SIGKILL style numbers.
var signalNumberFlag = regexp.MustCompile(`^-(TERM|KILL|INT|HUP|QUIT|STOP|CONT|[0-9]+)$`)

// killOperands returns the PID operands of a kill argument list, with the
// signal selector removed. A missing signal after -s, -n or --signal is an
// error.
func killOperands(args []string) ([]string, error) {
	operands := args
	if len(operands) > 0 && (operands[0] == "-s" || operands[0] == "--signal" || operands[0] == "-n") {
		if len(operands) < 2 {
			return nil, fmt.Errorf("missing signal")
		}
		operands = operands[2:]
	} else if len(operands) > 0 && strings.HasPrefix(operands[0], "-") && operands[0] != "--" {
		operands = operands[1:]
	}
	if len(operands) > 0 && operands[0] == "--" {
		operands = operands[1:]
	}
	return operands, nil
}

// checkKillOperands refuses a kill whose PID or process group operand is
// the harness, one of its groups or one of its ancestors.
func checkKillOperands(args []string, targets []platform.SignalTarget) error {
	operands, err := killOperands(args)
	if err != nil {
		return err
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

// signalSelector is a parsed pkill, killall or pgrep pattern selection.
type signalSelector struct {
	full        bool
	insensitive bool
	raw         []string
	patterns    []*regexp.Regexp
}

// matches reports whether any pattern matches text.
func (s signalSelector) matches(text string) bool {
	for _, re := range s.patterns {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// parseSelector reads the flags and patterns of a pkill, killall or pgrep
// call. Unknown flags are refused for pkill and killall, which signal, and
// ignored for pgrep, which only lists.
func parseSelector(name string, args []string) (signalSelector, error) {
	var sel signalSelector
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
				if signalNumberFlag.MatchString(arg) {
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
						if name == "pgrep" {
							continue
						}
						return sel, fmt.Errorf("unsupported signal selector %q", arg)
					}
				}
			}
			continue
		}
		patterns = append(patterns, arg)
	}
	if len(patterns) == 0 {
		return sel, fmt.Errorf("missing process pattern")
	}
	if (name == "pkill" || name == "pgrep") && len(patterns) != 1 {
		return sel, fmt.Errorf("expected one process pattern")
	}
	sel.full, sel.insensitive, sel.raw = full, insensitive, patterns
	for _, pattern := range patterns {
		expr := pattern
		anchored := exact
		if name == "killall" {
			if strings.Contains(pattern, "/") {
				return sel, fmt.Errorf("killall executable-path selectors cannot be checked")
			}
			// killall matches exact process names; the escaping keeps the
			// pattern literal.
			expr = regexp.QuoteMeta(expr)
			anchored = true
		}
		if anchored {
			expr = "^(" + expr + ")$"
		}
		if insensitive {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return sel, fmt.Errorf("invalid process pattern: %w", err)
		}
		sel.patterns = append(sel.patterns, re)
	}
	return sel, nil
}

// checkSignalArgs refuses a signal invocation whose target matches a
// protected process. kill takes PIDs; pkill, killall and pgrep take patterns.
func checkSignalArgs(name string, args []string, targets []platform.SignalTarget) error {
	if name == "kill" {
		return checkKillOperands(args, targets)
	}
	sel, err := parseSelector(name, args)
	if err != nil {
		return err
	}
	if name == "killall" {
		// Linux comm is limited to 15 bytes. killall may match a long
		// operand by this truncated name unless exact-name checks apply.
		for _, pattern := range sel.raw {
			for _, p := range targets {
				candidate, processName := pattern, p.Name
				if sel.insensitive {
					candidate, processName = strings.ToLower(candidate), strings.ToLower(processName)
				}
				if len(processName) == 15 && strings.HasPrefix(candidate, processName) {
					return fmt.Errorf("long process name matches protected PID %d", p.PID)
				}
			}
		}
	}
	for _, p := range targets {
		text := p.Name
		if sel.full {
			text = p.Command
		}
		if sel.matches(text) {
			return fmt.Errorf("process pattern matches protected PID %d", p.PID)
		}
	}
	return nil
}
