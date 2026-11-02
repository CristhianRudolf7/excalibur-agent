package automations

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ValidateSpec validates the schedule and the complete executable graph.
func ValidateSpec(s Spec) error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name must not be empty")
	}
	if len(s.Steps) < 1 || len(s.Steps) > 30 {
		return fmt.Errorf("spec requires 1 to 30 steps")
	}
	if err := validateSchedule(s.Schedule); err != nil {
		return err
	}
	ids := map[string]Step{}
	for _, step := range s.Steps {
		if strings.TrimSpace(step.ID) == "" {
			return fmt.Errorf("step id must not be empty")
		}
		if _, ok := ids[step.ID]; ok {
			return fmt.Errorf("duplicate step id %q", step.ID)
		}
		ids[step.ID] = step
		switch step.Type {
		case StepAgent, StepNotify, StepCondition:
		case StepScript:
			if step.Language != "python" && step.Language != "node" && step.Language != "bash" {
				return fmt.Errorf("script language must be python, node or bash")
			}
		case StepTool:
			if strings.TrimSpace(step.Tool) == "" {
				return fmt.Errorf("tool step requires a tool name")
			}
		default:
			return fmt.Errorf("unknown step type %q", step.Type)
		}
	}
	outgoing := map[string][]Edge{}
	for _, e := range s.Edges {
		if _, ok := ids[e.From]; !ok {
			return fmt.Errorf("edge references unknown step %q", e.From)
		}
		if _, ok := ids[e.To]; !ok {
			return fmt.Errorf("edge references unknown step %q", e.To)
		}
		outgoing[e.From] = append(outgoing[e.From], e)
	}
	for id, step := range ids {
		edges := outgoing[id]
		if step.Type == StepCondition {
			labels := map[string]int{}
			for _, e := range edges {
				labels[e.Label]++
			}
			if len(edges) != 2 || labels["yes"] != 1 || labels["no"] != 1 {
				return fmt.Errorf("condition %q requires exactly one yes and one no edge", id)
			}
		} else if len(edges) > 1 {
			return fmt.Errorf("step %q has more than one outgoing edge", id)
		} else if len(edges) == 1 && edges[0].Label != "" {
			return fmt.Errorf("only condition edges may have labels")
		}
	}
	colors := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if colors[id] == 1 {
			return fmt.Errorf("graph contains a cycle")
		}
		if colors[id] == 2 {
			return nil
		}
		colors[id] = 1
		for _, e := range outgoing[id] {
			if err := visit(e.To); err != nil {
				return err
			}
		}
		colors[id] = 2
		return nil
	}
	if err := visit(s.Steps[0].ID); err != nil {
		return err
	}
	if len(colors) != len(ids) {
		return fmt.Errorf("every step must be reachable from the entry step")
	}
	if _, err := json.Marshal(s); err != nil {
		return fmt.Errorf("spec must be JSON encodable: %w", err)
	}
	return nil
}
