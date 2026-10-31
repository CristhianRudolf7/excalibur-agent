package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// SuiteID names one of the Section 23.5 suites.
type SuiteID string

// Suite registry: what each suite measures and how it runs.
const (
	SuiteMemScope      SuiteID = "mem-scope"
	SuiteMemTime       SuiteID = "mem-time"
	SuiteMemConflict   SuiteID = "mem-conflict"
	SuiteMemCite       SuiteID = "mem-cite"
	SuiteSteer         SuiteID = "steer"
	SuiteAvail         SuiteID = "avail"
	SuiteControl       SuiteID = "control"
	SuiteNotify        SuiteID = "notify"
	SuiteCrash         SuiteID = "crash"
	SuiteConcurrency   SuiteID = "concurrency"
	SuiteCompaction    SuiteID = "compaction"
	SuiteCache         SuiteID = "cache"
	SuiteToolRetrieval SuiteID = "tool-retrieval"
	SuiteSkillsAB      SuiteID = "skills-ab"
	SuiteTerminalTools SuiteID = "terminal-vs-tools"
	SuiteFlowSim       SuiteID = "flow-sim"
	SuiteVision        SuiteID = "vision"
)

// Suite describes one own-suite entry.
type Suite struct {
	ID       SuiteID `json:"id"`
	Measures string  `json:"measures"`
	Method   string  `json:"method"`
	// Fixtures lists the deterministic fixture names materialized by
	// Prepare; FixtureHash pins them (Section 23.8).
	Fixtures []string `json:"fixtures"`
}

// Registry lists all 17 suites of Section 23.5 in stable order.
func Registry() []Suite {
	return []Suite{
		{SuiteMemScope, "Scope leaks", "Memories planted in project A; queries from B; any hit is a failure", []string{"projects-a-b", "memories-planted"}},
		{SuiteMemTime, "Temporality and expiry", "Facts that change and expire; questions about now vs before", []string{"timeline", "expiry-rules"}},
		{SuiteMemConflict, "Contradictions", "Contradictory statements; measured whether they are flagged or resolved", []string{"conflict-pairs"}},
		{SuiteMemCite, "Exact citations", "Questions about documents; page and paragraph checked against the original", []string{"documents", "questions"}},
		{SuiteSteer, "Redirection", "Corrections injected at random points; adherence and latency until applied", []string{"injection-points"}},
		{SuiteAvail, "Master availability", "p50/p95 of intake confirm, fast lane and master reply with 0/4/8 background runs, saturated CPU and fallen provider", []string{"load-levels"}},
		{SuiteControl, "Control orders", "Time from pause/cancel to zero resource use per tool-cancel class; target resolution correctness", []string{"control-scripts"}},
		{SuiteNotify, "Notices", "Urgent and normal delivery over the right channel, no duplicates, origin client on/offline", []string{"notice-scripts"}},
		{SuiteCrash, "Recovery", "kill -9 at random journal points; no duplicate effects and correct resume", []string{"crash-points"}},
		{SuiteConcurrency, "Parallel edits", "Two agents on one file; integration with worktrees", []string{"shared-file"}},
		{SuiteCompaction, "Fidelity", "Questions after compaction; artifact trace accuracy", []string{"compaction-corpus"}},
		{SuiteCache, "Cache efficiency", "Real hit rate vs the layer-structure theory", []string{"cache-workload"}},
		{SuiteToolRetrieval, "Deferred tools", "search_tools and decider accuracy", []string{"tool-queries"}},
		{SuiteSkillsAB, "Skill value", "Tasks with and without each skill, per model", []string{"skill-tasks"}},
		{SuiteTerminalTools, "Specific tools value", "Same tasks and model, terminal only vs terminal plus specific tools: success, tokens, time, errors", []string{"terminal-tasks"}},
		{SuiteFlowSim, "Flow simulation", "Flows with planted errors; coverage and detection", []string{"flows"}},
		{SuiteVision, "Vision model", "Cost, latency and extraction accuracy over captures and documents by resolution and crop", []string{"vision-corpus"}},
	}
}

// ByID returns the suite or an error for unknown ids.
func ByID(id SuiteID) (Suite, error) {
	for _, s := range Registry() {
		if s.ID == id {
			return s, nil
		}
	}
	return Suite{}, fmt.Errorf("eval: unknown suite %q", id)
}

// FixtureHash deterministically hashes a suite's fixture names plus
// their content map, for the Section 23.8 deterministic-setup check.
func FixtureHash(s Suite, contents map[string]string) string {
	keys := append([]string(nil), s.Fixtures...)
	sort.Strings(keys)
	h := sha256.New()
	fmt.Fprintf(h, "suite=%s\n", s.ID)
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%s\n", k, contents[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}
