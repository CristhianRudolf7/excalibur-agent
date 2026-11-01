package engine

import (
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestHistoryRewriteInFinishChecklist(t *testing.T) {
	run := NewRun("history-warning", "task", Profile{})
	run.Request = "Edit repository content"
	run.History = []ctxcompiler.ActivityEvent{
		{ToolCalls: []providers.ToolCall{{ID: "git", Name: "exec", Arguments: []byte(`{"command":"git commit --amend"}`)}}},
		{ToolResult: &providers.ToolResult{CallID: "git", Name: "exec", Output: "exit: 0"}},
	}
	e := &Engine{}
	if text := e.confirmationText(run); !strings.Contains(text, tools.GitHistoryWarning) {
		t.Fatal(text)
	}
	run.Request = "Rewrite commits"
	if text := e.confirmationText(run); strings.Contains(text, tools.GitHistoryWarning) {
		t.Fatal(text)
	}
}
