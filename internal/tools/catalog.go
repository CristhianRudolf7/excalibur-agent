package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// StubTool is a catalog placeholder for core tools owned by other
// subsystems (Section 13.2): the definition lives here so the catalog
// is complete and name-unique, while Invoke delegates to a Provider
// wired by the owning milestone. Until wired it reports unimplemented.
type StubTool struct {
	Def      Definition
	Provider func(ctx context.Context, inv Invocation) (Result, error)
}

func (t *StubTool) Definition() Definition { return t.Def }

func (t *StubTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	if t.Provider != nil {
		return t.Provider(ctx, inv)
	}
	if !StubRepliesEnabled() {
		return Result{Status: StatusToolError, Output: fmt.Sprintf("tool %q has no backend in this run", t.Def.Name)}, nil
	}
	return Result{Status: StatusOK, Output: stubReply(t.Def.Name)}, nil
}

// stubReply answers a listed but unwired tool without an error. It says
// what happened (nothing) and what to do instead, so the agent does not
// read an unwired tool as a failed step or as a completed one.
func stubReply(name string) string {
	switch name {
	case "ack_directive":
		return "no directives are pending in this run; nothing to acknowledge"
	case "leave_continuation_note":
		return "continuation notes are not kept in this run; nothing was saved"
	}
	return fmt.Sprintf("%s has no backend in this run, so nothing was done; continue with the file and exec tools", name)
}

func (t *StubTool) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }

func stubDef(name, desc, group string, effect types.EffectClass, idem types.IdempotencyClass, cancel types.CancelMode, fast bool, tags, keywords []string) Definition {
	return Definition{
		Name: name, Description: desc,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Effect:      effect, Idempotency: idem, Cancel: cancel,
		Timeout: 60 * time.Second, OutputBudget: DefaultOutputBudget, Fast: fast,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: group, Tags: tags, Keywords: keywords,
	}
}

// CoreStubDefinitions returns definitions for every Section 13.2 core
// tool not implemented in H3. Each names the interface its owner must
// provide; H3 implements the file/terminal/control subset directly.
func CoreStubDefinitions() []Definition {
	return []Definition{
		stubDef("send_message", "Send a message to the user with text and attachments. Master only", "conversation", types.EffectCommunication, types.IdemKeyed, types.CancelImmediate, true, []string{"message"}, []string{"send", "message", "user"}),
		stubDef("ask_user", "Ask for a clarification or decision with options and default", "conversation", types.EffectCommunication, types.IdemKeyed, types.CancelImmediate, true, []string{"question"}, []string{"ask", "question", "clarify"}),
		stubDef("leave_continuation_note", "Save a brief next-step note for the next call", "conversation", types.EffectWriteLocal, types.IdemIdempotent, types.CancelAfterCompletion, false, []string{"note"}, []string{"note", "continuation", "context"}),
		stubDef("ack_directive", "Acknowledge a directive and explain how it applies. Subagents only", "conversation", types.EffectWriteLocal, types.IdemIdempotent, types.CancelImmediate, false, []string{"directive"}, []string{"ack", "directive"}),
		stubDef("settings", "Read and change agent settings from the conversation. Master only", "control", types.EffectWriteLocal, types.IdemIdempotent, types.CancelAfterCompletion, true, []string{"config"}, []string{"settings", "config"}),
		stubDef("web_search", "Search the internet with the own search API; registers the search", "web", types.EffectReadOnly, types.IdemPure, types.CancelImmediate, true, []string{"web", "search"}, []string{"web", "search", "internet"}),
		stubDef("web_read", "Read a page by URL with light HTTP reading as Markdown", "web", types.EffectReadOnly, types.IdemIdempotent, types.CancelImmediate, false, []string{"web", "read"}, []string{"web", "read", "page", "url"}),
		stubDef("task_db", "Grouped transactional operations on projects, tasks, plans and evidence", "work", types.EffectWriteLocal, types.IdemKeyed, types.CancelAfterCompletion, true, []string{"task"}, []string{"task", "plan", "project", "evidence"}),
		stubDef("memory_search", "Retrieve memories by lexical, semantic, temporal and relational search", "memory", types.EffectReadOnly, types.IdemPure, types.CancelImmediate, true, []string{"memory"}, []string{"memory", "search", "recall"}),
		stubDef("memory_write", "Write to memory with admission: add, supersede, retire, disputes, merges", "memory", types.EffectWriteLocal, types.IdemKeyed, types.CancelAfterCompletion, true, []string{"memory"}, []string{"memory", "write", "remember"}),
		stubDef("memory_propose", "Propose a candidate fact; stays pending for consolidation", "memory", types.EffectWriteLocal, types.IdemIdempotent, types.CancelAfterCompletion, true, []string{"memory"}, []string{"memory", "propose", "fact"}),
		stubDef("history_search", "Search past messages and actions by text, date, project, tool or state", "memory", types.EffectReadOnly, types.IdemPure, types.CancelImmediate, true, []string{"history"}, []string{"history", "search", "actions"}),
		stubDef("library_search", "Search the user document library and return citable fragments", "memory", types.EffectReadOnly, types.IdemPure, types.CancelImmediate, true, []string{"library"}, []string{"library", "search", "documents"}),
		stubDef("doc_read", "Read the agent profile, user profile or notebook by index or section", "memory", types.EffectReadOnly, types.IdemPure, types.CancelImmediate, true, []string{"doc"}, []string{"doc", "read", "profile", "notebook"}),
		stubDef("doc_edit", "Edit a fragment of those documents checking its hash first", "memory", types.EffectWriteLocal, types.IdemIdempotent, types.CancelAfterCompletion, true, []string{"doc"}, []string{"doc", "edit", "profile"}),
		stubDef("pin_observation", "Mark an observation important so masking never hides it", "context", types.EffectWriteLocal, types.IdemIdempotent, types.CancelImmediate, false, []string{"context"}, []string{"pin", "observation", "mask"}),
		stubDef("search_skills", "Search skills by text and group; returns name, group and path", "skills", types.EffectReadOnly, types.IdemPure, types.CancelImmediate, true, []string{"skills"}, []string{"search", "skills"}),
		stubDef("load_skill", "Load a SKILL.md body and resolve its deferred tools", "skills", types.EffectReadOnly, types.IdemIdempotent, types.CancelImmediate, true, []string{"skills"}, []string{"load", "skill"}),
		stubDef("search_tools", "Search deferred tools and return the 5 most relevant schemas", "skills", types.EffectReadOnly, types.IdemPure, types.CancelImmediate, true, []string{"tools"}, []string{"search", "tools", "deferred"}),
		stubDef("call_tool", "Invoke a registered tool by name with validated arguments, e.g. {\"name\":\"lsp\",\"arguments\":{\"path\":\"x\"}}; the name may also sit inside arguments", "skills", types.EffectExec, types.IdemKeyed, types.CancelCooperative, false, []string{"tools"}, []string{"call", "tool", "deferred"}),
		stubDef("subagent", "Manage subagents: spawn, status, message, redirect, pause, resume, interrupt, cancel", "delegation", types.EffectExec, types.IdemKeyed, types.CancelCooperative, true, []string{"subagent"}, []string{"subagent", "delegate", "spawn"}),
		stubDef("reminders", "Create, list, snooze, complete and delete reminders", "agenda", types.EffectWriteLocal, types.IdemKeyed, types.CancelAfterCompletion, true, []string{"agenda"}, []string{"reminder", "schedule", "agenda"}),
	}
}

