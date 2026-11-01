// Required-artifact gate through the engine finish path (Sections
// 7.3, 7.5): missing/empty/unexecutable files, unverified ports and
// live background sessions block completed and return the agent to
// work with a concise checklist; the switch restores legacy passes.
package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func artifactRun(dir string, request string, history []ctxcompiler.ActivityEvent) *Run {
	run := testRun(Profile{})
	run.Workdir = dir
	run.Request = request
	run.History = history
	return run
}

func TestArtifactGateBlocksMissingFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	missing := filepath.Join(dir, "run.py")
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done"), finalResp("done")}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = verifyConfig()
	run := artifactRun(dir, "Put the function in a file called "+missing+".", writeHistory(missing))
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompletedPending)
	if model.calls() != 2 {
		t.Fatalf("bounded returns need 2 model calls, got %d", model.calls())
	}
	if got := tasks.CompactNote(run.LastGate); !strings.Contains(got, "required artifact missing") {
		t.Fatalf("pendings should name the missing artifact: %q", got)
	}
	if rep := e.Report(run); len(rep.Pendings) == 0 {
		t.Fatal("honest state needs pendings")
	}
}

func TestArtifactAndStrongGatesCombinePendings(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	missing := filepath.Join(dir, "run.py")
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done"), finalResp("done")}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	// Both gates on (defaults): the self-verify config keeps the
	// strong exercise layer, the artifact gate keeps the filesystem
	// layer. A missing file plus a shallow observation must name
	// both gaps, never collapse to completed.
	e.deps.SelfVerify = verifyConfig()
	run := artifactRun(dir, "Put the function in a file called "+missing+".", shallowHistory(missing))
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompletedPending)
	got := tasks.CompactNote(run.LastGate)
	if !strings.Contains(got, "required artifact missing") {
		t.Fatalf("pendings should name the missing artifact: %q", got)
	}
	if !strings.Contains(strings.ToLower(got), "exercis") {
		t.Fatalf("pendings should demand exercising the deliverable: %q", got)
	}
}

func TestArtifactGatePassesWhenFileExists(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "run.py")
	if err := os.WriteFile(path, []byte("def run_tasks(): pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("created " + path + " and verified the import"), verifierPassResp()}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = verifyConfig()
	history := writeHistory(path)
	history = append(history,
		ctxcompiler.ActivityEvent{Seq: 2, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c2", "exec", `{"command":"python3 -c 'import run'"}`)}},
		ctxcompiler.ActivityEvent{Seq: 3, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c2", Name: "exec", Output: path + " ok"}},
	)
	run := artifactRun(dir, "Put the function in a file called "+path+".", history)
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
}

func TestArtifactGateOffPassesMissing(t *testing.T) {
	// Disable the independent filesystem scope guard for this legacy ablation.
	t.Setenv("EXCALIBUR_SCOPE_GUARD", "off")
	ctx := context.Background()
	dir := t.TempDir()
	missing := filepath.Join(dir, "never.py")
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("created " + missing)}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	cfg := verifyConfig()
	cfg.EnableStrong = false
	e.deps.SelfVerify = cfg
	e.deps.Artifacts = artifactOff()
	// Verified history (write plus a passing check that names the
	// path) so the self-verification layer passes; only the
	// artifact filesystem layer is switched off here. The strong
	// exercise layer stays off too: `test -s` observes without
	// exercising, and strong behavior lives in strongverify_test.go.
	history := writeHistory(missing)
	history = append(history,
		ctxcompiler.ActivityEvent{Seq: 2, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c2", "exec", `{"command":"test -s `+missing+`"}`)}},
		ctxcompiler.ActivityEvent{Seq: 3, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c2", Name: "exec", Output: missing + " ok"}},
	)
	run := artifactRun(dir, "Create the file "+missing+".", history)
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
}

func TestArtifactGateBlocksUnexecutable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	bin := filepath.Join(dir, "povray")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done"), finalResp("done")}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = verifyConfig()
	run := artifactRun(dir, "Compile and install to "+bin+".",
		[]ctxcompiler.ActivityEvent{
			{Seq: 0, Role: providers.RoleAssistant, Content: "",
				ToolCalls: []providers.ToolCall{toolCall("c1", "exec", `{"command":"make install"}`)}},
			{Seq: 1, Role: providers.RoleTool,
				ToolResult: &providers.ToolResult{CallID: "c1", Name: "exec", Output: "installed " + bin}},
		})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompletedPending)
	if got := tasks.CompactNote(run.LastGate); !strings.Contains(got, "not executable") {
		t.Fatalf("pendings should name the unexecutable install: %q", got)
	}
}

func TestArtifactGatePassesExecutable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	bin := filepath.Join(dir, "povray")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("installed " + bin + " and verified it runs")}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	cfg := verifyConfig()
	cfg.EnableStrong = false
	e.deps.SelfVerify = cfg
	run := artifactRun(dir, "Compile and install to "+bin+".",
		[]ctxcompiler.ActivityEvent{
			{Seq: 0, Role: providers.RoleAssistant, Content: "",
				ToolCalls: []providers.ToolCall{toolCall("c1", "exec", `{"command":"make install"}`)}},
			{Seq: 1, Role: providers.RoleTool,
				ToolResult: &providers.ToolResult{CallID: "c1", Name: "exec", Output: "installed " + bin}},
			{Seq: 2, Role: providers.RoleAssistant, Content: "",
				ToolCalls: []providers.ToolCall{toolCall("c2", "exec", `{"command":"`+bin+` --help"}`)}},
			{Seq: 3, Role: providers.RoleTool,
				ToolResult: &providers.ToolResult{CallID: "c2", Name: "exec", Output: "usage: povray"}},
		})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
}

