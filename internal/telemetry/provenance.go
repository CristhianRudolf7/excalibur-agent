package telemetry

import (
	"fmt"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Provenance links one artifact (edit, memory, evidence, message,
// commit) to its run, step, tool call, request and instruction
// revision, so "why did you do X?" is answered by walking the chain
// with history_search and the journal (Section 22.3).
type Provenance struct {
	ArtifactKind   string      `json:"artifact_kind"`
	ArtifactID     string      `json:"artifact_id"`
	RunID          types.RunID `json:"run_id"`
	Step           int         `json:"step"`
	ToolCallID     string      `json:"tool_call_id,omitempty"`
	RequestID      string      `json:"request_id"`
	InstructionRev int         `json:"instruction_rev_seen"`
	EvidenceID     string      `json:"evidence_id,omitempty"`
}

// Chain renders the why-chain in order: artifact -> tool call -> run
// step -> request -> instructions.
func (p Provenance) Chain() string {
	tool := p.ToolCallID
	if tool == "" {
		tool = "(direct)"
	}
	ev := p.EvidenceID
	if ev == "" {
		ev = "(no separate evidence)"
	}
	return fmt.Sprintf(
		"%s %s was produced by tool call %s in run %s step %d "+
			"from request %s under instructions rev %d; evidence %s.",
		p.ArtifactKind, p.ArtifactID, tool, p.RunID, p.Step,
		p.RequestID, p.InstructionRev, ev,
	)
}

// Validate checks the chain is complete.
func (p Provenance) Validate() error {
	if p.ArtifactKind == "" || p.ArtifactID == "" {
		return fmt.Errorf("telemetry: provenance missing artifact")
	}
	if p.RunID == "" || p.RequestID == "" {
		return fmt.Errorf("telemetry: provenance missing run or request link")
	}
	if p.Step <= 0 {
		return fmt.Errorf("telemetry: provenance step must be positive")
	}
	return nil
}
