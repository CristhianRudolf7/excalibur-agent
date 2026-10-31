package agenda

import (
	"fmt"
	"strings"
)

// Built-in durable workflows (21.7). Each is constructed from the 21.4
// node types, ships with version-1 tests, and can be copied and
// adapted. Structural invariants are checked by ConfirmBeforeSend and
// BoundedRounds below.

// Built-in workflow IDs (21.7).
const (
	BuiltinDeepResearch = "deep-research-run"
	BuiltinDebate       = "decision-debate"
	BuiltinJobPipeline  = "job-application-pipeline"
	BuiltinMonitor      = "information-monitor"
	BuiltinCourse       = "course-builder"
	BuiltinSession      = "project-session"
)

// BuiltinNames lists the six integrated workflows (21.7).
func BuiltinNames() []string {
	return []string{
		BuiltinDeepResearch,
		BuiltinDebate,
		BuiltinJobPipeline,
		BuiltinMonitor,
		BuiltinCourse,
		BuiltinSession,
	}
}

// Builtin returns the workflow definition for a built-in name.
func Builtin(name string) (*WorkflowDef, bool) {
	switch name {
	case BuiltinDeepResearch:
		return deepResearchRun()
	case BuiltinDebate:
		return decisionDebate()
	case BuiltinJobPipeline:
		return jobApplicationPipeline()
	case BuiltinMonitor:
		return informationMonitor()
	case BuiltinCourse:
		return courseBuilder()
	case BuiltinSession:
		return projectSession()
	}
	return nil, false
}

// AllBuiltins returns every built-in definition, validated.
func AllBuiltins() ([]*WorkflowDef, error) {
	var out []*WorkflowDef
	for _, name := range BuiltinNames() {
		d, ok := Builtin(name)
		if !ok {
			return nil, fmt.Errorf("agenda: missing builtin %q", name)
		}
		if err := d.Validate(); err != nil {
			return nil, fmt.Errorf("agenda: builtin %q invalid: %w", name, err)
		}
		out = append(out, d)
	}
	return out, nil
}

// deepResearchRun is the six-step deep research workflow (21.7):
// questions, parallel bounded research rounds, evidence table,
// contrast, gap-driven extra rounds, report with citations.
func deepResearchRun() (*WorkflowDef, bool) {
	d := &WorkflowDef{
		ID: BuiltinDeepResearch, Name: "Deep research", Version: 1,
		Description: "Six-step investigation with bounded research rounds and cited report.",
		Nodes: []Node{
			{ID: "frame", Type: NodeAgent, Name: "Frame questions and criteria", AgentBrief: "Turn the request into research questions and acceptance criteria."},
			{ID: "rounds", Type: NodeLoop, Name: "Research rounds", Body: []string{"investigate", "evidence", "contrast"}, MaxIterations: 3, Condition: "has_gaps == true"},
			{ID: "investigate", Type: NodeAgent, Name: "Parallel investigators", AgentBrief: "Search and read sources within the round budget."},
			{ID: "evidence", Type: NodeTool, Name: "Evidence table", Tool: "evidence_table"},
			{ID: "contrast", Type: NodeAgent, Name: "Contrast and contradictions", AgentBrief: "Contrast findings, flag contradictions and gaps."},
			{ID: "report", Type: NodeAgent, Name: "Cited report", AgentBrief: "Write the report with citations, conclusions and evidence limitations. Cite only sources actually read."},
		},
		Edges: []Edge{
			{From: "frame", To: "rounds"},
			{From: "rounds", To: "report"},
		},
		Tests: []WorkflowTest{
			{Name: "report cites read sources", Inputs: map[string]any{"topic": "grids"},
				Mocks:    map[string]any{"evidence": map[string]any{"has_gaps": false}, "report": map[string]any{"citations": []string{"https://a.example"}}},
				Expected: map[string]any{"report.citations": "[https://a.example]"}},
		},
	}
	return d, true
}

