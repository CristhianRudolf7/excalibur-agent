package engine

import (
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

func TestFinishErrorUsesFailingEntryAfterSuccessfulProbes(t *testing.T) {
	t.Setenv("EXCALIBUR_FINISH_ERROR_ENTRY", "on")
	failed := "exit: 9  duration: 0.0s\nfailed validation\noriginal failure detail"
	run := &Run{drafts: []tasks.DraftCriterion{{ID: "D1", Key: "validation", Check: "cat unrelated.json"}}}
	for _, v := range []struct {
		id, command, out string
		failed           bool
	}{
		{"bad", "validate-data", failed, true},
		{"probe", "cat unchanged.json", "exit: 0\nprobe output", false},
		{"probe2", "test -s unchanged.json", "exit: 0\n", false},
	} {
		run.History = append(run.History, ctxcompiler.ActivityEvent{ToolCalls: []providers.ToolCall{{ID: v.id, Name: "exec", Arguments: []byte(`{"command":"` + v.command + `"}`)}}}, ctxcompiler.ActivityEvent{ToolResult: &providers.ToolResult{CallID: v.id, Name: "exec", Output: v.out, IsError: v.failed}})
	}
	e := &Engine{}
	reason := lastErrorReason(failed, "validate-data")
	items := e.refusalItems(run, []string{reason})
	if len(items) != 1 || items[0].Check != "validate-data" || !strings.Contains(items[0].Missing, "exit 9") || !strings.Contains(items[0].Missing, "original failure detail") || strings.Contains(items[0].Missing, "probe output") {
		t.Fatalf("mixed entries: %+v", items)
	}
	if _, isErr, _ := lastNonFinishError(run); isErr {
		t.Fatal("successful probes remain successful; stale errors must not become the last result")
	}
}

func TestFinishErrorExcerptExcludesHarnessSuggestions(t *testing.T) {
	t.Setenv("EXCALIBUR_FINISH_ERROR_ENTRY", "on")
	reason := lastErrorReason("exit: 2\nprogram: missing input\nhint: no such file or directory. Check the path; nearest existing parent: .\n unrelated file listing\n[time: 1m used]", "validate-input")
	if !strings.Contains(reason, "program: missing input") || strings.Contains(reason, "unrelated file listing") || strings.Contains(reason, "[time:") {
		t.Fatal(reason)
	}
}
