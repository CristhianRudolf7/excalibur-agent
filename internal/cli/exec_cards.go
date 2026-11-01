package cli

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// execFailedLine matches the finished-failure header of an exec tool
// result ("exit: 127 ... alive: false", "exit: unknown ... alive:
// false"). exec always reports StatusOK and carries the exit code
// in text, so the header is the failure signal; running sessions
// ("exit: running ... alive: true") and clean exits never match.
var execFailedLine = regexp.MustCompile(`(?m)^exit: (unknown|[1-9]\d*)\b.*alive: false`)

// skillCardsRunner appends one error-triggered skill card to failed
// tool results in cards mode (Section 12.4). Success results pass
// through byte-identical; other modes bypass entirely (nil index or
// disabled switch). At most one compact card (Section cards.go,
// capped at 40 lines) lands at the end of the failed output with a
// marker, so the records show exactly when the mechanism fired.
type skillCardsRunner struct {
	base       engine.ToolRunner
	index      *skills.Index
	errorCards bool
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
	card, ok := r.index.ErrorCardFor(res.Output, skills.CardMaxLines)
	if !ok || strings.Contains(res.Output, "[skill card — error-triggered]") {
		return res, err
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