// decisionDebate is the multi-agent decision workflow (21.7) with a
// bounded number of discussion rounds. Evidence outweighs consensus:
// a claim without evidence does not count.
func decisionDebate() (*WorkflowDef, bool) {
	d := &WorkflowDef{
		ID: BuiltinDebate, Name: "Decision debate", Version: 1,
		Description: "Multi-agent investigation and decision with bounded debate rounds.",
		Nodes: []Node{
			{ID: "define", Type: NodeAgent, Name: "Define decision", AgentBrief: "Define the decision, criteria and constraints."},
			{ID: "research", Type: NodeParallel, Name: "Specialist research", Branches: []string{"spec_a", "spec_b"}},
			{ID: "spec_a", Type: NodeAgent, Name: "Specialist A", AgentBrief: "Investigate the proposal case with evidence."},
			{ID: "spec_b", Type: NodeAgent, Name: "Specialist B", AgentBrief: "Investigate the counter case with evidence."},
			{ID: "compare", Type: NodeAgent, Name: "Compare alternatives", AgentBrief: "Compare alternatives on equal terms with tests and evidence."},
			{ID: "debate", Type: NodeLoop, Name: "Debate rounds", Body: []string{"argue", "rebut"}, MaxIterations: 3, Condition: "unresolved == true"},
			{ID: "argue", Type: NodeAgent, Name: "Propose", AgentBrief: "Defend a proposal with evidence."},
			{ID: "rebut", Type: NodeAgent, Name: "Challenge", AgentBrief: "Attack proposals and seek counter-evidence."},
			{ID: "recommend", Type: NodeAgent, Name: "Recommend", AgentBrief: "Recommend with sources, uncertainties and pending tests, or declare insufficient evidence."},
		},
		Edges: []Edge{
			{From: "define", To: "research"},
			{From: "research", To: "compare"},
			{From: "compare", To: "debate"},
			{From: "debate", To: "recommend"},
		},
		Tests: []WorkflowTest{
			{Name: "bounded debate", Inputs: map[string]any{"decision": "queue"},
				Mocks:    map[string]any{"recommend": map[string]any{"verdict": "insufficient evidence"}},
				Expected: map[string]any{"recommend.verdict": "insufficient evidence"}},
		},
	}
	return d, true
}

// jobApplicationPipeline is the job-search workflow (21.7). The send
// step runs only after an explicit user-confirmation node, and review
// rounds are bounded.
func jobApplicationPipeline() (*WorkflowDef, bool) {
	d := &WorkflowDef{
		ID: BuiltinJobPipeline, Name: "Job application pipeline", Version: 1,
		Description: "Profile, search, prepare with bounded review, confirm, then send.",
		Nodes: []Node{
			{ID: "profile", Type: NodeAgent, Name: "Candidate profile", AgentBrief: "Confirm CV, experience, roles, location, salary and hard constraints without inventing experience."},
			{ID: "search", Type: NodeTool, Name: "Search offers", Tool: "job_search"},
			{ID: "prepare", Type: NodeLoop, Name: "Prepare and review", Body: []string{"adapt", "check"}, MaxIterations: 3, Condition: "needs_changes == true"},
			{ID: "adapt", Type: NodeAgent, Name: "Adapt CV and letter", AgentBrief: "Adapt CV, letter and answers without inventing experience; generate PDFs."},
			{ID: "check", Type: NodeAgent, Name: "Coherence check", AgentBrief: "Check coherence and fidelity against the real profile."},
			{ID: "confirm_send", Type: NodeConfirm, Name: "Confirm before sending", ConfirmPrompt: "Send this application now? Nothing is sent without your explicit confirmation."},
			{ID: "send", Type: NodeTool, Name: "Send application", Tool: "job_send", Effects: true, Compensation: "job_withdraw",
				DryRun: &DryRunSpec{Validate: []string{"cv"}, Simulated: map[string]any{"sent": false, "dry_run": true}}},
			{ID: "followup", Type: NodeAgent, Name: "Follow up", AgentBrief: "Track outcomes, prepare interviews and reminders, learn from rejections."},
		},
		Edges: []Edge{
			{From: "profile", To: "search"},
			{From: "search", To: "prepare"},
			{From: "prepare", To: "confirm_send"},
			{From: "confirm_send", To: "send"},
			{From: "send", To: "followup"},
		},
		Tests: []WorkflowTest{
			{Name: "dry run sends nothing", Inputs: DryRunScope(map[string]any{"cv": "cv.pdf"}),
				Mocks:    map[string]any{},
				Expected: map[string]any{}},
			{Name: "confirmation path completes", Inputs: map[string]any{"cv": "cv.pdf"},
				Mocks: map[string]any{"send": map[string]any{"sent": true}}, ExpectAborted: false},
		},
	}
	return d, true
}

