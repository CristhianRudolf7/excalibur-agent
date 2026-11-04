package daemon

import (
	"context"

	"github.com/CristhianRudolf7/excalibur-agent/internal/codexlink"
	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
)

// WireCodex translates the account selection before building the daemon's provider.
// Login routes can use a separate Store: the relay reloads the selected file per call.
func WireCodex(ctx context.Context, dataDir string, settings models.ModelSettings, cfg config.ModelsConfig, opts codexlink.RelayOptions) (models.ModelSettings, *codexlink.Server, error) {
	selected, err := config.UsesCodex(cfg, settings.CredentialsFile, nil)
	if err != nil || !selected {
		return settings, nil, err
	}
	server, err := codexlink.Listen(ctx, codexlink.NewStore(dataDir), opts)
	if err != nil {
		return settings, nil, err
	}
	settings.Provider = "openai"
	settings.BaseURL = server.BaseURL()
	settings.APIKey = server.Key()
	settings.APIKeyEnv = ""
	settings.APIKeySecret = ""
	settings.FallbackKey = ""
	settings.FallbackKeyEnv = ""
	settings.FallbackKeySecret = ""
	// Account plans provide Responses, so the daemon's decisions use the chat model.
	settings.DecisionsModel = settings.Model
	return settings, server, nil
}