func TestArtifactGateBlocksEarlierLiveSession(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done"), finalResp("done")}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = verifyConfig()
	// This test isolates background-session artifact checks.
	e.deps.SelfVerify.EnableStrongVerifier = false
	run := artifactRun("", "Build the project with make.", []ctxcompiler.ActivityEvent{
		{Seq: 0, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c1", "exec", `{"command":"make -j"}`)}},
		{Seq: 1, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c1", Name: "exec", Output: "session: sess-1  alive: true\nbuilding..."}},
		{Seq: 2, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c2", "exec", `{"command":"ps aux | grep make"}`)}},
		{Seq: 3, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c2", Name: "exec", Output: "session: sess-2  alive: false\nroot 123 make"}},
	})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	// The last result settled, so freshness passes; the earlier
	// session is still alive and unverified, which the artifact
	// background check must catch.
	mustState(t, run, types.RunCompletedPending)
	if got := tasks.CompactNote(run.LastGate); !strings.Contains(got, "still running") {
		t.Fatalf("pendings should name the live session: %q", got)
	}
}

func TestArtifactGatePassesClosedSessions(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done")}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = verifyConfig()
	run := artifactRun("", "Build the project with make.", []ctxcompiler.ActivityEvent{
		{Seq: 0, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c1", "exec", `{"command":"make -j"}`)}},
		{Seq: 1, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c1", Name: "exec", Output: "session: sess-1  alive: true\nbuilding..."}},
		{Seq: 2, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c2", "exec_io", `{"action":"close","session":"sess-1"}`)}},
		{Seq: 3, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c2", Name: "exec_io", Output: "closed sess-1"}},
	})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompleted)
}

func TestArtifactGatePortUnverifiedBlocks(t *testing.T) {
	ctx := context.Background()
	model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp("done"), finalResp("done")}}
	e, js := testEngine(model, scriptedRunner(), passingGate())
	e.deps.SelfVerify = verifyConfig()
	run := artifactRun("", "Set up a server on port 8080.", []ctxcompiler.ActivityEvent{
		{Seq: 0, Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{toolCall("c1", "exec", `{"command":"ls"}`)}},
		{Seq: 1, Role: providers.RoleTool,
			ToolResult: &providers.ToolResult{CallID: "c1", Name: "exec", Output: "session: sess-1  alive: false\nok"}},
	})
	startRun(t, e, run, js)
	if err := e.Run(ctx, run); err != nil {
		t.Fatal(err)
	}
	mustState(t, run, types.RunCompletedPending)
	if got := tasks.CompactNote(run.LastGate); !strings.Contains(got, "port 8080") {
		t.Fatalf("pendings should name the unverified port: %q", got)
	}
}
