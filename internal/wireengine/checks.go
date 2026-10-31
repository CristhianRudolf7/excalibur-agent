package wireengine

import (
	"sort"

	"github.com/CristhianRudolf7/excalibur-agent/internal/codeintel"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// ImpactedChecks selects the checks to run during the work from the
// changed files, the in-progress criteria and the code graph
// (Sections 10.10, 18.2): the graph expands the changed set with the
// test files the changes impact, then the ladder selection runs the
// checks whose criterion impact area covers a changed or impacted
// file plus the checks of the criteria in progress. A nil graph
// degrades to the pure impact-area selection.
func ImpactedChecks(all []tasks.CriterionChecks, changedFiles []string, inProgress map[types.CriterionID]bool, g *codeintel.Graph) []types.Check {
	changed := append([]string(nil), changedFiles...)
	if g != nil && len(changedFiles) > 0 {
		changed = append(changed, g.ImpactedTests(changedFiles).Files()...)
	}
	return tasks.SelectDuringWork(all, changed, inProgress)
}

// ImpactedTestFiles returns the graph-selected test files for changed,
// ordered, for evidence and for the integration queue (Section 16.4
// step 3: the queue runs the impacted tests before fast-forwarding).
func ImpactedTestFiles(g *codeintel.Graph, changed []string) []string {
	if g == nil || len(changed) == 0 {
		return nil
	}
	out := g.ImpactedTests(changed).Files()
	sort.Strings(out)
	return out
}

// BeforeCloseChecks returns the checks to run before closing: the full
// project test folder plus every criterion check (Section 18.2).
func BeforeCloseChecks(all []tasks.CriterionChecks, folder []types.Check) []types.Check {
	return tasks.SelectBeforeClose(all, folder)
}
