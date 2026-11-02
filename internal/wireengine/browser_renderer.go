package wireengine

import (
	"context"
	"github.com/CristhianRudolf7/excalibur-agent/internal/browser"
	"github.com/CristhianRudolf7/excalibur-agent/internal/websearch"
)

type browserRenderer struct {
	Engine    *browser.Engine
	UserAgent string
}

func (r *browserRenderer) Render(ctx context.Context, url string) (string, string, error) {
	return r.Engine.Render(ctx, url, func(ctx context.Context, url string) ([]byte, string, error) {
		return websearch.FetchRenderResource(ctx, url, r.UserAgent)
	})
}
