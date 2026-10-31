package generic

import (
	"context"
	"fmt"
	"net/http"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/openaiwire"
)

// providerName identifies this adapter in normalized errors.
const providerName = "generic"

// Options configures the generic adapter.
type Options struct {
	// BaseURL is the compatible API root (".../v1" style);
	// "/chat/completions" is appended by the shared client. It is
	// required: no standard environment variable exists, so the
	// gateway passes the configured URL.
	BaseURL string
	// APIKey is the bearer credential; "" sends no auth header,
	// which suits local servers without credentials.
	APIKey string
	// HTTPClient performs requests; nil means a private client with
	// no timeout, so callers must bound calls with context
	// deadlines.
	HTTPClient *http.Client
	// ExtraHeaders are sent on every request.
	ExtraHeaders map[string]string
}

// Adapter is the generic providers.Provider.
type Adapter struct {
	inner *openaiwire.Client
	owned *http.Client
}

// New builds the adapter. An empty base URL is an input error.
func New(o Options) (*Adapter, error) {
	if o.BaseURL == "" {
		return nil, &providers.ProviderError{
			Kind:     providers.KindInput,
			Provider: providerName,
			Err:      fmt.Errorf("generic: base URL is required"),
		}
	}
	httpClient := o.HTTPClient
	var owned *http.Client
	if httpClient == nil {
		owned = &http.Client{}
		httpClient = owned
	}
	inner, err := openaiwire.New(openaiwire.Options{
		Provider:     providerName,
		BaseURL:      o.BaseURL,
		APIKey:       o.APIKey,
		HTTPClient:   httpClient,
		ExtraHeaders: o.ExtraHeaders,
		Reasoning:    openaiwire.ReasonEffort,
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
