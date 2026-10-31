package openrouter

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/openaiwire"
)

// providerName identifies this adapter in normalized errors.
const providerName = "openrouter"

// DefaultBaseURL is the OpenRouter API root; "/chat/completions" is
// appended by the shared client.
const DefaultBaseURL = "https://openrouter.ai/api/v1"

// envAPIKey is the environment credential (Section 19.2: vault first
// in H10, then environment, then user configuration file).
const envAPIKey = "OPENROUTER_API_KEY"

// defaultTitle is the X-Title attribution header.
const defaultTitle = "Excalibur"

// Options configures the OpenRouter adapter.
type Options struct {
	// APIKey is the bearer credential; "" falls back to
	// OPENROUTER_API_KEY. Calls without any key fail locally as
	// auth errors without touching the network.
	APIKey string
	// BaseURL is the API root; "" means DefaultBaseURL.
	BaseURL string
	// HTTPClient performs requests; nil means a private client with
	// no timeout, so callers must bound calls with context
	// deadlines.
	HTTPClient *http.Client
	// AppURL sets the HTTP-Referer attribution header when non-empty.
	AppURL string
	// AppName sets the X-Title attribution header; "" means
	// "Excalibur".
	AppName string
}

// Adapter is the OpenRouter providers.Provider.
type Adapter struct {
	key   string
	inner *openaiwire.Client
	owned *http.Client
}

// New builds the adapter.
func New(o Options) (*Adapter, error) {
	key := o.APIKey
	if key == "" {
		key = os.Getenv(envAPIKey)
	}
	base := o.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	httpClient := o.HTTPClient
	var owned *http.Client
	if httpClient == nil {
		owned = &http.Client{}
		httpClient = owned
	}
	title := o.AppName
	if title == "" {
		title = defaultTitle
	}
	headers := map[string]string{"X-Title": title}
	if o.AppURL != "" {
		headers["HTTP-Referer"] = o.AppURL
	}
	inner, err := openaiwire.New(openaiwire.Options{
		Provider:     providerName,
		BaseURL:      base,
		APIKey:       key,
		HTTPClient:   httpClient,
		ExtraHeaders: headers,
		Reasoning:    openaiwire.ReasonObject,
	})
	if err != nil {
		return nil, err
	}
	return &Adapter{key: key, inner: inner, owned: owned}, nil
}

// Name implements providers.Provider.
func (a *Adapter) Name() string { return providerName }

// missingKey builds the local auth error used when no credential
// resolved from options or environment.
func (a *Adapter) missingKey(model string) error {
	return &providers.ProviderError{
		Kind:     providers.KindAuth,
		Provider: providerName,
		Model:    model,
		Err:      fmt.Errorf("openrouter: missing API key (set %s)", envAPIKey),
	}
}

// Chat implements providers.Provider.
func (a *Adapter) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	if a.key == "" {
		return providers.ChatResponse{}, a.missingKey(req.Model)
	}
	return a.inner.Chat(ctx, req)
}

// Stream implements providers.Provider.
func (a *Adapter) Stream(ctx context.Context, req providers.ChatRequest, emit func(providers.StreamEvent) error) error {
	if a.key == "" {
		return a.missingKey(req.Model)
	}
	return a.inner.Stream(ctx, req, emit)
}

// Close implements providers.Provider.
func (a *Adapter) Close() error {
	if a.owned != nil {
		a.owned.CloseIdleConnections()
	}
	return nil
}
