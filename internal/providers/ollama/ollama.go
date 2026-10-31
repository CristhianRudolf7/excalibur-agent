package ollama

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/openaiwire"
)

// providerName identifies this adapter in normalized errors.
const providerName = "ollama"

// DefaultBaseURL is the local Ollama OpenAI-compatible root;
// "/chat/completions" is appended by the shared client.
const DefaultBaseURL = "http://localhost:11434/v1"

// Options configures the Ollama adapter.
type Options struct {
	// BaseURL is the OpenAI-compatible API root; "" resolves via
	// OLLAMA_BASE_URL, OLLAMA_HOST (with "/v1" appended) or the
	// local default, in that order.
	BaseURL string
	// HTTPClient performs requests; nil means a private client with
	// no timeout, so callers must bound calls with context
	// deadlines.
	HTTPClient *http.Client
}

// Adapter is the Ollama providers.Provider.
type Adapter struct {
	inner *openaiwire.Client
	owned *http.Client
}

// resolveBaseURL applies the documented precedence.
func resolveBaseURL(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv("OLLAMA_BASE_URL"); v != "" {
		return v
	}
	if v := strings.TrimRight(os.Getenv("OLLAMA_HOST"), "/"); v != "" {
		if !strings.Contains(v, "://") {
			v = "http://" + v
		}
		return v + "/v1"
	}
	return DefaultBaseURL
}

// New builds the adapter. No credential is required.
func New(o Options) (*Adapter, error) {
	httpClient := o.HTTPClient
	var owned *http.Client
	if httpClient == nil {
		owned = &http.Client{}
		httpClient = owned
	}
	inner, err := openaiwire.New(openaiwire.Options{
		Provider:   providerName,
		BaseURL:    resolveBaseURL(o.BaseURL),
		HTTPClient: httpClient,
		Reasoning:  openaiwire.ReasonOmit,
	})
	if err != nil {
		return nil, err
	}
	return &Adapter{inner: inner, owned: owned}, nil
}

// Name implements providers.Provider.
func (a *Adapter) Name() string { return providerName }

// Chat implements providers.Provider.
func (a *Adapter) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	return a.inner.Chat(ctx, req)
}

// Stream implements providers.Provider.
func (a *Adapter) Stream(ctx context.Context, req providers.ChatRequest, emit func(providers.StreamEvent) error) error {
	return a.inner.Stream(ctx, req, emit)
}

// Close implements providers.Provider.
func (a *Adapter) Close() error {
	if a.owned != nil {
		a.owned.CloseIdleConnections()
	}
	return nil
}

// CostUSD prices one call at zero: local inference carries no API
// cost, but usage is still reported for budgets and telemetry.
func CostUSD(_ providers.Usage) float64 { return 0 }
