// Freshness tests (Sections 8.3, 8.6; verification row 8.11.2):
// editing a file in the impact area, or landing a new applicable
// instruction revision, stales the affected evidence on the spot,
// and the three queries (unchecked, failed, stale) return the right
// criteria.
package tasks

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func seedFreshCriterion(t *testing.T, store *MemoryStore, scope types.Scope, impact ...string) (types.Criterion, types.Check) {
	t.Helper()
	req := seedRequirement(store, scope, "export PDF")
	crit, chk := seedCriterion(store, req, req.Code.Number(), types.CriterionTest, impact...)
	return crit, chk
}

func TestEditStalesEvidenceImmediately(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()
	crit, chk := seedFreshCriterion(t, store, scope, "pdf/export.go")
	store.RecordFileEdit("pdf/export.go", "fp1")
	seedEvidence(store, chk, types.EvidencePass,
		CodeVersion{Commit: "abc"}, map[string]string{"pdf/export.go": "fp1"}, 3)
	store.SetInstructionRev(scope, 3)

	before := QueryCriteria(store, scope, QueryStale)
	if len(before) != 0 {
		t.Fatalf("fresh evidence listed as stale: %v", before)
	}

	// The edit lands: fingerprint moves.
	store.RecordFileEdit("pdf/export.go", "fp2")
	stale := QueryCriteria(store, scope, QueryStale)
	if len(stale) != 1 || stale[0].Criterion.ID != crit.ID {
		t.Fatalf("edit did not stale the evidence: %v", stale)
	}
	if stale[0].State != types.CriterionUnverified {
		t.Fatalf("stale evidence must read unverified, got %q", stale[0].State)
	}
	if len(QueryCriteria(store, scope, QueryUnchecked)) != 0 {
		t.Fatal("stale is not unchecked-without-evidence")
	}
}

func TestInstructionRevisionStalesEvidence(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()
	_, chk := seedFreshCriterion(t, store, scope, "pdf/export.go")
	store.RecordFileEdit("pdf/export.go", "fp1")
	seedEvidence(store, chk, types.EvidencePass,
		CodeVersion{Commit: "abc"}, map[string]string{"pdf/export.go": "fp1"}, 3)
	store.SetInstructionRev(scope, 3)
	store.SetInstructionRev(scope, 4) // applicable revision lands later

	stale := QueryCriteria(store, scope, QueryStale)
	if len(stale) != 1 {
		t.Fatalf("instruction bump did not stale the evidence: %v", stale)
	}
}

func TestThreeQueries(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()

	req := seedRequirement(store, scope, "req")
	cUnchecked, _ := seedCriterion(store, req, req.Code.Number(), types.CriterionTest, "u.go")
	cFailed, chkFailed := seedCriterion(store, req, req.Code.Number(), types.CriterionTest, "f.go")
	cStale, chkStale := seedCriterion(store, req, req.Code.Number(), types.CriterionTest, "s.go")

	store.RecordFileEdit("f.go", "fp")
	seedEvidence(store, chkFailed, types.EvidenceFail, CodeVersion{Commit: "c"}, map[string]string{"f.go": "fp"}, 0)
	store.RecordFileEdit("s.go", "fp1")
	seedEvidence(store, chkStale, types.EvidencePass, CodeVersion{Commit: "c"}, map[string]string{"s.go": "fp1"}, 0)
	store.RecordFileEdit("s.go", "fp2")

	unchecked := QueryCriteria(store, scope, QueryUnchecked)
	if len(unchecked) != 1 || unchecked[0].Criterion.ID != cUnchecked.ID {
		t.Fatalf("unchecked wrong: %v", unchecked)
	}
	failed := QueryCriteria(store, scope, QueryFailed)
	if len(failed) != 1 || failed[0].Criterion.ID != cFailed.ID {
		t.Fatalf("failed wrong: %v", failed)
	}
	stale := QueryCriteria(store, scope, QueryStale)
	if len(stale) != 1 || stale[0].Criterion.ID != cStale.ID {
		t.Fatalf("stale wrong: %v", stale)
	}
}

func TestCriteriaAffectedByEdit(t *testing.T) {
	store := NewMemoryStore()
	scope := seedScope()
	crit, _ := seedFreshCriterion(t, store, scope, "pdf/export.go")
	affected := CriteriaAffectedByEdit(store, scope, "pdf/export.go")
	if len(affected) != 1 || affected[0].ID != crit.ID {
		t.Fatalf("reverse index missed the edit: %v", sortedCodes(affected))
	}
	if len(CriteriaAffectedByEdit(store, scope, "unrelated.go")) != 0 {
		t.Fatal("unrelated edit must affect nothing")
	}
}

func TestCodeVersionRoundtrip(t *testing.T) {
	v := CodeVersion{Commit: "3f2a1c9", Dirty: "ab12cd"}
	if got := ParseCodeVersion(v.String()); !got.Equal(v) {
		t.Fatalf("roundtrip failed: %q", v.String())
	}
	cur := CodeVersion{Commit: "3f2a1c9", Dirty: "ff00"}
	if !cur.IsStale(CodeVersion{Commit: "3f2a1c9", Dirty: "ab12cd"}) {
		t.Fatal("dirty-tree change must stale the evidence")
	}
}
