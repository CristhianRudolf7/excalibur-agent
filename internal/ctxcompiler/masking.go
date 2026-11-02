package ctxcompiler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Observation is one tool result eligible for masking: the call
// identity, a one-line label, the full output and its protection
// flags. Full outputs stay in the content store; the context only
// keeps markers (11.3).
type Observation struct {
	// Mask is set only after the full-to-marker transition.
	Mask *ObservationMask
	// CallID is the tool call identifier (get_tool_output key).
	CallID string
	// ToolName is the tool that produced the output.
	ToolName string
	// Label summarizes the call, e.g. "read_file src/main.go".
	Label string
	// Output is the full literal output.
	Output string
	// Pinned keeps the observation literal (pin_observation, 11.3.5).
	Pinned bool
	// IsError marks an error result, which is conserved while recent
	// (11.3.5).
	IsError bool
	// Tokens overrides the estimated cost; 0 means estimate.
	Tokens int
}

// ObservationMask is immutable metadata for an omitted observation. Full output
// remains in the tool output store, addressed by CallID.
type ObservationMask struct {
	CallID string `json:"call_id"`
	Digest string `json:"digest"`
	Lines  int    `json:"lines"`
	Marker string `json:"marker"`
}

func MaskIdempotentEnabled() bool { return envFlagDefaultOn(nil, "EXCALIBUR_MASK_IDEMPOTENT") }

func alreadyMasked(o Observation) bool {
	if !MaskIdempotentEnabled() {
		return false
	}
	// Old journal records may predate typed masking metadata. Match only this
	// observation's complete marker, never an arbitrary mention in tool output.
	return o.Mask != nil || (strings.HasPrefix(o.Output, "[observation "+o.CallID+" omitted: ") && strings.HasSuffix(o.Output, fmt.Sprintf("; retrieve with get_tool_output(%q)]", o.CallID)))
}

// linesOf counts newline-terminated lines, at least 1 for non-empty
// output.
func linesOf(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// shortDigest returns the first 8 hex chars of the output SHA-256.
func shortDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}

// RenderMarker renders the L4 placeholder for a masked observation.
// The format is fixed so tests and the model can rely on it.
func RenderMarker(o Observation) string {
	if alreadyMasked(o) {
		if o.Mask != nil {
			return o.Mask.Marker
		}
		return o.Output
	}
	label := o.Label
	if label == "" {
		label = o.ToolName
	}
	return fmt.Sprintf("[observation %s omitted: %s, %d lines, sha %s; retrieve with get_tool_output(%q)]",
		o.CallID, label, linesOf(o.Output), shortDigest(o.Output), o.CallID)
}

// ContentStore keeps full observation outputs addressable by call
// ID. Production uses the blob store of Section 4.2; tests use the
// in-memory fake.
type ContentStore interface {
	// Store saves the full output of callID.
	Store(callID, output string)
	// Load returns the exact stored output of callID.
	Load(callID string) (string, error)
}

// MaskPlan is the outcome of one masking pass.
type MaskPlan struct {
	// Masked parallels the input: true where the observation must be
	// replaced by its marker.
	Masked []bool
	// Markers holds the marker per masked input, "" elsewhere.
	Markers []string
	// UnmaskedTokens is the remaining literal observation cost.
	UnmaskedTokens int
	// Protected counts the observations kept literal.
	Protected int
}

// Masker applies observation masking in batches (11.3). The newest
// LiteralObservations entries, pinned observations and error results
// are never masked. Masking runs only when the unmasked total passes
// the trigger, or on resume past the provider cache TTL when the
// cache is already lost; then it masks the oldest candidates first
// down toward the target, or until only protected entries remain. The
// cache breaks once per batch, never per step. Threshold comparisons
// use the token estimate with the configured safety margin (11.7).
type Masker struct {
	keepLast int
	trigger  int
	target   int
	margin   float64
	counter  Counter
}

