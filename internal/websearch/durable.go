package websearch

import (
	"context"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// durableBrave refuses before credentials or transport are touched. The model
// harness persists observations; use requires an explicit storage-rights opt-in.
type durableBrave struct{}

func (durableBrave) Name() string { return "brave" }
func (durableBrave) Search(ctx context.Context, _ string, _ tools.SearchOptions) ([]tools.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, &ProviderError{Provider: "brave", MissingKey: true, DurableRefused: true}
}
