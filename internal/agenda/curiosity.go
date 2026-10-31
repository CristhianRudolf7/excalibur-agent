package agenda

import (
	"fmt"
	"time"
)

// Self-initiated research hooks (21.8). In the low-activity window a
// curiosity agenda proposes investigations from the user's interests,
// open questions and unresolved memory contradictions. Every proposal
// is a task with objective, budget and validation criteria; the user
// approves it (or configuration pre-approves up to a budget) and it
// runs as deep-research-run.

// ProposalSource names where a research idea came from (21.8).
type ProposalSource string

// Proposal sources (21.8).
const (
	SourceInterest      ProposalSource = "interest"
	SourceOpenQuestion  ProposalSource = "open_question"
	SourceContradiction ProposalSource = "contradiction"
)

// Proposal is one curiosity-agenda research proposal (21.8).
type Proposal struct {
	ID        string         `json:"id"`
	Topic     string         `json:"topic"`
	Objective string         `json:"objective"`
	Source    ProposalSource `json:"source"`
	// SourceRef points at the interest, question or memory edge.
	SourceRef string `json:"source_ref"`
	// BudgetUSD caps the investigation spend.
	BudgetUSD float64 `json:"budget_usd"`
	// Criteria are the validation criteria (21.8: cited sources and
	// stated limitations by default).
	Criteria []string `json:"criteria"`
	// Workflow names the runner (deep-research-run by default).
	Workflow string        `json:"workflow"`
	State    ProposalState `json:"state"`
	Created  time.Time     `json:"created_utc"`
}

// ProposalState tracks approval (21.8).
type ProposalState string

// Proposal states.
const (
	ProposalPending     ProposalState = "pending"
	ProposalApproved    ProposalState = "approved"
	ProposalPreapproved ProposalState = "preapproved"
	ProposalRejected    ProposalState = "rejected"
	ProposalRunning     ProposalState = "running"
	ProposalDone        ProposalState = "done"
)

// CuriosityPolicy bounds self-initiated research (21.8, user
// configurable; defaults require cited sources and limitations).
type CuriosityPolicy struct {
	// Enabled gates the whole hook.
	Enabled bool `json:"enabled"`
	// PreapproveUnderUSD auto-approves proposals up to this budget;
	// larger ones wait for the user.
	PreapproveUnderUSD float64 `json:"preapprove_under_usd"`
	// MaxPerWeek caps proposals.
	MaxPerWeek int `json:"max_per_week"`
	// AllowedScopes limits which scopes proposals may touch.
	AllowedScopes []string `json:"allowed_scopes,omitempty"`
}

// DefaultCuriosityPolicy is the initial policy (21.8).
func DefaultCuriosityPolicy() CuriosityPolicy {
	return CuriosityPolicy{Enabled: true, PreapproveUnderUSD: 1.0, MaxPerWeek: 3}
}

// DefaultValidationCriteria are required of every proposal (21.8).
func DefaultValidationCriteria() []string {
	return []string{"cites only sources actually read", "states evidence limitations"}
}

// NewProposal builds a proposal with the default validation criteria
// appended when missing.
func NewProposal(id, topic, objective string, source ProposalSource, ref string, budget float64) *Proposal {
	criteria := DefaultValidationCriteria()
	return &Proposal{
		ID: id, Topic: topic, Objective: objective, Source: source,
		SourceRef: ref, BudgetUSD: budget, Criteria: criteria,
		Workflow: BuiltinDeepResearch, State: ProposalPending,
		Created: time.Now().UTC(),
	}
}

// Review applies the approval policy: pre-approve under budget when
// the policy allows, otherwise leave pending for the user (21.8).
func (p *Proposal) Review(policy CuriosityPolicy) ProposalState {
	if p.State != ProposalPending {
		return p.State
	}
	if !policy.Enabled {
		p.State = ProposalRejected
		return p.State
	}
	if p.BudgetUSD <= policy.PreapproveUnderUSD {
		p.State = ProposalPreapproved
	} else {
		p.State = ProposalPending
	}
	return p.State
}

// Approve records explicit user approval (21.8).
func (p *Proposal) Approve() {
	p.State = ProposalApproved
}

// Reject records explicit user rejection (21.8).
func (p *Proposal) Reject() {
	p.State = ProposalRejected
}

// Runnable reports whether the proposal may execute.
func (p *Proposal) Runnable() bool {
	return p.State == ProposalApproved || p.State == ProposalPreapproved
}

// Validate checks the proposal contract (21.8).
func (p *Proposal) Validate() error {
	if p.Topic == "" || p.Objective == "" {
		return fmt.Errorf("agenda: proposal %q needs topic and objective", p.ID)
	}
	if p.BudgetUSD <= 0 {
		return fmt.Errorf("agenda: proposal %q needs a positive budget", p.ID)
	}
	if len(p.Criteria) == 0 {
		return fmt.Errorf("agenda: proposal %q needs validation criteria", p.ID)
	}
	switch p.Source {
	case SourceInterest, SourceOpenQuestion, SourceContradiction:
	default:
		return fmt.Errorf("agenda: proposal %q: unknown source %q", p.ID, p.Source)
	}
	return nil
}
