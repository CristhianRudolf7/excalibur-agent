package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func testRegistry(t *testing.T) (*Registry, types.ProjectID, types.ProjectID) {
	t.Helper()
	r := NewRegistry()
	base := t.TempDir()
	a := filepath.Join(base, "projA")
	b := filepath.Join(base, "projA", "sub")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	pa, err := r.Register(Project{Name: "Alpha", Alias: "a", Roots: []string{a}})
	if err != nil {
		t.Fatal(err)
	}
	pb, err := r.Register(Project{Name: "Beta", Roots: []string{b}})
	if err != nil {
		t.Fatal(err)
	}
	return r, pa.ID, pb.ID
}

// Section 9.6: files resolve by longest prefix with the candidate
// list; ties break by run project, then mentioned project.
func TestResolveNestedRoots(t *testing.T) {
	r, aID, bID := testRegistry(t)
	base := filepath.Join(os.TempDir())
	_ = base
	// A file directly under A resolves to A with both candidates.
	pa, _ := r.Get(aID)
	target := filepath.Join(pa.Roots[0], "sub", "main.go")
	res, err := r.Resolve(target, ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Chosen.ID != bID {
		t.Fatalf("want most specific project Beta, got %q", res.Chosen.Name)
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("want full candidate list of 2, got %d", len(res.Candidates))
	}
	// A file only under A resolves to A.
	res, err = r.Resolve(filepath.Join(pa.Roots[0], "top.go"), ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Chosen.ID != aID || len(res.Candidates) != 1 {
		t.Fatalf("want only Alpha, got %+v", res)
	}
	// Run-project tie-break wins over specificity.
	res, err = r.Resolve(target, ResolveOptions{RunProject: aID})
	if err != nil {
		t.Fatal(err)
	}
	if res.Chosen.ID != aID {
		t.Fatalf("want run project Alpha, got %q", res.Chosen.Name)
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("candidates must always be complete, got %d", len(res.Candidates))
	}
	// Mentioned-project tie-break.
	res, err = r.Resolve(target, ResolveOptions{MentionedProject: aID})
	if err != nil {
		t.Fatal(err)
	}
	if res.Chosen.ID != aID {
		t.Fatalf("want mentioned project Alpha, got %q", res.Chosen.Name)
	}
}

// Section 9.6: symlinked roots canonicalize to the same project.
func TestResolveSymlink(t *testing.T) {
	r := NewRegistry()
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := r.Register(Project{Name: "Sym", Roots: []string{real}})
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	res, err := r.Resolve(filepath.Join(link, "f.go"), ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Chosen.ID != p.ID {
		t.Fatalf("symlink did not canonicalize: got %q", res.Chosen.Name)
	}
}

// Section 9.6: unknown files are an error, not a silent default.
func TestResolveUnknownFile(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Resolve("/no/such/project/file.go", ResolveOptions{}); err == nil {
		t.Fatal("want error for file outside every project")
	}
}

func TestDuplicateNamesRejected(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Register(Project{Name: "A", Roots: []string{t.TempDir()}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Register(Project{Name: "a", Roots: []string{t.TempDir()}}); err == nil {
		t.Fatal("want duplicate name rejection")
	}
}

// Section 9.6: low-confidence inference followed by a write asks
// first; explicit commands switch; high confidence switches.
func TestDecideSwitch(t *testing.T) {
	id := types.NewProjectID()
	explicit := types.Scope{Kind: types.ScopeProject, ID: string(id)}
	d := DecideSwitch(&explicit, nil, true)
	if !d.Switch || d.Ask {
		t.Fatalf("explicit command must switch without asking: %+v", d)
	}
	d = DecideSwitch(nil, &Inference{Project: id, Confidence: 0.9}, false)
	if !d.Switch || d.Ask {
		t.Fatalf("high confidence must switch: %+v", d)
	}
	d = DecideSwitch(nil, &Inference{Project: id, Confidence: 0.3}, true)
	if !d.Ask || d.Switch {
		t.Fatalf("low confidence with write must ask: %+v", d)
	}
	d = DecideSwitch(nil, &Inference{Project: id, Confidence: 0.3}, false)
	if !d.Switch {
		t.Fatalf("low confidence read may switch for the turn: %+v", d)
	}
}

// Section 9.6: tagged answers never mix project lists silently.
func TestTagResults(t *testing.T) {
	out := RenderTagged(append(
		TagResults("A", []string{"x"}),
		TagResults("B", []string{"y"})...,
	))
	if out != "[A] x\n[B] y" {
		t.Fatalf("bad tagged render: %q", out)
	}
}

// Section 9.6: runs cannot read or write outside their scope set;
// transversal work needs an explicit dual scope.
func TestScopeEnforcement(t *testing.T) {
	run := DefaultReadScopes(types.Scope{Kind: types.ScopeProject, ID: "aaa"})
	other := types.Scope{Kind: types.ScopeProject, ID: "bbb"}
	if err := EnforceRead(run, other); err == nil {
		t.Fatal("cross-project read must be rejected")
	}
	if err := EnforceWrite(run, other); err == nil {
		t.Fatal("cross-project write must be rejected")
	}
	if err := EnforceRead(run, run[1]); err != nil {
		t.Fatalf("own project read must pass: %v", err)
	}
	if _, ok := EnforceRead(run, other).(*ScopeError); !ok {
		t.Fatal("rejection must be a *ScopeError")
	}
	dual := DualScope{A: run[1], B: other, Reason: "unify config"}
	if err := dual.Validate(); err != nil {
		t.Fatal(err)
	}
	set := dual.Scopes()
	if err := EnforceRead(set, other); err != nil {
		t.Fatalf("dual scope must allow B: %v", err)
	}
	if err := (DualScope{A: run[1], B: run[1]}).Validate(); err == nil {
		t.Fatal("dual scope needs two distinct scopes")
	}
}

// Section 9.6: facts without a clear scope wait as conversation
// candidates.
func TestCandidateScope(t *testing.T) {
	c := CandidateMemory{Statement: "likes tea", ConversationID: "cnv_x"}
	if got := ClassifyCandidate(c, nil).String(); got != "conversation:cnv_x" {
		t.Fatalf("want conversation holding scope, got %q", got)
	}
	target := types.Scope{Kind: types.ScopeProject, ID: "prj_x"}
	if got := ClassifyCandidate(c, &target); got != target {
		t.Fatalf("want project scope, got %v", got)
	}
}

func TestConversationScopes(t *testing.T) {
	var c ConversationScopes
	if got := c.Active("cnv_1"); got != types.GeneralScope() {
		t.Fatalf("default scope must be general, got %v", got)
	}
	s := types.Scope{Kind: types.ScopeProject, ID: "prj_1"}
	if err := c.SetActive("cnv_1", s); err != nil {
		t.Fatal(err)
	}
	if got := c.Active("cnv_1"); got != s {
		t.Fatalf("want %v, got %v", s, got)
	}
	if err := c.SetActive("cnv_1", types.Scope{Kind: "bogus", ID: "x"}); err == nil {
		t.Fatal("bad scope must be rejected")
	}
}
