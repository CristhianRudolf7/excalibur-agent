package openai

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/openaiwire"
)

// providerName identifies this adapter in normalized errors.
const providerName = "openai"

// envAPIKey is the environment credential (Section 19.2: vault first
// in H10, then environment, then user configuration file).
const envAPIKey = "OPENAI_API_KEY"

// Options configures the OpenAI adapter.
type Options struct {
	// APIKey is the bearer credential; "" falls back to
	// OPENAI_API_KEY. Calls without any key fail locally as auth
	// errors without touching the network.
	APIKey string
	// BaseURL is the API root; "" means DefaultBaseURL.
	BaseURL string
	// HTTPClient performs requests; nil means a private client with
	// no timeout, so callers must bound calls with context
	// deadlines.
	HTTPClient *http.Client
	// Organization sets the OpenAI-Organization header.
	Organization string
	// UseResponses selects the Responses wire instead of Chat
	// Completions (Section 19.2).
	UseResponses bool
	// PromptCacheKey is the stable per (role, scope) cache key for
	// the automatic prefix cache (Section 11.6). It only applies to
	// the Responses wire, which is the single wire that accepts it.
	PromptCacheKey string
}

// Adapter is the OpenAI providers.Provider.
type Adapter struct {
	key     string
	chat    *openaiwire.Client
	resp    *respClient
	owned   *http.Client
	useResp bool
}

// New builds the adapter. Missing credentials are a call-time auth
// error, not a construction error, so gateways can build adapters
// before keys resolve.
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
	headers := map[string]string{}
	if o.Organization != "" {
		headers["OpenAI-Organization"] = o.Organization
	}
	chat, err := openaiwire.New(openaiwire.Options{
		Provider:               providerName,
		BaseURL:                base,
		APIKey:                 key,
		HTTPClient:             httpClient,
		ExtraHeaders:           headers,
		Reasoning:              openaiwire.ReasonEffort,
		UseMaxCompletionTokens: true,
	})
	if err != nil {
		return nil, err
	}
	return &Adapter{
		key:  key,
		chat: chat,
		resp: &respClient{
			provider:       providerName,
			baseURL:        strings.TrimRight(base, "/"),
			apiKey:         key,
			http:           httpClient,
			headers:        headers,
			promptCacheKey: o.PromptCacheKey,
		},
		owned:   owned,
		useResp: o.UseResponses,
	}, nil
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
		Err:      fmt.Errorf("openai: missing API key (set %s)", envAPIKey),
	}
}

// Chat implements providers.Provider.
func (a *Adapter) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	if a.key == "" {
		return providers.ChatResponse{}, a.missingKey(req.Model)
	}
	if !a.useResp {
		for _, m := range req.Messages {
			if len(m.Images) > 0 || (m.ToolResult != nil && len(m.ToolResult.Images) > 0) {
				return providers.ChatResponse{}, fmt.Errorf("openai: image input requires Responses")
			}
		}
	}
	if a.useResp {
		return a.resp.chat(ctx, req)
	}
	return a.chat.Chat(ctx, req)
}

// Stream implements providers.Provider.
func (a *Adapter) Stream(ctx context.Context, req providers.ChatRequest, emit func(providers.StreamEvent) error) error {
	if a.key == "" {
		return a.missingKey(req.Model)
	}
	if !a.useResp {
		for _, m := range req.Messages {
			if len(m.Images) > 0 || (m.ToolResult != nil && len(m.ToolResult.Images) > 0) {
				return fmt.Errorf("openai: image input requires Responses")
			}
		}
	}
	if a.useResp {
		return a.resp.stream(ctx, req, emit)
	}
	return a.chat.Stream(ctx, req, emit)
}

// Close implements providers.Provider.
func (a *Adapter) Close() error {
	if a.owned != nil {
		a.owned.CloseIdleConnections()
	}
	return nil
}
