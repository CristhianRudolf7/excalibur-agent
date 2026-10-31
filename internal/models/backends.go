// HTTP decision backends for Section 19.4: Jev API and pplx-decider
// API or local weights behind one shared shape.
//
// Wire: POST {state, questions:[{id,kind,prompt,options,context}]}
// answers {answers:[{id,choice,score,probability}]}. The exact vendor
// field names are integration details verified against the vendor
// docs at adoption; simulated servers in tests speak this shape and
// the discrepancy is recorded in docs/decisions.
package models

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Default HTTP tuning for the decision backends.
const (
	// DefaultDeciderTimeout bounds one backend call: decisions are
	// hundreds of milliseconds per Section 19.4.
	DefaultDeciderTimeout = 5 * time.Second
	// DefaultDeciderModel is the decisions model id. It is
	// configurable per DecisionsConfig; the operator default is
	// gpt-6-luna for the LLM fallback path.
	DefaultDeciderModel = "gpt-6-luna"
)

type wireQuestion struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Prompt  string   `json:"prompt"`
	Options []string `json:"options,omitempty"`
	Context string   `json:"context,omitempty"`
}

type wireAnswer struct {
	ID          string  `json:"id"`
	Choice      string  `json:"choice,omitempty"`
	Score       *int    `json:"score,omitempty"`
	Probability float64 `json:"probability"`
}

type wireRequest struct {
	State     string         `json:"state"`
	Questions []wireQuestion `json:"questions"`
	Model     string         `json:"model,omitempty"`
}

type wireResponse struct {
	Answers []wireAnswer `json:"answers"`
}

// HTTPDecider is one Jev- or pplx-shaped HTTP backend. Kind names
// the backend for errors and telemetry ("jev", "pplx-api",
// "pplx-local").
type HTTPDecider struct {
	kind       string
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
	timeout    time.Duration
}

// NewJevDecider returns the hosted Jev backend client.
func NewJevDecider(baseURL, apiKey, model string, client *http.Client) *HTTPDecider {
	return newHTTPDecider("jev", baseURL, apiKey, model, client)
}

// NewPplxDecider returns the pplx-decider backend client: the same
// shape against the hosted API or the local 4B sidecar URL.
func NewPplxDecider(baseURL, apiKey, model string, client *http.Client) *HTTPDecider {
	return newHTTPDecider("pplx", baseURL, apiKey, model, client)
}

func newHTTPDecider(kind, baseURL, apiKey, model string, client *http.Client) *HTTPDecider {
	if client == nil {
		client = &http.Client{}
	}
	if model == "" {
		model = DefaultDeciderModel
	}
	return &HTTPDecider{kind: kind, baseURL: baseURL, apiKey: apiKey, model: model, httpClient: client, timeout: DefaultDeciderTimeout}
}

// Kind reports the backend name.
func (d *HTTPDecider) Kind() string { return d.kind }

// Close releases nothing; the HTTP client is caller-owned.
func (d *HTTPDecider) Close() error { return nil }

// Ask sends one bounded question and normalizes the calibrated
// answer. Final-verdict questions are rejected before any network
// use. The request never carries reasoning_effort: the decision
// APIs have no such field in their contract.
func (d *HTTPDecider) Ask(ctx context.Context, q Question) (Answer, error) {
	if err := q.Validate(); err != nil {
		return Answer{}, err
	}
	if d.baseURL == "" {
		return Answer{}, fmt.Errorf("models: %s decider has no base URL", d.kind)
	}
	if err := ctx.Err(); err != nil {
		return Answer{}, err
	}
	body, err := json.Marshal(wireRequest{
		State: q.Context,
		Questions: []wireQuestion{{
			ID: "q0", Kind: string(q.Kind), Prompt: q.Prompt,
			Options: q.Options, Context: q.Context,
		}},
		Model: d.model,
	})
	if err != nil {
		return Answer{}, err
	}
	timeout := d.timeout
	if timeout <= 0 {
		timeout = DefaultDeciderTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL, bytes.NewReader(body))
	if err != nil {
		return Answer{}, fmt.Errorf("models: %s decider request: %w", d.kind, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if d.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+d.apiKey)
	}
	resp, err := d.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Answer{}, ctx.Err()
		}
		return Answer{}, fmt.Errorf("models: %s decider call: %w", d.kind, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Answer{}, fmt.Errorf("models: %s decider status %d", d.kind, resp.StatusCode)
	}
	var decoded wireResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return Answer{}, fmt.Errorf("models: %s decider decode: %w", d.kind, err)
	}
	if len(decoded.Answers) == 0 {
		return Answer{}, fmt.Errorf("models: %s decider returned no answers", d.kind)
	}
	got := decoded.Answers[0]
	ans := Answer{Kind: q.Kind, Choice: got.Choice, Prob: got.Probability}
	if got.Score != nil {
		ans.Score = *got.Score
	}
	return ans.Normalize(q.Kind, q.Options)
}
