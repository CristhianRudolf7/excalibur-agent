package codeintel

import (
	"context"
	"fmt"
	"testing"
)

func TestDiagramPlanSplitsOverview(t *testing.T) {
	g, err := OpenGraph(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	for i := 0; i < 12; i++ {
		rel := fmt.Sprintf("module%02d/main.go", i)
		upd, err := ParseFile(rel, []byte("package example\nfunc Main() {}\n"))
		if err != nil {
			t.Fatal(err)
		}
		if err := g.UpsertFile(upd); err != nil {
			t.Fatal(err)
		}
	}
	a := diagramArgs{Level: "overview", MaxNodes: 3}
	plans, err := planDiagrams(context.Background(), g, a, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 7 {
		t.Fatalf("expected overview + six details: %d", len(plans))
	}
	if plans[0].Projection.Collapsed != 10 {
		t.Fatalf("collapse: %+v", plans[0])
	}
	if plans[1].Title != "Module — module00" {
		t.Fatalf("unstable detail order: %+v", plans)
	}
	for _, p := range plans {
		if len(p.Projection.Nodes) > 3 {
			t.Fatalf("unbounded detail: %+v", p)
		}
	}
	a.MaxNodes = 40
	plans, err = planDiagrams(context.Background(), g, a, "")
	if err != nil || len(plans) != 1 {
		t.Fatalf("unnecessary split: %+v %v", plans, err)
	}
}
