package cli

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

func idleEmbedded() *wireengine.Embedded {
	return &wireengine.Embedded{Surface: wireengine.EnsureSurfaceStores("run_hide_test")}
}

func TestPruneAckDirectiveIdle(t *testing.T) {
	emb := idleEmbedded()
	if !pruneFromPrefix(ProfileEval, emb, "ack_directive") {
		t.Fatal("idle ack_directive must be pruned from the eval prefix")
	}
	if !pruneFromPrefix(ProfileDelegate, emb, "ack_directive") {
		t.Fatal("idle ack_directive must be pruned from the delegate prefix")
	}
	if pruneFromPrefix(ProfileEval, nil, "ack_directive") != true {
		t.Fatal("nil surface counts as no pending directives")
	}
	// A pending root directive brings the tool back.
	if _, err := emb.Surface.Orch.SendDirective(emb.Surface.Root, emb.Surface.Root,
		orchestrator.ActionAdjust, "steer", "", orchestrator.PriorityNormal); err != nil {
		t.Fatal(err)
	}
	if pruneFromPrefix(ProfileEval, emb, "ack_directive") {
		t.Fatal("pending directive must restore ack_directive to the prefix")
	}
	// Switch off restores the always-show legacy prefix.
	t.Setenv("EXCALIBUR_TOOL_HIDE_ACK", "0")
	if pruneFromPrefix(ProfileEval, idleEmbedded(), "ack_directive") {
		t.Fatal("EXCALIBUR_TOOL_HIDE_ACK=0 must always show ack_directive")
	}
}

func TestPruneClockEval(t *testing.T) {
	emb := idleEmbedded()
	if !pruneFromPrefix(ProfileEval, emb, "clock") {
		t.Fatal("clock must be pruned from the eval prefix")
	}
	if !pruneFromPrefix(ProfileDelegate, emb, "clock") {
		t.Fatal("clock must be pruned from the delegate prefix")
	}
	if pruneFromPrefix(ProfileDefault, emb, "clock") {
		t.Fatal("assistant clock must stay visible")
	}
	if pruneFromPrefix(ProfileEval, emb, "exec") {
		t.Fatal("working tools must never be pruned")
	}
	t.Setenv("EXCALIBUR_TOOL_HIDE_CLOCK", "off")
	if pruneFromPrefix(ProfileEval, emb, "clock") {
		t.Fatal("EXCALIBUR_TOOL_HIDE_CLOCK=off must restore clock")
	}
}
