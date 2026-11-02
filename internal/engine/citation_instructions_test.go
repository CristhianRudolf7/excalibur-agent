package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

func TestBuilderCitationGuidanceOnlyWithLibraryTools(t *testing.T) {
	defs := []providers.ToolDefinition{{Name: "exec", Description: "Run"}, {Name: "library_search", Description: "Search"}}
	b := &DefaultBuilder{Config: ctxcompiler.DefaultConfig(), Snapshot: testSnapshotFunc(), ToolDefs: func(*Run) []providers.ToolDefinition { return defs }}
	run := testRun(Profile{})
	t.Setenv("EXCALIBUR_CITATIONS_PIPELINE", "on")
	req, _, err := b.BuildRequest(context.Background(), run, false)
	if err != nil {
		t.Fatal(err)
	}
	if req.Tools[0].Description != "Run" || !strings.Contains(req.Tools[1].Description, "exact [[doc:") || defs[1].Description != "Search" {
		t.Fatalf("guidance mutated unrelated or shared defs: %+v %+v", req.Tools, defs)
	}
	// Tool definitions are retained per run once sent (stable prompt prefix),
	// so the switch is observed by a new run.
	t.Setenv("EXCALIBUR_CITATIONS_PIPELINE", "off")
	req, _, err = b.BuildRequest(context.Background(), testRun(Profile{}), false)
	if err != nil {
		t.Fatal(err)
	}
	if req.Tools[1].Description != "Search" {
		t.Fatal("switch off changed request")
	}
}
