// Completion closing hooks: self-verification before completed
// (Sections 7.3, 7.5, 8.7).
//
// The step loop owns the finish path; the deterministic rules live in
// internal/tasks (selfverify.go) so they stay testable without a
// model. This file is the seam between them: it stashes the literal
// instruction from the compiled task layer, maps the run history to
// the engine-independent history view, runs the single refine pass
// through the verifier role, and merges the verification outcome
// into the gate report before the honest-state logic decides.
//
// Nothing here changes the step loop prompts or the tool protocol:
// prompts stay with the context compiler and the drivers, and the
// verification phase never executes tools itself — it only checks
// the agent already ran fresh executable checks, and otherwise
// returns the agent to work with a concise list of what is missing.
package engine

import (
	"context"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// selfVerifyConfig resolves the effective switches: the driver
// override wins, otherwise the environment-backed defaults apply so
// every mechanism can be ablated without a rebuild.
func (e *Engine) selfVerifyConfig() tasks.SelfVerifyConfig {
	if e.deps.SelfVerify != nil {
		return *e.deps.SelfVerify
	}
	return tasks.DefaultSelfVerifyConfig()
}

// artifactConfig resolves the effective artifact-gate switches: the
// driver override wins, otherwise the environment-backed defaults.
func (e *Engine) artifactConfig() tasks.ArtifactConfig {
	if e.deps.Artifacts != nil {
		return *e.deps.Artifacts
	}
	return tasks.DefaultArtifactConfig()
}

// stashRequest keeps the literal instruction on the run the first
// time a compiled task layer carries it. Drivers do not need to
// change: the layer already renders request:\n... verbatim.
func (r *Run) stashRequest(taskLayer string) {
	req := tasks.ExtractRequestFromTaskLayer(taskLayer)
	if req == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Request == "" {
		r.Request = req
	}
}

// historyTools maps the append-only history to the verification
// view in order, joining each tool result with its call arguments.
func historyTools(history []ctxcompiler.ActivityEvent) []tasks.HistoryTool {
	argsOf := map[string]string{}
	for _, ev := range history {
		for _, tc := range ev.ToolCalls {
			if _, ok := argsOf[tc.ID]; !ok {
				argsOf[tc.ID] = string(tc.Arguments)
			}
		}
	}
	var out []tasks.HistoryTool
	for _, ev := range history {
		if ev.ToolResult == nil {
			continue
		}
		tr := ev.ToolResult
		out = append(out, tasks.HistoryTool{
			Name:    tr.Name,
			Args:    argsOf[tr.CallID],
			Output:  tr.Output,
			IsError: tr.IsError,
		})
	}
	return out
}

// refineDrafts runs the single model refine pass over the
// deterministic drafts through the verifier role: one cheap call,
// once per run, with fallback to the deterministic drafts on any
// error. It never sees executor reasoning — only the instruction
// and the drafts.
func (e *Engine) refineDrafts(ctx context.Context, run *Run, cfg tasks.SelfVerifyConfig, request string, drafts []tasks.DraftCriterion) []tasks.DraftCriterion {
	if !cfg.EnableRefine || len(drafts) == 0 || strings.TrimSpace(request) == "" {
		return drafts
	}
	run.mu.Lock()
	if run.refined {
		d := run.drafts
		run.mu.Unlock()
		if len(d) > 0 {
			return d
		}
		return drafts
	}
	run.mu.Unlock()
	if e.deps.Models == nil {
		return drafts
	}
	prompt := tasks.RefinePrompt(request, drafts)
	resp, meta, err := e.deps.Models.Chat(ctx, types.RoleVerifier, providers.ChatRequest{
		Messages: []providers.Message{
			{Role: providers.RoleUser, Content: prompt},
		},
		MaxTokens: 512,
	})
	if err != nil {
		markRefined(run, drafts)
		return drafts
	}
	run.mu.Lock()
	run.Tracker.Observe(tasks.Usage{
		InputTokens: int(resp.Usage.Input), OutputTokens: int(resp.Usage.Output),
		CostUSD: meta.CostUSD,
	})
	run.mu.Unlock()
	if refined := tasks.ParseRefinedCriteria(resp.Message.Content, cfg.MaxDrafts); len(refined) > 0 {
		markRefined(run, refined)
		return refined
	}
	markRefined(run, drafts)
	return drafts
}

func markRefined(run *Run, drafts []tasks.DraftCriterion) {
	run.mu.Lock()
	defer run.mu.Unlock()
	run.refined = true
	run.drafts = append([]tasks.DraftCriterion(nil), drafts...)
}

// runStrongVerifier performs the independent verifier pass for one
// finish attempt: a fresh small context (request, drafts, evidence
// lines, change diff, final answer excerpt — never executor
// reasoning) goes to one cheap verifier-role call that writes its
// own small checks and returns per-draft pass/fail with reasons. A
// FAIL returns the agent to work with the reasons; anything else
// (pass, model error, unparseable reply) falls back to the
// deterministic outcome so the verifier can never block on format
// alone. The daemon big-task path owns its Section 8.7 subagent
// spawn from the orchestrator, so this embedded equivalent stands
// down when the gate input already requires that verdict.
func (e *Engine) runStrongVerifier(ctx context.Context, run *Run, cfg tasks.SelfVerifyConfig, drafts []tasks.DraftCriterion, history []tasks.HistoryTool, finalText string) (bool, []string) {
	if !cfg.EnableStrong || !cfg.EnableStrongVerifier {
		return true, nil
	}
	if e.deps.Models == nil {
		return true, nil
	}
	run.mu.Lock()
	verifierOwned := run.GateInput.VerifierRequired
	request := run.Request
	run.mu.Unlock()
	if verifierOwned {
		return true, nil
	}
	// Nothing checkable, nothing to judge: without drafts the
	// deterministic phase already decided, and the verifier would
	// spend a model call with no items to pass or fail.
	if len(drafts) == 0 {
		return true, nil
	}
	small := tasks.BuildVerifierContext(request, drafts, history, 20)
	prompt := tasks.StrongVerifierPrompt(small, history, finalText)
	resp, meta, err := e.deps.Models.Chat(ctx, types.RoleVerifier, providers.ChatRequest{
		Messages: []providers.Message{
			{Role: providers.RoleUser, Content: prompt},
		},
		MaxTokens: 512,
	})
	if err != nil {
		return true, nil
	}
	run.mu.Lock()
	run.Tracker.Observe(tasks.Usage{
		InputTokens: int(resp.Usage.Input), OutputTokens: int(resp.Usage.Output),
		CostUSD: meta.CostUSD,
	})
	run.mu.Unlock()
	verdict := tasks.ParseStrongVerdict(resp.Message.Content, drafts)
	if !verdict.Explicit || verdict.Pass {
		return true, nil
	}
	reasons := append([]string(nil), verdict.Reasons...)
	if len(reasons) > 3 {
		reasons = reasons[:3]
	}
	return false, reasons
}

// selfVerify merges the mandatory verification phase into the gate
// report for one finish attempt. It returns the merged report and
// whether the verification hooks did anything (for journaling).
func (e *Engine) selfVerify(ctx context.Context, run *Run, gate tasks.GateReport, finalText string) tasks.GateReport {
	cfg := e.selfVerifyConfig()
	if !cfg.Enable {
		return gate
	}
	run.mu.Lock()
	request := run.Request
	in := run.GateInput
	steps := run.Steps
	level := in.Level
	history := append([]ctxcompiler.ActivityEvent(nil), run.History...)
	run.mu.Unlock()

	drafts := tasks.ExtractDraftCriteria(request, cfg.MaxDrafts)
	if cfg.EnableExtraction && cfg.EnableRefine {
		drafts = e.refineDrafts(ctx, run, cfg, request, drafts)
	} else {
		run.mu.Lock()
		run.drafts = append([]tasks.DraftCriterion(nil), drafts...)
		run.mu.Unlock()
	}
	_ = level
	merged, kept := tasks.ApplySelfVerify(cfg, gate, in, tasks.SelfVerifyRequest{
		Request:   request,
		Drafts:    drafts,
		History:   historyTools(history),
		FinalText: finalText,
		Steps:     steps,
		Level:     in.Level,
	})
	// The independent verifier speaks last, and only when the
	// deterministic checks would otherwise let the run complete:
	// a FAIL verdict appends the disagreement reasons (bounded by
	// the same gate attempts as every other pending) instead of
	// letting a wrong result finish as completed.
	if merged.OK {
		historyView := historyTools(history)
		if ok, reasons := e.runStrongVerifier(ctx, run, cfg, kept, historyView, finalText); !ok {
			merged.Pendings = append(merged.Pendings,
				"independent verifier disagreement: "+strings.Join(reasons, "; "))
			merged.OK = false
		}
	}
	run.mu.Lock()
	run.drafts = append([]tasks.DraftCriterion(nil), kept...)
	workdir := run.Workdir
	run.mu.Unlock()
	merged = e.applyArtifacts(run, merged, historyTools(history), request, finalText, workdir)
	return merged
}

// applyArtifacts merges the required-artifact gate into the report:
// missing/empty/unexecutable files, unverified ports/URLs, and live
// background sessions. It never executes tools itself; filesystem
// checks read the run workdir and service checks read the agent's
// own successful history. Bounded by the same gate attempts as the
// rest of the finish path (Section 7.3).
func (e *Engine) applyArtifacts(run *Run, gate tasks.GateReport, history []tasks.HistoryTool, request, finalText, workdir string) tasks.GateReport {
	cfg := e.artifactConfig()
	if !cfg.Enable {
		return gate
	}
	pendings := tasks.ApplyArtifactGate(cfg, tasks.ArtifactRequest{
		Request:   request,
		History:   history,
		FinalText: finalText,
		Workdir:   workdir,
		Stat:      e.deps.ArtifactStat,
		Read:      e.deps.ArtifactRead,
	})
	if len(pendings) == 0 {
		return gate
	}
	out := gate
	out.Pendings = append(out.Pendings, pendings...)
	out.OK = false
	return out
}

// verificationNote renders the injected note for a failed finish
// attempt: the gate pendings plus the verification outcome stay on
// one compact line so the context cost is bounded.
func verificationNote(rep tasks.GateReport) string {
	if len(rep.Pendings) == 0 {
		return ""
	}
	return tasks.VerificationNote(rep.Pendings)
}
