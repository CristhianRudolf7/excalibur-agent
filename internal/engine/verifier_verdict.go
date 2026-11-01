package engine

import (
	"context"
	"errors"

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

var errVerifierNoVerdict = errors.New("verifier produced no verdict; unverified: retry verification with executable behavioural evidence")

// verifierChat bounds recovery to one smaller-context retry. Usage returned on
// failures is accounted as well; adapters may omit it on provider errors.
func (e *Engine) verifierChat(ctx context.Context, run *Run, site, prompt, smaller string, legacyBudget int, valid func(string) bool) (providers.ChatResponse, models.CallMeta, error) {
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
		}
		resp, meta, err = e.deps.Models.Chat(WithModelCallSite(ctx, callSite), types.RoleVerifier, providers.ChatRequest{Messages: []providers.Message{{Role: providers.RoleUser, Content: prompt}}, MaxTokens: budget})
		run.mu.Lock()
		run.Tracker.Observe(tasks.Usage{InputTokens: int(resp.Usage.Input), OutputTokens: int(resp.Usage.Output), CostUSD: meta.CostUSD})
		run.mu.Unlock()
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
	return resp, meta, errVerifierNoVerdict
}
