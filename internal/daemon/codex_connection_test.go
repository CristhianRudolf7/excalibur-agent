package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/codexlink"
	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
)

func TestWireCodexProviderSelection(t *testing.T) {
	t.Setenv("EXCALIBUR_PROVIDER", "")
	file := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(file, []byte(`{"provider":"codex","model":"test-model"}`), 0600); err != nil {
		t.Fatal(err)
	}
	original := models.ModelSettings{Provider: "openai", Model: "test-model", ReasoningEffort: "low", CredentialsFile: file, BaseURL: "https://invalid.example", APIKey: "unused", APIKeySecret: "unused", FallbackKey: "unused", FallbackKeyEnv: "unused", FallbackKeySecret: "unused"}
	settings, server, err := WireCodex(context.Background(), t.TempDir(), original, config.ModelsConfig{}, codexlink.RelayOptions{})
	if err != nil || server == nil {
		t.Fatal("account file selection did not create relay")
	}
	defer server.Close()
	if settings.Provider != "openai" || settings.BaseURL != server.BaseURL() || settings.APIKey != server.Key() || settings.Model != original.Model || settings.ReasoningEffort != "low" || settings.DecisionsModel != settings.Model {
		t.Fatal("incorrect account wiring")
	}
	if settings.APIKeySecret != "" || settings.FallbackKey != "" || settings.FallbackKeyEnv != "" || settings.FallbackKeySecret != "" {
		t.Fatal("deployment credentials retained")
	}
	unchanged, other, err := WireCodex(context.Background(), t.TempDir(), original, config.ModelsConfig{Provider: "stub"}, codexlink.RelayOptions{})
	if err != nil || other != nil || unchanged != original {
		t.Fatal("unrelated provider changed")
	}
}