// informationMonitor polls configured sources, deduplicates,
// prioritizes and alerts with provenance (21.7).
func informationMonitor() (*WorkflowDef, bool) {
	d := &WorkflowDef{
		ID: BuiltinMonitor, Name: "Information monitor", Version: 1,
		Description: "Poll, dedupe, prioritize and alert with source and date.",
		Nodes: []Node{
			{ID: "poll", Type: NodeTool, Name: "Poll sources", Tool: "source_poll"},
			{ID: "dedupe", Type: NodeTool, Name: "Deduplicate", Tool: "news_dedupe"},
			{ID: "prioritize", Type: NodeAgent, Name: "Prioritize", AgentBrief: "Prioritize by learned preferences."},
			{ID: "alert_now", Type: NodeCondition, Name: "Alert?", Condition: "priority == \"high\""},
			{ID: "alert", Type: NodeTool, Name: "Alert with provenance", Tool: "notify", Effects: true,
				DryRun: &DryRunSpec{Validate: []string{"items"}, Simulated: map[string]any{"alerted": false}}},
			{ID: "digest", Type: NodeTool, Name: "Update monitor digest", Tool: "monitor_store"},
		},
		Edges: []Edge{
			{From: "poll", To: "dedupe"},
			{From: "dedupe", To: "prioritize"},
			{From: "prioritize", To: "alert_now"},
			{From: "alert_now", To: "alert", When: "priority == \"high\""},
			{From: "alert_now", To: "digest", When: "priority != \"high\""},
			{From: "alert", To: "digest"},
		},
		Tests: []WorkflowTest{
			{Name: "low priority skips alert", Inputs: map[string]any{"priority": "low"},
				Mocks:    map[string]any{"prioritize": map[string]any{"priority": "low"}},
				Expected: map[string]any{"digest.mock": "digest"}},
		},
	}
	return d, true
}

// courseBuilder creates a course: goals, syllabus, lessons and quizzes
// (21.7).
func courseBuilder() (*WorkflowDef, bool) {
	d := &WorkflowDef{
		ID: BuiltinCourse, Name: "Course builder", Version: 1,
		Description: "Clarify goals, draft syllabus, prepare lessons and quizzes, record progress.",
		Nodes: []Node{
			{ID: "goals", Type: NodeAgent, Name: "Clarify goals", AgentBrief: "Clarify objective, scope and level."},
			{ID: "syllabus", Type: NodeAgent, Name: "Full syllabus", AgentBrief: "Draft the complete syllabus."},
			{ID: "lessons", Type: NodeLoop, Name: "Prepare lessons", Body: []string{"lesson"}, MaxIterations: 20, Condition: "more_lessons == true"},
			{ID: "lesson", Type: NodeAgent, Name: "One lesson", AgentBrief: "Prepare one lesson with its quizzes in the right format."},
			{ID: "record", Type: NodeTool, Name: "Record progress", Tool: "classroom_record"},
		},
		Edges: []Edge{
			{From: "goals", To: "syllabus"},
			{From: "syllabus", To: "lessons"},
			{From: "lessons", To: "record"},
		},
		Tests: []WorkflowTest{
			{Name: "single lesson records", Inputs: map[string]any{"more_lessons": false},
				Mocks: map[string]any{}, Expected: map[string]any{}},
		},
	}
	return d, true
}

