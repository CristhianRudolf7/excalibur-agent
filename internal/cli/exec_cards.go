package cli

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// execFailedLine matches the finished-failure header of an exec tool
// result ("exit: 127 ... alive: false", "exit: unknown ... alive:
// false"). exec always reports StatusOK and carries the exit code
// in text, so the header is the failure signal; running sessions
// ("exit: running ... alive: true") and clean exits never match.
var execFailedLine = regexp.MustCompile(`(?m)^exit: (unknown|[1-9]\d*)\b.*alive: false`)

// strictExecFailedHeader requires an actual numeric failure in the first header.
var strictExecFailedHeader = regexp.MustCompile(`^exit: [1-9]\d*\b[^\n]*alive: false`)

// installedLine matches successful provisioning, not missing-binary detection.
var installedLine = regexp.MustCompile(`(?m)^provision: (installed |verified working command)`)

// skillCardsRunner appends one error-triggered skill card to failed
// tool results in cards mode (Section 12.4). Success results pass
// through byte-identical; other modes bypass entirely (nil index or
// disabled switch). At most one compact card (Section cards.go,
// capped at 40 lines) lands at the end of the failed output with a
// marker, so the records show exactly when the mechanism fired.
//
// With EXCALIBUR_CARDS_STRICT on (default) a card also needs: a
// non-probe command, no successful install of the binary the error
// names in the same output, and no earlier card with the same name
// in this run. Each runner serves one run, so the once-per-name
// record is per run.
type skillCardsRunner struct {
	base       engine.ToolRunner
	index      *skills.Index
	errorCards bool

	mu    sync.Mutex
	fired map[string]bool
}

func (r *skillCardsRunner) Definition(name string) (tools.Definition, error) {
	return r.base.Definition(name)
}

func (r *skillCardsRunner) Execute(ctx context.Context, name string, args json.RawMessage, o tools.ExecuteOptions) (tools.Result, error) {
	res, err := r.base.Execute(ctx, name, args, o)
	if r == nil || r.index == nil || !r.errorCards {
		return res, err
	}
	// Runner-level errors pass through untouched: the engine
	// discards res when err != nil and renders err.Error()
	// instead, so a card appended here would never reach the
	// model (and cancel/transient errors must keep their shape).
	if err != nil {
		return res, err
	}
	if !cardsFailed(name, res, nil) {
		return res, err
	}
	if strings.Contains(res.Output, "[skill card — error-triggered]") {
		return res, err
	}
	skill, reason, ok := skills.MatchError(res.Output)
	if !ok {
		return res, err
	}
	strict := skills.CardsStrictFromEnv(nil)
	if strict {
		if name != "exec" || !strictExecFailedHeader.MatchString(res.Output) {
			return res, err
		}
		if cardsProbeCommand(execCommandOf(args)) {
			return res, err
		}
		if installedInOutput(res.Output, reason) {
			return res, err
		}
		r.mu.Lock()
		done := r.fired[skill]
		r.mu.Unlock()
		if done {
			return res, err
		}
	}
	card, ok := r.index.CardExcerpt(skill, reason, skills.CardMaxLines)
	if !ok {
		return res, err
	}
	if strict {
		r.mu.Lock()
		if r.fired[skill] {
			r.mu.Unlock()
			return res, err
		}
		if r.fired == nil {
			r.fired = map[string]bool{}
		}
		r.fired[skill] = true
		r.mu.Unlock()
	}
	if !strings.HasSuffix(res.Output, "\n") {
		res.Output += "\n"
	}
	res.Output += "\n[skill card — error-triggered]\n" + card
	return res, err
}

// cardsFailed reports whether a tool result failed: non-OK
// statuses, and — because exec always reports StatusOK with the
// exit code in text — finished exec headers with a nonzero or
// unknown exit. err is always nil here (see Execute).
func cardsFailed(name string, res tools.Result, err error) bool {
	if err != nil || res.Status != tools.StatusOK {
		return true
	}
	if name == "exec" && execFailedLine.MatchString(res.Output) {
		return true
	}
	return false
}

// installedInOutput reports whether the output carries a successful
// install or verified working command for the binary the card's
// reason names ("missing binary: X"). Cards without a binary reason
// are not suppressed: an unrelated install does not explain them.
func installedInOutput(output, reason string) bool {
	const prefix = "missing binary: "
	if !strings.HasPrefix(reason, prefix) {
		return false
	}
	bin := strings.TrimPrefix(reason, prefix)
	if bin == "" {
		return false
	}
	named := regexp.MustCompile(`(^|[^A-Za-z0-9_.+-])` + regexp.QuoteMeta(bin) + `($|[^A-Za-z0-9_.+-])`)
	for _, line := range strings.Split(output, "\n") {
		if installedLine.MatchString(line) && named.MatchString(line) {
			return true
		}
	}
	return false
}

// execCommandOf extracts the command string of an exec call, after
// the surface's argument normalization (so "cmd" aliases count).
func execCommandOf(args json.RawMessage) string {
	var a struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(tools.NormalizeArgs("exec", args, tools.DefaultSurfaceTuning()), &a) != nil {
		return ""
	}
	return a.Command
}

// cardsProbeCommand reports whether every segment of a shell command
// is a probe: a check whose non-zero exit is a normal answer ("no
// match", "not installed") rather than a failure. Cards never fire on
// probes. This is a local copy of the probe predicate that the
// exec-results package exports as tools.IsProbeCommand; switch to
// that name when it lands.
func cardsProbeCommand(cmd string) bool {
	segments := tasks.SplitShellSegments(cmd)
	probes := 0
	for _, seg := range segments {
		toks := tasks.ShellWords(seg)
		for len(toks) > 0 && envAssignmentRe.MatchString(toks[0]) {
			toks = toks[1:]
		}
		if len(toks) == 0 || neutralWords(toks) {
			continue
		}
		if !isProbeWords(toks) {
			return false
		}
		probes++
	}
	return probes > 0
}

var envAssignmentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// probeTools are program names whose ordinary negative answer is a
// non-zero exit: test and [, lookups, searches with no match, file
// comparisons and presence checks.
var probeTools = map[string]bool{
	"test": true, "[": true, "which": true, "type": true, "hash": true,
	"ls": true, "stat": true, "id": true, "getent": true, "pgrep": true,
	"pidof": true, "diff": true, "cmp": true, "grep": true, "egrep": true,
	"fgrep": true, "rg": true,
}

// neutralWords are segments that neither make a command a probe nor
// a real step: display, no-op and directory changes, such as the
// `|| true` tail that masks a probe's exit.
func neutralWords(toks []string) bool {
	switch toks[0] {
	case "true", ":", "echo", "printf", "cd":
		return true
	}
	return false
}

func isProbeWords(toks []string) bool {
	prog := toks[0]
	if i := strings.LastIndex(prog, "/"); i >= 0 {
		prog = prog[i+1:]
	}
	args := toks[1:]
	if prog == "command" && len(args) > 0 && (args[0] == "-v" || args[0] == "-V") {
		return true
	}
	if prog == "dpkg" && len(args) > 0 && (args[0] == "-s" || args[0] == "-l" || args[0] == "-L") {
		return true
	}
	if prog == "apt-cache" && len(args) > 0 && (args[0] == "policy" || args[0] == "show") {
		return true
	}
	return probeTools[prog]
}
