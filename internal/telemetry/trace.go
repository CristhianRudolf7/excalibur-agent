package telemetry

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Pinned OpenTelemetry GenAI convention version. The conventions are
// still Development upstream, so the version in use is fixed here and
// recorded on every exported span (Section 22.1).
const GenAIConventionsVersion = "1.37.0"

// Span names used by the harness.
const (
	SpanInvokeAgent = "invoke_agent"
	SpanExecuteTool = "execute_tool"
	SpanChat        = "chat"
)

// Span is one exported OpenTelemetry-compatible span. It carries the
// GenAI semantic-convention attributes the validator checks; the SDK
// itself stays optional so unit tests never need a collector.
type Span struct {
	TraceID    string         `json:"trace_id"`
	SpanID     string         `json:"span_id"`
	ParentID   string         `json:"parent_span_id,omitempty"`
	Name       string         `json:"name"`
	Kind       string         `json:"kind"`
	StartedAt  time.Time      `json:"started_at"`
	EndedAt    time.Time      `json:"ended_at,omitempty"`
	Status     string         `json:"status"`
	Attributes map[string]any `json:"attributes"`
}

// Tracer builds spans for one run.
type Tracer struct {
	TraceID string
	RunID   types.RunID
}

// NewTracer starts a trace for a run. TraceID must be 32 lowercase hex
// chars; callers without one should use NewTraceID.
func NewTracer(traceID string, runID types.RunID) *Tracer {
	return &Tracer{TraceID: traceID, RunID: runID}
}

// StartAgent opens the root invoke_agent span.
func (t *Tracer) StartAgent(model, system string) Span {
	return Span{
		TraceID:   t.TraceID,
		SpanID:    NewSpanID(),
		Name:      SpanInvokeAgent,
		Kind:      "server",
		StartedAt: time.Now().UTC(),
		Status:    "unset",
		Attributes: map[string]any{
			"gen_ai.conventions_version": GenAIConventionsVersion,
			"gen_ai.operation.name":      SpanInvokeAgent,
			"gen_ai.system":              "excalibur",
			"gen_ai.request.model":       model,
			"excalibur.run_id":           string(t.RunID),
			"gen_ai.prompt":              system,
		},
	}
}

// StartModel opens one model-call span under parent.
func (t *Tracer) StartModel(parent Span, model, role string, inputTokens int64) Span {
	return Span{
		TraceID:   t.TraceID,
		SpanID:    NewSpanID(),
		ParentID:  parent.SpanID,
		Name:      SpanChat,
		Kind:      "client",
		StartedAt: time.Now().UTC(),
		Status:    "unset",
		Attributes: map[string]any{
			"gen_ai.conventions_version": GenAIConventionsVersion,
			"gen_ai.operation.name":      SpanChat,
			"gen_ai.system":              "excalibur",
			"gen_ai.request.model":       model,
			"gen_ai.agent.role":          role,
			"gen_ai.usage.input_tokens":  inputTokens,
			"excalibur.run_id":           string(t.RunID),
		},
	}
}

// StartTool opens one execute_tool span under parent.
func (t *Tracer) StartTool(parent Span, tool, callID string) Span {
	return Span{
		TraceID:   t.TraceID,
		SpanID:    NewSpanID(),
		ParentID:  parent.SpanID,
		Name:      SpanExecuteTool,
		Kind:      "client",
		StartedAt: time.Now().UTC(),
		Status:    "unset",
		Attributes: map[string]any{
			"gen_ai.conventions_version": GenAIConventionsVersion,
			"gen_ai.operation.name":      SpanExecuteTool,
			"gen_ai.tool.name":           tool,
			"gen_ai.tool.call.id":        callID,
			"excalibur.run_id":           string(t.RunID),
		},
	}
}

// Finish closes a span with a status ("ok" or "error").
func Finish(span *Span, status string, extra map[string]any) {
	span.EndedAt = time.Now().UTC()
	span.Status = status
	for k, v := range extra {
		span.Attributes[k] = v
	}
}

// ValidateSpan checks the pinned conventions: name, trace ids and the
// required gen_ai attributes.
func ValidateSpan(s Span) error {
	if s.TraceID == "" || s.SpanID == "" {
		return fmt.Errorf("telemetry: span %q missing ids", s.Name)
	}
	if v, ok := s.Attributes["gen_ai.conventions_version"]; !ok || v != GenAIConventionsVersion {
		return fmt.Errorf("telemetry: span %q has wrong conventions version: %v", s.Name, v)
	}
	switch s.Name {
	case SpanInvokeAgent:
		if _, ok := s.Attributes["gen_ai.request.model"]; !ok {
			return fmt.Errorf("telemetry: invoke_agent span missing gen_ai.request.model")
		}
	case SpanExecuteTool:
		if _, ok := s.Attributes["gen_ai.tool.name"]; !ok {
			return fmt.Errorf("telemetry: execute_tool span missing gen_ai.tool.name")
		}
	case SpanChat:
		if _, ok := s.Attributes["gen_ai.usage.input_tokens"]; !ok {
			return fmt.Errorf("telemetry: chat span missing gen_ai.usage.input_tokens")
		}
	default:
		return fmt.Errorf("telemetry: unknown span name %q", s.Name)
	}
	if _, err := json.Marshal(s.Attributes); err != nil {
		return fmt.Errorf("telemetry: span attributes not JSON: %w", err)
	}
	return nil
}
