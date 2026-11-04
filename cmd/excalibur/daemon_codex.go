package main

import (
	"context"

	"github.com/CristhianRudolf7/excalibur-agent/internal/cli"
	"github.com/CristhianRudolf7/excalibur-agent/internal/codexlink"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/openai"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/registry"
)

func buildDaemonProvider(ctx context.Context, settings models.ModelSettings, relay *codexlink.Server, resolve registry.SecretResolver) (providers.Provider, error) {
	if relay == nil {
		return cli.BuildProvider(ctx, settings, resolve)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Bind the ephemeral key directly so unrelated deployment keys cannot override it.
	return openai.New(openai.Options{APIKey: relay.Key(), BaseURL: relay.BaseURL(), UseResponses: true})
}
