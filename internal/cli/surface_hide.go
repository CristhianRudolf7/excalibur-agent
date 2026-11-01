package cli

import (
	"os"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

// Eval-prefix pruning for small models (agent-quality loop-noise
// workstream). Hidden tools stay fully wired: a hallucinated call
// still executes, so nothing errors that worked before. Hiding only
// drops the tool from the model-visible prefix, which is rebuilt
// every step, so the change is live the moment the condition flips
// and the frozen eval profile table (names) never moves.

// hideAckIdle reports whether ack_directive stays out of the model
// prefix: default ON (hide while no directive is pending;
// EXCALIBUR_TOOL_HIDE_ACK=0 restores the always-show legacy).
// Trial records show dozens of ack_directive calls with no directive
// pending: pure overhead the prefix can simply omit.
func hideAckIdle() bool { return !toolSwitchOff("EXCALIBUR_TOOL_HIDE_ACK") }

// hideClockEval reports whether clock stays out of the eval model
// prefix: default ON (trial records show zero productive clock
// calls across the frozen and current builds; wall time and the
// remaining budget already ride every model call in L5);
// EXCALIBUR_TOOL_HIDE_CLOCK=0 restores the legacy prefix.
func hideClockEval() bool { return !toolSwitchOff("EXCALIBUR_TOOL_HIDE_CLOCK") }

// toolSwitchOff reads one default-ON tool switch: unset or any value
// other than 0/false/off/no keeps the mechanism on.
func toolSwitchOff(key string) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "off", "no":
		return true
	}
	return false
}

// pendingDirectives reports whether the embedded run has unacked
// root directives: the only ack_directive workload in single-run
// scope. A nil surface (unwired) counts as none pending.
func pendingDirectives(emb *wireengine.Embedded) bool {
	if emb == nil || emb.Surface == nil || emb.Surface.Orch == nil {
		return false
	}
	return len(emb.Surface.Orch.PendingDirectives(emb.Surface.Root)) > 0
}

// pruneFromPrefix reports whether a core tool stays out of the model
// prefix for this step: ack_directive while idle, clock under the
// eval profile.
func pruneFromPrefix(profile ProfileName, emb *wireengine.Embedded, name string) bool {
	if name == "ack_directive" && hideAckIdle() && !pendingDirectives(emb) {
		return true
	}
	if name == "clock" && profile == ProfileEval && hideClockEval() {
		return true
	}
	return false
}
