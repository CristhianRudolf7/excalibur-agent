package master

import (
	"fmt"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// RequestCard is the Section 6.3 file every task carries: the literal
// source messages, the interpretation, requirements with at least one
// criterion each, assumptions, open questions and pinned durable
// instructions. Small tasks collapse to one implicit requirement.
type RequestCard struct {
	Original           string
	OriginalMsgIDs     []string
	Interpretation     string
	Requirements       []CardRequirement
	Assumptions        []Assumption
	OpenQuestions      []Question
	PinnedInstructions []string
}

// CardRequirement pairs one requirement with its criteria.
type CardRequirement struct {
	Text     string
	Kind     string
	Criteria []CardCriterion
}

// CardCriterion is one observable acceptance check with its method.
type CardCriterion struct {
	Statement string
	Method    string
}

// BuildCard assembles the request file. requirements and criteria may
// be empty for N1 trivia: the lane records one implicit requirement
// ("do what was asked") with a simple inspection criterion, so the
// invariant "every requirement has at least one criterion with a
// method" always holds (Section 6.7).
func BuildCard(original string, msgIDs []string, interpretation string, reqs []CardRequirement, assumptions []Assumption, open []Question, pinned []string) RequestCard {
	if len(reqs) == 0 {
		reqs = []CardRequirement{{
			Text: "Do what was asked",
			Kind: string(types.RequirementFunctional),
			Criteria: []CardCriterion{{
				Statement: "The requested outcome is delivered",
				Method:    string(types.CriterionInspection),
			}},
		}}
	}
	for i := range reqs {
		if len(reqs[i].Criteria) == 0 {
			reqs[i].Criteria = []CardCriterion{{
				Statement: "The requirement is observably met",
				Method:    string(types.CriterionInspection),
			}}
		}
		for j := range reqs[i].Criteria {
			if reqs[i].Criteria[j].Method == "" {
				reqs[i].Criteria[j].Method = string(types.CriterionInspection)
			}
		}
		if reqs[i].Kind == "" {
			reqs[i].Kind = string(types.RequirementFunctional)
		}
	}
	return RequestCard{
		Original:           original,
		OriginalMsgIDs:     append([]string(nil), msgIDs...),
		Interpretation:     interpretation,
		Requirements:       reqs,
		Assumptions:        append([]Assumption(nil), assumptions...),
		OpenQuestions:      append([]Question(nil), open...),
		PinnedInstructions: append([]string(nil), pinned...),
	}
}

// Validate enforces the Section 6.7 invariant: literal original with
// msg ids, and every requirement carrying a criterion with a method.
func (c RequestCard) Validate() error {
	if strings.TrimSpace(c.Original) == "" {
		return fmt.Errorf("master: request card needs the literal original")
	}
	if len(c.OriginalMsgIDs) == 0 {
		return fmt.Errorf("master: request card needs source msg ids")
	}
	if len(c.Requirements) == 0 {
		return fmt.Errorf("master: request card needs at least one requirement")
	}
	for _, r := range c.Requirements {
		if strings.TrimSpace(r.Text) == "" {
			return fmt.Errorf("master: requirement needs text")
		}
		if len(r.Criteria) == 0 {
			return fmt.Errorf("master: requirement %q needs a criterion", r.Text)
		}
		for _, cr := range r.Criteria {
			if strings.TrimSpace(cr.Statement) == "" || strings.TrimSpace(cr.Method) == "" {
				return fmt.Errorf("master: criterion needs a statement and a method")
			}
		}
	}
	return nil
}

// ToTypes converts the card to the shared RequestFile shape.
func (c RequestCard) ToTypes() types.RequestFile {
	rf := types.RequestFile{
		Original:           c.Original,
		OriginalMessageIDs: append([]string(nil), c.OriginalMsgIDs...),
		Interpretation:     c.Interpretation,
		PinnedInstructions: append([]string(nil), c.PinnedInstructions...),
	}
	for _, r := range c.Requirements {
		rf.Requirements = append(rf.Requirements, r.Text)
		for _, cr := range r.Criteria {
			rf.Criteria = append(rf.Criteria, cr.Statement+" ["+cr.Method+"]")
		}
	}
	for _, a := range c.Assumptions {
		rf.Assumptions = append(rf.Assumptions, a.Text+" ("+a.Reason+")")
	}
	for _, q := range c.OpenQuestions {
		rf.OpenQuestions = append(rf.OpenQuestions, q.Topic+" (default: "+q.Default+")")
	}
	return rf
}