// NewMasker builds a Masker from cfg. A nil counter selects a
// default Estimator.
func NewMasker(cfg Config, counter Counter) *Masker {
	if counter == nil {
		counter = NewEstimator(0)
	}
	return &Masker{
		keepLast: cfg.LiteralObservations,
		trigger:  cfg.MaskTriggerTokens,
		target:   cfg.MaskTargetTokens,
		margin:   cfg.EstimateMargin,
		counter:  counter,
	}
}

// cost returns the token cost of one observation.
func (m *Masker) cost(o Observation) int {
	if alreadyMasked(o) {
		return 0
	}
	if o.Tokens > 0 {
		return o.Tokens
	}
	return m.counter.Count(o.Output)
}

// UnmaskedTotal sums the literal cost of every observation.
func (m *Masker) UnmaskedTotal(obs []Observation) int {
	total := 0
	for _, o := range obs {
		total += m.cost(o)
	}
	return total
}

// protected reports whether index i is exempt from masking.
func protected(obs []Observation, i, keepLast int) bool {
	if obs[i].Pinned || obs[i].IsError {
		return true
	}
	return i >= len(obs)-keepLast
}

// Apply computes one masking batch. cacheExpired advances the batch
// on resume past the provider TTL (11.3.4): masking then is free
// because the cache is already lost.
func (m *Masker) Apply(obs []Observation, cacheExpired bool) MaskPlan {
	plan := MaskPlan{Masked: make([]bool, len(obs)), Markers: make([]string, len(obs))}
	costs := make([]int, len(obs))
	total := 0
	for i, o := range obs {
		costs[i] = m.cost(o)
		total += costs[i]
	}
	plan.UnmaskedTokens = total
	if WithMargin(total, m.margin) <= m.trigger && !cacheExpired {
		for i := range obs {
			if protected(obs, i, m.keepLast) {
				plan.Protected++
			}
		}
		return plan
	}
	if cacheExpired && WithMargin(total, m.margin) <= m.target {
		for i := range obs {
			if protected(obs, i, m.keepLast) {
				plan.Protected++
			}
		}
		return plan
	}
	remaining := total
	for i := range obs {
		if WithMargin(remaining, m.margin) <= m.target {
			break
		}
		if alreadyMasked(obs[i]) || protected(obs, i, m.keepLast) {
			continue
		}
		plan.Masked[i] = true
		plan.Markers[i] = RenderMarker(obs[i])
		remaining -= costs[i]
	}
	plan.UnmaskedTokens = remaining
	for i := range obs {
		if !plan.Masked[i] {
			plan.Protected++
		}
	}
	return plan
}

// ApplyToActivity rewrites masked tool results inside activity events
// in place: entries whose ToolResult.CallID is masked keep their call
// identity and error flag but carry the marker as output. It returns
// the masked call IDs.
func (p MaskPlan) ApplyToActivity(obs []Observation, activity []ActivityEvent) []string {
	byCall := make(map[string]string, len(obs))
	identities := make(map[string]*ObservationMask, len(obs))
	var masked []string
	for i, o := range obs {
		if p.Masked[i] && !alreadyMasked(o) {
			byCall[o.CallID] = p.Markers[i]
			identities[o.CallID] = &ObservationMask{CallID: o.CallID, Digest: shortDigest(o.Output), Lines: linesOf(o.Output), Marker: p.Markers[i]}
			masked = append(masked, o.CallID)
		}
	}
	if len(byCall) == 0 {
		return nil
	}
	for i := range activity {
		r := activity[i].ToolResult
		if r == nil {
			continue
		}
		if marker, ok := byCall[r.CallID]; ok {
			if MaskIdempotentEnabled() {
				activity[i].Mask = identities[r.CallID]
				// Replace the result rather than mutating pointers held by earlier requests.
				copyResult := *r
				r = &copyResult
			}
			r.Output = marker
			activity[i].ToolResult = r
		}
	}
	return masked
}