// BuiltinDeps bundles the collaborators for the H3 built-ins.
type BuiltinDeps struct {
	Files    *FileDeps
	Sessions *SessionManager
	Outputs  OutputStore
	ClockNow func() time.Time
	Status   SnapshotProvider
}

// DefaultBuiltinDeps returns test-ready deps with in-memory stores.
func DefaultBuiltinDeps() *BuiltinDeps {
	hist := NewMemoryHistory()
	outs := NewMemoryOutputStore()
	return &BuiltinDeps{
		Files:    &FileDeps{Fingerprints: NewFingerprintStore(), Leases: NewMemoryLeases(), History: hist},
		Sessions: NewSessionManager(TerminalDeps{}),
		Outputs:  outs,
	}
}

// BuiltinTools returns the H3 tool implementations with their deps.
func BuiltinTools(deps *BuiltinDeps) []Tool {
	if deps == nil {
		deps = DefaultBuiltinDeps()
	}
	files := deps.Files
	if files == nil {
		files = &FileDeps{Fingerprints: NewFingerprintStore(), Leases: NewMemoryLeases(), History: NewMemoryHistory()}
	}
	sess := deps.Sessions
	if sess == nil {
		sess = NewSessionManager(TerminalDeps{})
	}
	outs := deps.Outputs
	if outs == nil {
		outs = NewMemoryOutputStore()
	}
	tools := []Tool{
		&ReadFileTool{Deps: files},
		&WriteFileTool{Deps: files},
		&EditFileTool{Deps: files},
		&ApplyPatchTool{Deps: files},
		&GrepTool{},
		&GlobTool{},
		&ExecTool{Sessions: sess, Outputs: outs, Workdir: files.runWorkdir()},
		&ExecIOTool{Sessions: sess},
		&ClockTool{Now: deps.ClockNow},
		&GetToolOutputTool{Outputs: outs, History: files.History, Sessions: sess},
		&StatusTool{Provider: deps.Status, Sessions: sess},
		&FinishTool{},
	}
	if InspectImageEnabled() {
		tools = append(tools, &InspectImageTool{Deps: files})
	}
	for _, s := range CoreStubDefinitions() {
		def := s
		tools = append(tools, &StubTool{Def: def})
	}
	return tools
}

// RegisterBuiltins registers every H3 built-in plus the core stubs.
// All tools register through the same Registry interface as
// extensions: there is no privileged path (Section 14.12).
func RegisterBuiltins(reg Registry, deps *BuiltinDeps) error {
	for _, t := range BuiltinTools(deps) {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}

// BuiltinManifest returns the excalibur-extension.yaml content for the
// built-in extension (Section 14.12, kind builtin).
func BuiltinManifest(deps *BuiltinDeps) Manifest {
	tools := BuiltinTools(deps)
	ext := make([]ExtensionTool, 0, len(tools))
	for _, t := range tools {
		ext = append(ext, ExtensionTool{Definition: t.Definition()})
	}
	sort.Slice(ext, func(i, j int) bool { return ext[i].Name < ext[j].Name })
	return Manifest{
		ID: "excalibur-builtin", Version: "0.1.0", Contract: ExtensionContract,
		Kind: ExtensionBuiltin, Lifecycle: LifecycleResident,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Tools: ext,
	}
}
