package ctxcompiler_test

import (
	"strings"
	"testing"

	ctxcompiler "github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// 11.8 row 5: durable instructions render byte-identically across
// compaction, restart and delegation.
func TestInstructionsSurviveEverything(t *testing.T) {
	reg := ctxcompiler.NewRegistry()
	general := types.GeneralScope()
	project := types.Scope{Kind: types.ScopeProject, ID: "prj_demo"}
	task := types.Scope{Kind: types.ScopeTask, ID: "tsk_7"}
	a := reg.Add(general, "Always use English.", "triage")
	b := reg.Add(project, "Never touch vendor/.", "triage")
	c := reg.Add(task, "Use TDD for the fix.", "triage")

	before := reg.Text(general, project, task)

	// Compaction re-renders from the registry: simulate by rendering
	// the snapshot compiler input before and after a compact pass.
	snap := testSnapshot()
	snap.Instructions = ctxcompiler.Views(reg.Active())
	cfg := ctxcompiler.DefaultConfig()
	first, err := ctxcompiler.Compile(cfg, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	// Restart: snapshot the registry to bytes and restore.
	raw, err := reg.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	reg2 := ctxcompiler.NewRegistry()
	if err := reg2.Restore(raw); err != nil {
		t.Fatal(err)
	}
	after := reg2.Text(general, project, task)
	if before != after {
		t.Fatalf("instructions changed across restart:\n%q\n%q", before, after)
	}
	snap2 := testSnapshot()
	snap2.Instructions = ctxcompiler.Views(reg2.Active())
	second, err := ctxcompiler.Compile(cfg, snap2, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	if first.LayerHash(ctxcompiler.LayerScope) != second.LayerHash(ctxcompiler.LayerScope) {
		t.Fatal("L2 instructions changed across restart")
	}
	// Delegation: the card carries the same literals for the
	// delegated scope.
	card := reg2.DelegateCard(task)
	if !strings.Contains(card, c.Text) {
		t.Fatalf("delegation card misses task instruction %q", c.Text)
	}
	for _, other := range []string{a.Text, b.Text} {
		if strings.Contains(card, other) {
			t.Fatalf("task card must not carry other scopes: %q", other)
		}
	}
	if !strings.Contains(card, "Mandatory instructions") {
		t.Fatal("delegation card must be explicit, never a skill suggestion")
	}
}

// Withdrawing and revising keep the literal history consistent.
func TestInstructionLifecycle(t *testing.T) {
	reg := ctxcompiler.NewRegistry()
	scope := types.GeneralScope()
	in := reg.Add(scope, "Draft text.", "triage")
	if !reg.Revise(in.ID, "Final text.") {
		t.Fatal("revise must find the instruction")
	}
	got, ok := reg.Get(in.ID)
	if !ok || got.Text != "Final text." || got.Revision != 2 || got.State != ctxcompiler.InstructionActive {
		t.Fatalf("bad revision: %+v", got)
	}
	if !reg.Withdraw(in.ID) {
		t.Fatal("withdraw must find the instruction")
	}
	if text := reg.Text(scope); strings.Contains(text, "Final text.") {
		t.Fatal("withdrawn instructions must not render")
	}
	if reg.Withdraw("ins_9999") || reg.Revise("ins_9999", "x") {
		t.Fatal("unknown IDs must report false")
	}
}

// Instructions land in the right layers: general/project in L2,
// task/conversation in L3.
func TestInstructionLayering(t *testing.T) {
	if ctxcompiler.ScopeLayer(types.ScopeTask) != ctxcompiler.LayerTask {
		t.Fatal("task instructions render in L3")
	}
	if ctxcompiler.ScopeLayer(types.ScopeConversation) != ctxcompiler.LayerTask {
		t.Fatal("conversation instructions render in L3")
	}
	if ctxcompiler.ScopeLayer(types.ScopeProject) != ctxcompiler.LayerScope {
		t.Fatal("project instructions render in L2")
	}
	if ctxcompiler.ScopeLayer(types.ScopeGeneral) != ctxcompiler.LayerScope {
		t.Fatal("general instructions render in L2")
	}
	snap := testSnapshot()
	snap.Instructions = []ctxcompiler.InstructionView{
		{Scope: "task:tsk_7", ID: "ins_0001", Text: "Use TDD."},
	}
	cfg := ctxcompiler.DefaultConfig()
	c, err := ctxcompiler.Compile(cfg, snap, fixedCounter{per: 4})
	if err != nil {
		t.Fatal(err)
	}
	l2 := c.LayerText(ctxcompiler.LayerScope)
	_ = l2
}