// projectSession is one work session of a long initiative (17.6/21.7):
// compile a fresh context from the DB, verify the starting state,
// advance the milestone within budget, leave state for the next one.
func projectSession() (*WorkflowDef, bool) {
	d := &WorkflowDef{
		ID: BuiltinSession, Name: "Project session", Version: 1,
		Description: "One session of a long initiative: fresh context, verify, advance, hand off.",
		Nodes: []Node{
			{ID: "context", Type: NodeTool, Name: "Compile fresh context", Tool: "context_compile"},
			{ID: "verify", Type: NodeTool, Name: "Verify starting state", Tool: "state_verify"},
			{ID: "healthy", Type: NodeCondition, Name: "State healthy?", Condition: "regression == false"},
			{ID: "advance", Type: NodeLoop, Name: "Advance milestone", Body: []string{"work"}, MaxIterations: 50, Condition: "milestone_done == false"},
			{ID: "work", Type: NodeAgent, Name: "Work", AgentBrief: "Advance the milestone within the session budget and window."},
			{ID: "report_regression", Type: NodeAgent, Name: "Report regression", AgentBrief: "Report the planted or found regression with evidence and stop."},
			{ID: "handoff", Type: NodeAgent, Name: "Handoff notes", AgentBrief: "Leave plan state, evidence and continuation notes for the next session."},
		},
		Edges: []Edge{
			{From: "context", To: "verify"},
			{From: "verify", To: "healthy"},
			{From: "healthy", To: "advance", When: "regression == false"},
			{From: "healthy", To: "report_regression", When: "regression == true"},
			{From: "advance", To: "handoff"},
		},
		Tests: []WorkflowTest{
			{Name: "clean session advances", Inputs: map[string]any{"regression": false, "milestone_done": true},
				Mocks: map[string]any{}, Expected: map[string]any{}},
		},
	}
	return d, true
}

// ConfirmBeforeSend checks the 21.4/21.7 invariant: every effectful
// send-like node must be dominated by a user-confirmation node on all
// paths from the entry. It returns the violating node IDs.
func ConfirmBeforeSend(d *WorkflowDef) []string {
	entry, err := d.EntryNode()
	if err != nil {
		return []string{"<no-entry>"}
	}
	var bad []string
	for i := range d.Nodes {
		n := &d.Nodes[i]
		if !n.Effects || !isSendLike(n) {
			continue
		}
		if !dominatedByConfirm(d, entry.ID, n.ID) {
			bad = append(bad, n.ID)
		}
	}
	return bad
}

func isSendLike(n *Node) bool {
	t := strings.ToLower(n.Tool + " " + n.Name + " " + n.ID)
	return strings.Contains(t, "send") || strings.Contains(t, "submit") || strings.Contains(t, "apply") || strings.Contains(t, "post")
}

// dominatedByConfirm reports whether every entry-to-target path passes
// a confirm node.
func dominatedByConfirm(d *WorkflowDef, entry, target string) bool {
	adj := map[string][]string{}
	for _, e := range d.Edges {
		adj[e.From] = append(adj[e.From], e.To)
	}
	// DFS from entry avoiding confirm nodes: reaching target means a
	// path exists with no confirmation.
	seen := map[string]bool{}
	var reaches func(id string) bool
	reaches = func(id string) bool {
		if id == target {
			return true
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		n, ok := d.NodeByID(id)
		if ok && n.Type == NodeConfirm {
			return false
		}
		for _, nx := range adj[id] {
			if reaches(nx) {
				return true
			}
		}
		return false
	}
	return !reaches(entry)
}

// BoundedRounds returns loop nodes without an explicit bound
// (21.4/21.7: every loop is bounded, every debate and review caps its
// rounds). Empty means the invariant holds; Validate already rejects
// unbounded loops, so this is a second checker for review tooling.
func BoundedRounds(d *WorkflowDef) []string {
	var bad []string
	for _, n := range d.Nodes {
		if n.Type == NodeLoop && n.MaxIterations < 1 {
			bad = append(bad, n.ID)
		}
	}
	return bad
}

// VerifyCitedSources enforces the deep-research invariant (21.10): the
// report cites only sources actually read.
func VerifyCitedSources(citations []string, read map[string]bool) []string {
	var unread []string
	for _, c := range citations {
		if !read[c] {
			unread = append(unread, c)
		}
	}
	return unread
}
