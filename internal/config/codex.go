package config

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

// UsesCodex selects account authentication using the daemon's provider layers.
// credentialsPath is the operator file selected by the production loader.
func UsesCodex(cfg ModelsConfig, credentialsPath string, getenv func(string) string) (bool, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	provider := strings.TrimSpace(getenv("EXCALIBUR_PROVIDER"))
	if provider == "" {
		provider = strings.TrimSpace(cfg.Provider)
	}
	if provider != "" {
		return strings.EqualFold(provider, "codex"), nil
	}
	if credentialsPath == "" {
		credentialsPath = strings.TrimSpace(getenv("EXCALIBUR_CREDENTIALS_FILE"))
	}
	if credentialsPath == "" {
		credentialsPath = cfg.CredentialsFile
	}
	if credentialsPath == "" {
		return false, nil
	}
	b, err := os.ReadFile(credentialsPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("account provider configuration cannot be read")
	}
	var fields struct {
		Provider string `json:"provider"`
	}
	if json.Unmarshal(b, &fields) != nil {
		return false, errors.New("account provider configuration is invalid")
	}
	return strings.EqualFold(strings.TrimSpace(fields.Provider), "codex"), nil
}
