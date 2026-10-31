// Evidence freshness with code version (Sections 8.3, 8.6).
//
// A criterion state is computed, never written by the model:
//
//	estado(criterio):
//	  e = latest evidence for the criterion
//	  without e: unverified
//	  without fresh(e): unverified (stale)
//	  with e.result = pass: satisfied
//	  else: failed
//
//	fresh(e):
//	  for every path r in the impact area:
//	    when current_fingerprint(r) != e.fingerprints[r]: false
//	  when the applicable instruction revision changed since e: false
//	  else true
//
// Fingerprints come from git when the project uses it (tree hash of
// the paths, very cheap) or from the local edit history. A reverse
// index path -> criteria marks the affected evidence stale at the
// moment of each edit, without walking the whole graph. Every
// evidence records the code version it ran on: the commit plus, for a
// dirty tree, the hash of the uncommitted changes (Section 8.6).
package tasks

import (
	"sort"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// CodeVersion is the version of the code an evidence ran on: the
// commit plus, for a dirty tree, the hash of the uncommitted changes
// (Section 8.6).
type CodeVersion struct {
	// Commit is the VCS commit, or "" when the project uses no VCS.
	Commit string
	// Dirty is the hash of uncommitted changes, "" on a clean tree.
	Dirty string
}

// String renders the compact form used in evidence rows and in the
// tests-folder checklist, e.g. "3f2a1c9" or "3f2a1c9+ab12cd".
func (v CodeVersion) String() string {
	if v.Dirty == "" {
		return v.Commit
	}
	if v.Commit == "" {
		return "dirty:" + v.Dirty
	}
	return v.Commit + "+" + v.Dirty
}

// ParseCodeVersion parses the compact form back.
func ParseCodeVersion(s string) CodeVersion {
	if i := strings.Index(s, "+"); i >= 0 {
		return CodeVersion{Commit: s[:i], Dirty: s[i+1:]}
	}
	if rest, ok := strings.CutPrefix(s, "dirty:"); ok {
		return CodeVersion{Dirty: rest}
	}
	return CodeVersion{Commit: s}
}

// Equal reports whether two code versions name the same tree state.
func (v CodeVersion) Equal(o CodeVersion) bool {
	return v.Commit == o.Commit && v.Dirty == o.Dirty
}

// IsStale reports whether evidence taken on recorded is stale under
// the current tree state.
func (v CodeVersion) IsStale(recorded CodeVersion) bool {
	return !v.Equal(recorded)
}

// FreshInput is everything freshness needs for one evidence.
type FreshInput struct {
	// Record is the latest evidence with its instruction revision.
	Record EvidenceRecord
	// ImpactArea lists the criterion impact-area paths.
	ImpactArea []string
	// Current holds path -> current fingerprint.
	Current map[string]string
	// InstructionRev is the current applicable revision.
	InstructionRev int
}

// IsFresh implements fresh(e) of Section 8.3.
func IsFresh(in FreshInput) bool {
	for _, r := range in.ImpactArea {
		cur, ok := in.Current[r]
		if !ok {
			// A path with no recorded fingerprint cannot prove the
			// tree unchanged; only a recorded mismatch stales.
			// Missing on both sides stays fresh.
			if _, recorded := in.Record.Evidence.Fingerprints[r]; recorded {
				return false
			}
			continue
		}
		if rec, recorded := in.Record.Evidence.Fingerprints[r]; !recorded || rec != cur {
			return false
		}
	}
	return in.Record.InstructionRevSeen >= in.InstructionRev
}

// CriterionStatus is the computed state of one criterion with the
// reason the gate and the three queries need.
type CriterionStatus struct {
	Criterion types.Criterion
	State     types.CriterionState
	// Stale is true when evidence exists but is outdated.
	Stale bool
	// HasEvidence is true when at least one evidence exists.
	HasEvidence bool
}

// ComputeStatus resolves one criterion from its latest evidence
// (nil when none), its impact area, current fingerprints and the
// current instruction revision.
func ComputeStatus(c types.Criterion, latest *EvidenceRecord, current map[string]string, instrRev int) CriterionStatus {
	st := CriterionStatus{Criterion: c, State: types.CriterionUnverified}
	if c.State == types.CriterionNotApplicable {
		// Not-applicable with justification is terminal (Section
		// 7.5); the store keeps the decided state directly.
		st.State = types.CriterionNotApplicable
		return st
	}
	if latest == nil {
		return st
	}
	st.HasEvidence = true
	if !IsFresh(FreshInput{
		Record:         *latest,
		ImpactArea:     c.ImpactArea,
		Current:        current,
		InstructionRev: instrRev,
	}) {
		st.Stale = true
		return st
	}
	if latest.Evidence.Result == types.EvidencePass {
		st.State = types.CriterionSatisfied
	} else {
		st.State = types.CriterionFailed
	}
	return st
}

// LatestForCriterion returns the newest evidence across all checks
// of a criterion: the "ultima evidencia del criterio" of Section
// 8.3, used both for status computation and for the per-criterion
// pass-to-fail history (Section 8.6).
func LatestForCriterion(store PlanningStore, c types.CriterionID) (EvidenceRecord, bool) {
	var best *EvidenceRecord
	for _, chk := range store.ListChecks(c) {
		if rec, ok := store.LatestEvidence(chk.ID); ok {
			r := rec
			if best == nil || r.Evidence.CreatedAt.After(best.Evidence.CreatedAt) {
				best = &r
			}
		}
	}
	if best == nil {
		return EvidenceRecord{}, false
	}
	return *best, true
}

// CriterionQuery is one of the three freshness queries of Section
// 8.3: criteria without a check, failed, or with stale evidence.
type CriterionQuery string

// The three queries.
const (
	QueryUnchecked CriterionQuery = "unchecked"
	QueryFailed    CriterionQuery = "failed"
	QueryStale     CriterionQuery = "stale"
)

// QueryCriteria runs one of the three queries over the requirements
// of a scope: it loads every criterion, resolves its status, and
// returns the matching ones in code order.
func QueryCriteria(store PlanningStore, scope types.Scope, q CriterionQuery) []CriterionStatus {
	var out []CriterionStatus
	for _, req := range store.ListRequirements(scope) {
		for _, c := range store.ListCriteria(req.ID) {
			latest, ok := LatestForCriterion(store, c.ID)
			var rec *EvidenceRecord
			if ok {
				rec = &latest
			}
			st := ComputeStatus(c, rec, currentPrints(store, c.ImpactArea), store.InstructionRev(scope))
			switch q {
			case QueryUnchecked:
				if !st.HasEvidence && st.State != types.CriterionNotApplicable {
					out = append(out, st)
				}
			case QueryFailed:
				if st.State == types.CriterionFailed {
					out = append(out, st)
				}
			case QueryStale:
				if st.Stale {
					out = append(out, st)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Criterion.Code.String() < out[j].Criterion.Code.String()
	})
	return out
}

// CriteriaAffectedByEdit returns the criteria whose impact area
// covers path, using the reverse index path -> criteria. The edit
// pipeline calls it at the moment of each edit to mark the affected
// evidence stale without walking the whole graph (Section 8.3).
func CriteriaAffectedByEdit(store PlanningStore, scope types.Scope, path string) []types.Criterion {
	var out []types.Criterion
	for _, req := range store.ListRequirements(scope) {
		for _, c := range store.ListCriteria(req.ID) {
			for _, p := range c.ImpactArea {
				if p == path || strings.HasPrefix(path, p+"/") || strings.HasPrefix(p, path+"/") {
					out = append(out, c)
					break
				}
			}
		}
	}
	return out
}

func currentPrints(store PlanningStore, paths []string) map[string]string {
	cur := make(map[string]string, len(paths))
	for _, p := range paths {
		if fp, ok := store.FileFingerprint(p); ok {
			cur[p] = fp
		}
	}
	return cur
}
