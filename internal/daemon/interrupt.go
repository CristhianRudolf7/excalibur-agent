// Fast-lane control orders across runs (Section 17.5).
//
// Every order here resolves deterministically and applies without a
// master turn and without a model call: the reception fast lane (6.1)
// dispatches them even when the provider is down. Master-turn
// interruption follows the Codex pattern: cancel the token, allow a
// brief grace for a clean stop, abort, and leave a visible marker so
// the model sees the interruption (17.5).
//
// The Control interface is the narrow seam over the orchestrator (H8
// owns it): the daemon never walks the agent tree itself, so cascades
// stay in one place.
package daemon

import (
	"fmt"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// TurnInterruptGrace is the brief margin between cancelling a master
// turn and aborting it (17.5). Recorded here rather than tuned
// silently; see docs/decisions.
const TurnInterruptGrace = time.Second

// OrderKind identifies a 17.5 control order.
type OrderKind int

// Control order kinds.
const (
	// OrderStopTurn interrupts the running master turn in a
	// conversation; background work continues.
	OrderStopTurn OrderKind = iota
	// OrderPause pauses one target and its subtree, resumable.
	OrderPause
	// OrderCancel cancels one target and its descendants after the
	// caller confirms (confirmation happens above this seam).
	OrderCancel
	// OrderPauseAll pauses every run of the user at once.
	OrderPauseAll
	// OrderResume returns runs parked by a pause order to queued.
	OrderResume
)

// String reports the order kind for logs and tests.
func (k OrderKind) String() string {
	switch k {
	case OrderStopTurn:
		return "stop_turn"
	case OrderPause:
		return "pause"
	case OrderCancel:
		return "cancel"
	case OrderPauseAll:
		return "pause_all"
	default:
		return "resume"
	}
}

// Order is one parsed control order.
type Order struct {
	Kind OrderKind
	// Target names the run, task or conversation the order acts on.
	// Empty for OrderStopTurn (current conversation) and
	// OrderPauseAll.
	Target string
	// Raw keeps the original text for the audit trail.
	Raw string
}

// ParseOrder maps user text to a 17.5 order. It accepts the slash
// commands and the short Spanish and English forms of 17.5; anything
// else is an error so the fast lane never guesses a control target.
func ParseOrder(text string) (Order, error) {
	raw := text
	t := strings.TrimSpace(strings.ToLower(text))
	switch {
	case t == "/stop" || t == "para" || t == "stop":
		return Order{Kind: OrderStopTurn, Raw: raw}, nil
	case t == "/stop all" || t == "para todo" || t == "stop all":
		return Order{Kind: OrderPauseAll, Raw: raw}, nil
	}
	fields := strings.Fields(t)
	if len(fields) == 0 {
		return Order{}, fmt.Errorf("daemon: empty control order")
	}
	head, rest := fields[0], strings.TrimSpace(strings.TrimPrefix(t, fields[0]))
	switch head {
	case "/pause", "pausa", "pause", "deten", "detén":
		if rest == "" {
			return Order{}, fmt.Errorf("daemon: pause needs a target")
		}
		return Order{Kind: OrderPause, Target: rest, Raw: raw}, nil
	case "/cancel", "cancela", "cancel", "descarta":
		if rest == "" {
			return Order{}, fmt.Errorf("daemon: cancel needs a target")
		}
		return Order{Kind: OrderCancel, Target: rest, Raw: raw}, nil
	case "/resume", "sigue", "resume", "continua", "continúa":
		if rest == "" {
			return Order{}, fmt.Errorf("daemon: resume needs a target")
		}
		return Order{Kind: OrderResume, Target: rest, Raw: raw}, nil
	}
	// "para X" / "stop X": pause target X (17.5). A target starting
	// with "todo" plus more words is ambiguous with "para todo"
	// (pause all) and is rejected rather than guessed.
	if (strings.HasPrefix(t, "para ") || strings.HasPrefix(t, "stop ")) && len(fields) > 1 {
		target := strings.TrimSpace(t[strings.Index(t, " ")+1:])
		if strings.HasPrefix(target, "todo ") {
			return Order{}, fmt.Errorf("daemon: ambiguous order %q: use exact \"para todo\" for pause-all", text)
		}
		if target == "todo" {
			return Order{}, fmt.Errorf("daemon: ambiguous order %q: use exact \"para todo\" for pause-all", text)
		}
		return Order{Kind: OrderPause, Target: target, Raw: raw}, nil
	}
	return Order{}, fmt.Errorf("daemon: not a control order: %q", text)
}

// Control is the orchestrator seam for 17.5 orders. Every method is
// synchronous and deterministic: no model call, so orders work with
// the provider down.
type Control interface {
	// InterruptTurn cancels the running master turn in the
	// conversation, waits TurnInterruptGrace for a clean stop,
	// aborts and leaves the interruption marker.
	InterruptTurn(conversation string) error
	// PauseRun pauses the target and its subtree with an immediate
	// interrupt; resumable.
	PauseRun(target string, reason types.PauseReason) error
	// CancelRun cancels the target and its descendants definitively.
	CancelRun(target string) error
	// PauseAll pauses every run of the user with an immediate
	// interrupt.
	PauseAll() error
	// ResumeRun returns what a pause order parked to queued.
	ResumeRun(target string) error
}

// Apply dispatches a parsed order over the fast lane: resolution and
// application only, zero model calls by construction (there is no
// model handle anywhere on this path).
func Apply(o Order, conversation string, ctl Control) error {
	switch o.Kind {
	case OrderStopTurn:
		return ctl.InterruptTurn(conversation)
	case OrderPause:
		return ctl.PauseRun(o.Target, types.PauseUser)
	case OrderCancel:
		return ctl.CancelRun(o.Target)
	case OrderPauseAll:
		return ctl.PauseAll()
	case OrderResume:
		return ctl.ResumeRun(o.Target)
	default:
		return fmt.Errorf("daemon: unknown order kind %d", int(o.Kind))
	}
}
