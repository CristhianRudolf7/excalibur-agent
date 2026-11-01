package engine

import (
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestLiteralInstructionExamples(t *testing.T) {
	for _, tc := range []struct {
		name, instruction, want string
	}{
		{"function", "Implement f. f(3) = 9.", "f(3) → 9"},
		{"decimal", "For the function, f(1.5) = 2.25.", "f(1.5) → 2.25"},
		{"prose", "For example abc should print XYZ.", "abc → XYZ"},
		{"arrow", "Example: a b -> c d", "a b → c d"},
		{"fenced pair", "Input:\n```\n3 4\n```\nExpected output:\n```\n7\n```", "3 4 → 7"},
		{"fenced block", "Example:\n```\nInput:\n3 4\nExpected output:\n7\n```", "3 4 → 7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := instructionExamples(tc.instruction)
			if len(got) != 1 || !strings.Contains(got[0], tc.want) ||
				!strings.Contains(got[0], "test at least one input not in the examples") {
				t.Fatalf("literal pair missing %q: %v", tc.want, got)
			}
		})
	}
	if got := instructionExamples("Write a program. Test it thoroughly.\n```\nprint(3)\n```"); len(got) != 0 {
		t.Fatalf("invented examples: %v", got)
	}
}

func TestLiteralExamplesBoundAndDeduplicate(t *testing.T) {
	request := "f(1) = 1\nf(1) = 1\nf(2) = 4\nf(3) = 9\nf(4) = 16\nf(5) = 25\nf(6) = 36\n"
	got := instructionExamples(request)
	if len(got) != 5 || !strings.Contains(got[0], "f(1) → 1") || !strings.Contains(got[4], "f(5) → 25") {
		t.Fatalf("bound, order or deduplication: %v", got)
	}
}

func TestInstructionExamplesAreAdvisory(t *testing.T) {
	t.Setenv("EXCALIBUR_CHECKLIST_EXAMPLES", "on")
	model := &fakeEndpoint{responses: []providers.ChatResponse{
		toolResp(toolCall("f1", "finish", finishArgs("done", "checked"))),
		toolResp(toolCall("f2", "finish", finishArgs("done", "checked"))),
	}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = selfVerifyOffCfg()
	e.deps.Artifacts = artifactOffCfg()
	run := explicitRun()
	run.Request = "Implement the function. f(3) = 9."
	if got := e.confirmationText(run); !strings.Contains(got, "the instruction gives the example f(3) → 9:") {
		t.Fatalf("missing checklist example: %s", got)
	}
	t.Setenv("EXCALIBUR_CHECKLIST_EXAMPLES", "off")
	if got := e.confirmationText(run); strings.Contains(got, "the instruction gives the example") {
		t.Fatalf("switch off still extracts: %s", got)
	}
	t.Setenv("EXCALIBUR_CHECKLIST_EXAMPLES", "on")
	startRun(t, e, run, js)
	if err := e.Run(finishTestContext(t), run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
	if run.FinishRefusals != 0 || run.FinishCalls != 2 {
		t.Fatalf("example checklist: %d refusals, %d calls", run.FinishRefusals, run.FinishCalls)
	}
}
