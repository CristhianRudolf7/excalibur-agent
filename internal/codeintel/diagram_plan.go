package codeintel

import "context"

const maxDiagramDetails = 6

type plannedDiagram struct {
	Title      string
	Projection diagramProjection
}

// planDiagrams splits an overflowing overview into a map and at most six
// details. Ranking and directory focus come from the same indexed snapshot.
func planDiagrams(ctx context.Context, g *Graph, a diagramArgs, modulePrefix string) ([]plannedDiagram, error) {
	p, err := projectDiagram(ctx, g, a, modulePrefix)
	if err != nil {
		return nil, err
	}
	title := "Code " + a.Level
	if a.Focus != "" {
		title += " — " + a.Focus
	}
	out := []plannedDiagram{{title, p.bounded(a.MaxNodes)}}
	if a.Level != "overview" || len(p.Nodes) <= a.MaxNodes {
		return out, nil
	}
	for _, id := range p.ranked() {
		n := p.Nodes[id]
		if n.Group != "repository" {
			continue
		}
		detail := a
		detail.Level = "module"
		detail.Focus = id
		projection, err := projectDiagram(ctx, g, detail, modulePrefix)
		if err != nil {
			return nil, err
		}
		out = append(out, plannedDiagram{"Module — " + id, projection.bounded(a.MaxNodes)})
		if len(out) == maxDiagramDetails+1 {
			break
		}
	}
	return out, nil
}
