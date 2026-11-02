package engine

import (
	"context"
	"errors"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

type modelCallSiteKey struct{}

// WithModelCallSite attaches local audit metadata without changing provider wire contracts.
func WithModelCallSite(ctx context.Context, site string) context.Context {
	return context.WithValue(ctx, modelCallSiteKey{}, site)
}

// ModelCallSite returns the initiating engine hook, if present.
func ModelCallSite(ctx context.Context) string {
	site, _ := ctx.Value(modelCallSiteKey{}).(string)
	return site
}

// verifierChat makes one verifier-role call and, when the reply is
// empty, truncated or unparsable, one retry. Verdict recovery
// (EXCALIBUR_VERIFIER_VERDICT) uses a fixed first budget of 8192
// output tokens; the retry doubles it (EXCALIBUR_VERDICT_RETRY) and
// shrinks the prompt. A reply still without a verdict returns
// tasks.ErrVerifierNoVerdict and is journaled as "no verdict": the
// finish gate treats that as advisory, never as a pass. Usage
// returned on failures is accounted as well; adapters may omit it
// on provider errors.
func (e *Engine) verifierChat(ctx context.Context, run *Run, site, prompt, smaller string, legacyBudget int, valid func(string) bool) (providers.ChatResponse, models.CallMeta, error) {
	if run.deliveryPendingReason() != "" {
		return providers.ChatResponse{}, models.CallMeta{}, tasks.ErrVerifierNoVerdict
	}
	reviewCtx, cancel := e.modelDeliveryContext(ctx, run, false)
	defer cancel()
	enabled := tasks.VerifierVerdictEnabled()
	budget := legacyBudget
	if enabled {
		budget = 8192
	}
	var resp providers.ChatResponse
	var meta models.CallMeta
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		callSite := site
		if attempt > 0 {
			callSite += ".retry"
			prompt = smaller
			budget = tasks.VerdictRetryBudget(budget)
		}
		resp, meta, err = e.deps.Models.Chat(WithModelCallSite(reviewCtx, callSite), types.RoleVerifier, providers.ChatRequest{Messages: []providers.Message{{Role: providers.RoleUser, Content: prompt}}, MaxTokens: budget})
		run.mu.Lock()
		run.observeUsageLocked(tasks.Usage{InputTokens: int(resp.Usage.Input), OutputTokens: int(resp.Usage.Output), CostUSD: meta.CostUSD})
		run.mu.Unlock()
		if reviewCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			e.markDeliveryPending(ctx, run, site)
			return resp, meta, tasks.ErrVerifierNoVerdict
		}
		if !enabled {
			return resp, meta, err
		}
		if err == nil && valid(resp.Message.Content) {
			return resp, meta, nil
		}
		var providerErr *providers.ProviderError
		truncated := errors.As(err, &providerErr) && providerErr.Kind == providers.KindTruncated
		if err != nil && !truncated {
			return resp, meta, err
		}
		if ctx.Err() != nil {
			return resp, meta, ctx.Err()
		}
	}
	e.recordNoVerdict(ctx, run, site, budget, resp, err)
	return resp, meta, tasks.ErrVerifierNoVerdict
}

// recordNoVerdict journals a verifier call that ended without a
// verdict, so audits can tell "no verdict" apart from a pass or a
// refusal. It records shape only (site, budget, output tokens, empty
// or truncated), never the reply text.
func (e *Engine) recordNoVerdict(ctx context.Context, run *Run, site string, budget int, resp providers.ChatResponse, err error) {
	var providerErr *providers.ProviderError
	truncated := errors.As(err, &providerErr) && providerErr.Kind == providers.KindTruncated
	if rec := e.recorder(run.ID); rec != nil {
		_, _ = rec.Record(ctx, journal.EntryType("verifier_no_verdict"), journal.MarshalPayload(struct {
			Site         string `json:"site"`
			Budget       int    `json:"last_max_tokens"`
			OutputTokens int64  `json:"last_output_tokens"`
			Empty        bool   `json:"empty"`
			Truncated    bool   `json:"truncated"`
		}{site, budget, resp.Usage.Output, strings.TrimSpace(resp.Message.Content) == "", truncated}), "", "", "")
	}
}
