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
	run.mu.Lock()
	run.drafts = append([]tasks.DraftCriterion(nil), kept...)
	run.mu.Unlock()
	return merged
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
