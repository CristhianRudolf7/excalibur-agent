// Tests-folder checklist projection (Section 8.8).
//
// In software projects the harness keeps a tests folder or scripts
// (the project convention, or tests/ when none exists) with a
// README.md holding the checklist. The README is a projection of the
// database, not a plan: the harness rewrites only the section
// delimited by markers every time the evidence changes, one line per
// check:
//
//   - [x] test_login_ok — passed · 2026-10-28 14:02 · commit 3f2a1c9
//   - [ ] test_export_pdf — failed · 2026-10-28 14:05 · commit 3f2a1c9 · see evd_01JB…
//   - [ ] bench_startup — not run on the current version
//
// (Lines render in English: the repository is English-only per
// AGENTS.md.) The write goes through the same pipeline as any edit —
// lease, fingerprint and edit history — without touching anything
// outside the markers; a user edit inside the markers is conserved
// in the history and the section is regenerated. The completion gate
// requires every line run on the current version and passing. In the
// eval profile the folder and README live in the ephemeral Excalibur
// data directory, outside the evaluated tree, so the environment the
// verifier inspects never changes.
package tasks

import (
	"fmt"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Checklist markers delimit the generated section. Everything
// outside them is never touched.
const (
	ChecklistBegin = "<!-- excalibur:checklist -->"
	ChecklistEnd   = "<!-- excalibur:checklist:end -->"
)

// ChecklistLine is one rendered line: the check name, its outcome on
// the current code version, and the evidence pointer.
type ChecklistLine struct {
	Name       string
	Checked    bool
	Detail     string
	EvidenceID string
}

// EvalDir is the ephemeral data directory holding the checklist in
// the eval profile, outside the evaluated tree (Section 8.8).
const EvalDir = ".excalibur/eval"

// ChecklistPath returns the README path for a profile: the project
// tests folder normally, the ephemeral data dir in eval.
func ChecklistPath(projectRoot, testsDir string, eval bool) string {
	if eval {
		return EvalDir + "/tests/README.md"
	}
	dir := testsDir
	if dir == "" {
		dir = "tests"
	}
	return projectRoot + "/" + dir + "/README.md"
}

// ChecklistLineFor renders one line for a check with its latest
// evidence against the current code version.
func ChecklistLineFor(name string, latest *EvidenceRecord, current CodeVersion, at time.Time) ChecklistLine {
	line := ChecklistLine{Name: name}
	if latest == nil {
		line.Detail = "not run on the current version"
		return line
	}
	rec := *latest
	line.EvidenceID = string(rec.Evidence.ID)
	ts := rec.Evidence.CreatedAt.UTC().Format("2006-01-02 15:04")
	cv := ParseCodeVersion(rec.Evidence.CodeVersion)
	switch {
	case !current.Equal(cv):
		line.Detail = fmt.Sprintf("not run on the current version (last %s %s)", strings.ToLower(string(rec.Evidence.Result)), ts)
	case rec.Evidence.Result == types.EvidencePass:
		line.Checked = true
		line.Detail = fmt.Sprintf("passed · %s · commit %s", ts, cv.String())
	default:
		line.Detail = fmt.Sprintf("%s · %s · commit %s · see %s",
			strings.ToLower(string(rec.Evidence.Result)), ts, cv.String(), line.EvidenceID)
	}
	return line
}

// RenderChecklist renders the full checklist section body, one line
// per check, in stable name order.
func RenderChecklist(lines []ChecklistLine) string {
	var b strings.Builder
	for _, l := range lines {
		mark := " "
		if l.Checked {
			mark = "x"
		}
		fmt.Fprintf(&b, "- [%s] %s — %s\n", mark, l.Name, l.Detail)
	}
	return b.String()
}

// SpliceReadme replaces the delimited section with the rendered
// checklist, preserving everything outside the markers. When the
// README carries no markers, the section is appended at the end.
func SpliceReadme(readme, body string) string {
	start := strings.Index(readme, ChecklistBegin)
	end := strings.Index(readme, ChecklistEnd)
	section := ChecklistBegin + "\n" + strings.TrimSuffix(body, "\n") + "\n" + ChecklistEnd
	if start < 0 || end < 0 || end < start {
		trimmed := strings.TrimSuffix(readme, "\n")
		if trimmed == "" {
			return section + "\n"
		}
		return trimmed + "\n\n" + section + "\n"
	}
	return readme[:start] + section + readme[end+len(ChecklistEnd):]
}

// Editor is the file pipeline the checklist write goes through:
// lease, fingerprint and edit history (Sections 13.6, 13.7). The
// programming-tools milestone (H7) provides the real adapter; tests
// use a fake.
type Editor interface {
	// Read returns the current file content.
	Read(path string) (string, error)
	// WriteLeased writes content under a lease and records the edit
	// in the history with the given reason.
	WriteLeased(path, content, reason string) error
}

// RegenerateChecklist renders the checklist from the database and
// splices it into the README through the edit pipeline. A user edit
// inside the markers is conserved in the history (the pipeline
// records the pre-image) before the section is regenerated.
func RegenerateChecklist(store PlanningStore, ed Editor, path string, scope types.Scope, current CodeVersion, names map[types.CheckID]string, at time.Time) error {
	var lines []ChecklistLine
	for _, req := range store.ListRequirements(scope) {
		for _, c := range store.ListCriteria(req.ID) {
			for _, chk := range store.ListChecks(c.ID) {
				name := names[chk.ID]
				if name == "" {
					name = string(chk.ID)
				}
				latest, ok := store.LatestEvidence(chk.ID)
				var rec *EvidenceRecord
				if ok {
					r := latest
					rec = &r
				}
				lines = append(lines, ChecklistLineFor(name, rec, current, at))
			}
		}
	}
	body := RenderChecklist(lines)
	var readme string
	if ed != nil {
		cur, err := ed.Read(path)
		if err == nil {
			readme = cur
		}
	}
	next := SpliceReadme(readme, body)
	if ed == nil {
		return fmt.Errorf("tasks: no editor for checklist write at %q", path)
	}
	return ed.WriteLeased(path, next, "checklist regenerate")
}
