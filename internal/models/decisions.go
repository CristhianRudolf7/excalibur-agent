// Decisions model configuration for Section 19.4.
//
// The operator credentials carry three model slots: the chat model
// with reasoning_effort=low, the embedding model
// (text-embedding-3-small, whose contract has no reasoning effort)
// and the decisions model (gpt-6-luna). This file keeps the
// decisions default and the rule that reasoning_effort is only sent
// where the operation's contract accepts it.
package models

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// Operator model defaults from the credentials file.
const (
	// DefaultDecisionsModel is the operator decisions model. It is
	// configurable per deployment; code never hardcodes another.
	DefaultDecisionsModel = "gpt-6-luna"
	// DefaultEmbeddingModel is the operator embedding model. Its
	// contract accepts no reasoning effort.
	DefaultEmbeddingModel = "text-embedding-3-small"
	// DefaultDecisionsEffort is the reasoning effort for chat
	// decisions on models that accept it.
	DefaultDecisionsEffort = "low"
)

// DecisionsConfig names the decisions model and the effort sent
// with chat decisions. Embedding operations always omit the effort.
type DecisionsConfig struct {
	// Model is the decisions chat model (default gpt-6-luna).
	Model string
	// ReasoningEffort is sent with chat decisions (default low).
	ReasoningEffort string
	// EmbeddingModel is recorded for the no-effort rule (default
	// text-embedding-3-small).
	EmbeddingModel string
}

func (c DecisionsConfig) withDefaults() DecisionsConfig {
	if c.Model == "" {
		c.Model = DefaultDecisionsModel
	}
	if c.ReasoningEffort == "" {
		c.ReasoningEffort = DefaultDecisionsEffort
	}
	if c.EmbeddingModel == "" {
		c.EmbeddingModel = DefaultEmbeddingModel
	}
	return c
}

// ModelName returns the effective decisions model.
func (c DecisionsConfig) ModelName() string { return c.withDefaults().Model }

// EmbeddingModelName returns the effective embedding model.
func (c DecisionsConfig) EmbeddingModelName() string { return c.withDefaults().EmbeddingModel }

// ChatRequest builds a chat decisions request for model carrying
// the configured reasoning effort.
func (c DecisionsConfig) ChatRequest(model string, msgs []providers.Message) providers.ChatRequest {
	c = c.withDefaults()
	if model == "" {
		model = c.Model
	}
	return providers.ChatRequest{
		Model:           model,
		Messages:        msgs,
		ReasoningEffort: c.ReasoningEffort,
	}
}

// EmbeddingRequest builds the embedding-operation request shape:
// model plus input, with no reasoning_effort field, because the
// embedding contract does not accept one. The providers package has
// no remote-embedding call yet; this helper pins the rule so the
// future adapter cannot inherit the chat effort by mistake.
func (c DecisionsConfig) EmbeddingRequest(input string) map[string]any {
	c = c.withDefaults()
	return map[string]any{"model": c.EmbeddingModel, "input": input}
}

// OmitReasoningEffort reports whether an operation must not carry
// reasoning_effort. Embedding operations never do; chat decisions
// do when the role entry sets an effort.
func OmitReasoningEffort(operation string) bool {
	return operation == "embeddings"
}
