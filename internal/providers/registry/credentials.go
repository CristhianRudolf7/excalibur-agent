package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OperatorCredentials is the OpenAI-style operator credentials file:
// the chat endpoint and model, the fallback key for quota/rate-limit
// retries, the reasoning effort for chat operations, and the
// embedding and decisions models. The embedding contract accepts no
// reasoning effort, so it never carries one (Section 19.2).
type OperatorCredentials struct {
	// BaseURL is the API root, e.g. https://api.openai.com/v1.
	BaseURL string `json:"base_url"`
	// APIKey is the primary bearer credential.
	APIKey string `json:"api_key"`
	// FallbackAPIKey retries quota/rate-limit errors only, with
	// bounded retries (Section 19.5).
	FallbackAPIKey string `json:"fallback_api_key,omitempty"`
	// Model is the chat model id.
	Model string `json:"model,omitempty"`
	// ReasoningEffort is the chat reasoning effort (default low).
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// EmbeddingModel is the embedding model id; its requests never
	// carry reasoning effort.
	EmbeddingModel string `json:"embedding_model,omitempty"`
	// DecisionsModel is the decisions chat model id.
	DecisionsModel string `json:"decisions_model,omitempty"`
}

// CredentialsEnv names the variable pointing at the operator
// credentials file.
const CredentialsEnv = "EXCALIBUR_CREDENTIALS_FILE"

// DefaultCredentialsPath is the operator credentials file loaded
// when no explicit path is configured and the file exists:
// ~/.config/excalibur/credentials/openai.json.
func DefaultCredentialsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "excalibur", "credentials", "openai.json")
}

// CredentialsPathFromEnv reports the explicit credentials-file path
// from the environment, or "" when unset.
func CredentialsPathFromEnv() string {
	return strings.TrimSpace(os.Getenv(CredentialsEnv))
}

// LoadOperatorCredentials decodes the JSON credentials file at path.
// Unknown fields are ignored; an empty path or a missing file
// returns an empty value with no error so callers treat the file as
// an optional layer. Malformed JSON is an error.
func LoadOperatorCredentials(path string) (OperatorCredentials, error) {
	var out OperatorCredentials
	path = strings.TrimSpace(path)
	if path == "" {
		return out, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, fmt.Errorf("registry: read credentials %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return out, fmt.Errorf("registry: credentials %s is empty", path)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("registry: decode credentials %s: %w", path, err)
	}
	out.BaseURL = strings.TrimSpace(out.BaseURL)
	out.APIKey = strings.TrimSpace(out.APIKey)
	out.FallbackAPIKey = strings.TrimSpace(out.FallbackAPIKey)
	out.Model = strings.TrimSpace(out.Model)
	out.ReasoningEffort = strings.TrimSpace(out.ReasoningEffort)
	out.EmbeddingModel = strings.TrimSpace(out.EmbeddingModel)
	out.DecisionsModel = strings.TrimSpace(out.DecisionsModel)
	return out, nil
}

// FillDefaults returns a copy with the operator chat defaults
// applied where the file left gaps: reasoning effort low and the
// models of Section 19.4. Base URL, keys and model stay empty when
// unset so callers detect an unconfigured file.
func (c OperatorCredentials) FillDefaults() OperatorCredentials {
	if c.ReasoningEffort == "" {
		c.ReasoningEffort = "low"
	}
	if c.EmbeddingModel == "" {
		c.EmbeddingModel = "text-embedding-3-small"
	}
	if c.DecisionsModel == "" {
		c.DecisionsModel = "gpt-6-luna"
	}
	return c
}
