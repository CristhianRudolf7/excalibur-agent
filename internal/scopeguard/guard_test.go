package scopeguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, p, s string) {
	t.Helper()
	if e := os.WriteFile(p, []byte(s), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestProtectedFile(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "keep.txt")
	put(t, p, "original")
	put(t, filepath.Join(root, "result.txt"), "result")
	g, e := Start(root, "Do not modify keep.txt; write result.txt")
	if e != nil {
		t.Fatal(e)
	}
	if s := g.Check(); s != "" {
		t.Fatal(s)
	}
	put(t, p, "changed")
	if s := g.Check(); !strings.Contains(s, p) {
		t.Fatal(s)
	}
	put(t, p, "original")
	if s := g.Check(); s != "" {
		t.Fatal(s)
	}
	if e = os.Remove(p); e != nil {
		t.Fatal(e)
	}
	if s := g.Check(); !strings.Contains(s, "deleted") {
		t.Fatal(s)
	}
}
func TestDatabaseProbePreservesCompanions(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "data.db")
	put(t, p, "database")
	put(t, p+"-wal", "uncheckpointed input")
	g, e := Start(root, "Recover data.db into result.json")
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		d := g.Backups[p]
		for filepath.Dir(d) != originalsDir() {
			if filepath.Dir(d) == d {
				t.Errorf("backup %q is outside originals directory %q", g.Backups[p], originalsDir())
				return
			}
			d = filepath.Dir(d)
		}
		_ = os.RemoveAll(d)
	}()
	if len(g.Backups) != 2 {
		t.Fatalf("backups: %v", g.Backups)
	}
	if e = os.Remove(p + "-wal"); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(g.Backups[p+"-wal"])
	if e != nil || string(b) != "uncheckpointed input" {
		t.Fatalf("%s %v", b, e)
	}
	if g.Backups[p]+"-wal" != g.Backups[p+"-wal"] {
		t.Fatal("companion layout lost")
	}
	if !strings.Contains(g.Note(), g.Backups[p]) {
		t.Fatal(g.Note())
	}
}
func TestFailedCheckExpectationTampering(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "check.py")
	put(t, p, "expected = 9\nassert result == expected\n")
	put(t, filepath.Join(root, "output.txt"), "output")
	g, e := Start(root, "Produce output.txt")
	if e != nil {
		t.Fatal(e)
	}
	g.Observe(true)
	put(t, p, "expected = 3\nassert result == expected\n")
	g.Observe(false)
	if s := g.Check(); !strings.Contains(s, "test-tampering") || !strings.Contains(s, p) {
		t.Fatal(s)
	}
}
func TestImplementationRepairAllowed(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "check.py")
	put(t, p, "result = 3\nexpected = 9\nassert result == expected\n")
	put(t, filepath.Join(root, "output.txt"), "output")
	g, e := Start(root, "Produce output.txt")
	if e != nil {
		t.Fatal(e)
	}
	g.Observe(true)
	put(t, p, "result = 9\nexpected = 9\nassert result == expected\n")
	g.Observe(false)
	if s := g.Check(); s != "" {
		t.Fatal(s)
	}
}

func TestOnlyLocationsAndCounts(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "keep.txt")
	put(t, keep, "keep")
	g, e := Start(root, "Only modify result.txt; write result.txt with exactly 2 lines")
	if e != nil {
		t.Fatal(e)
	}
	if s := g.Check(); !strings.Contains(s, "required file location") {
		t.Fatal(s)
	}
	put(t, filepath.Join(root, "result.txt"), "one\n")
	if s := g.Check(); !strings.Contains(s, "got 1") {
		t.Fatal(s)
	}
	put(t, filepath.Join(root, "result.txt"), "one\ntwo\n")
	if s := g.Check(); s != "" {
		t.Fatal(s)
	}
	put(t, keep, "oops")
	if s := g.Check(); !strings.Contains(s, "outside allowed") {
		t.Fatal(s)
	}
}
func TestInlineTamperingLegacy(t *testing.T) {
	t.Setenv("EXCALIBUR_SCOPE_PROVIDED_CHECKS", "0")
	g, e := Start(t.TempDir(), "Compute the result")
	if e != nil {
		t.Fatal(e)
	}
	g.ObserveCall("exec", `{"command":"python3 - <<'PY'\nexpected = 9\nassert actual == expected\nPY"}`, "AssertionError", true)
	g.ObserveCall("exec", `{"command":"python3 - <<'PY'\nexpected = 3\nassert actual == expected\nPY"}`, "", false)
	if s := g.Check(); !strings.Contains(s, "inline expected value") {
		t.Fatal(s)
	}
}

func TestAllAndProtectedDirectory(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "input.txt")
	put(t, p, "original")
	g, e := Start(root, "Do not change all files")
	if e != nil {
		t.Fatal(e)
	}
	put(t, p, "edited")
	if s := g.Check(); !strings.Contains(s, p) {
		t.Fatal(s)
	}
	dir := filepath.Join(root, "inputs")
	if e = os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	g, e = Start(root, "Leave "+dir+" untouched")
	if e != nil {
		t.Fatal(e)
	}
	put(t, filepath.Join(dir, "extra.txt"), "extra")
	if s := g.Check(); !strings.Contains(s, "gained") {
		t.Fatal(s)
	}
}

func TestResumeKeepsInitialSnapshot(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "keep.txt")
	put(t, p, "original")
	g, e := StartRun(root, "Do not change keep.txt", "resume-test")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(filepath.Dir(g.StatePath))
	put(t, p, "modified")
	restored, e := StartRun(root, "Do not change keep.txt", "resume-test")
	if e != nil {
		t.Fatal(e)
	}
	if s := restored.Check(); !strings.Contains(s, p) {
		t.Fatal("resume lost original baseline:", s)
	}
	if _, e = StartRun(root, "Different instruction", "resume-test"); e == nil {
		t.Fatal("silently replaced instruction")
	}
}

func TestShellFailurePersistsInlineOracleLegacy(t *testing.T) {
	t.Setenv("EXCALIBUR_SCOPE_PROVIDED_CHECKS", "0")
	root := t.TempDir()
	g, e := StartRun(root, "Compute a value", "inline-resume")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(filepath.Dir(g.StatePath))
	g.ObserveCall("exec", `{"command":"expected=9; assert actual == 9"}`, "exit: 1  duration: 0.1s", false)
	g, e = StartRun(root, "Compute a value", "inline-resume")
	if e != nil {
		t.Fatal(e)
	}
	g.ObserveCall("exec", `{"command":"expected=3; assert actual == 3"}`, "exit: 0", false)
	if s := g.Check(); !strings.Contains(s, "test-tampering") {
		t.Fatal(s)
	}
}

func TestNumericJSONThresholds(t *testing.T) {
	for _, tc := range []struct {
		request, data string
		bad           bool
	}{
		{"Write rows.json with at least 2 records", "[1]", true},
		{"Write rows.json with at least 2 records", "[1,2]", false},
		{"Write rows.json with at most 2 entries", "[1,2,3]", true},
		{"Write rows.json with exactly 2 rows", "[1,2]", false},
		{"Write rows.json with exactly 2 bytes", "[]", false},
	} {
		t.Run(tc.request+tc.data, func(t *testing.T) {
			root := t.TempDir()
			put(t, filepath.Join(root, "rows.json"), tc.data)
			g, e := Start(root, tc.request)
			if e != nil {
				t.Fatal(e)
			}
			if s := g.Check(); (s != "") != tc.bad {
				t.Fatal(s)
			}
		})
	}
}

func TestExtensionlessAndDeletionConstraint(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "INPUT")
	put(t, p, "original")
	g, e := Start(root, "Do not modify INPUT")
	if e != nil {
		t.Fatal(e)
	}
	put(t, p, "changed")
	if s := g.Check(); !strings.Contains(s, p) {
		t.Fatal(s)
	}
	g, e = Start(root, "Do not delete INPUT")
	if e != nil {
		t.Fatal(e)
	}
	put(t, p, "allowed edit")
	if s := g.Check(); s != "" {
		t.Fatal(s)
	}
	if e = os.Remove(p); e != nil {
		t.Fatal(e)
	}
	if s := g.Check(); !strings.Contains(s, "deleted") {
		t.Fatal(s)
	}
}

func TestGitIndexScopeViolation(t *testing.T) {
	if _, e := exec.LookPath("git"); e != nil {
		t.Skip("git unavailable")
	}
	root := t.TempDir()
	p := filepath.Join(root, "keep.txt")
	put(t, p, "original")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("git: %v %s", e, b)
		}
	}
	git("init", "-q")
	git("add", "keep.txt")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture")
	g, e := Start(root, "Do not modify keep.txt")
	if e != nil {
		t.Fatal(e)
	}
	git("rm", "--cached", "keep.txt")
	if s := g.Check(); !strings.Contains(s, "git diff changed") {
		t.Fatal(s)
	}
}

func TestFailedTestsDoNotFreezeImplementationExpectations(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "implementation.py")
	put(t, source, "expected = 1\n")
	put(t, filepath.Join(root, "test_values.py"), "assert value == 7\n")
	g, e := Start(root, "Fix implementation.py")
	if e != nil {
		t.Fatal(e)
	}
	g.ObserveCall("exec", `{"command":"pytest test_values.py"}`, "exit: 1", false)
	put(t, source, "expected = 7\n")
	g.ObserveCall("exec", `{"command":"pytest test_values.py"}`, "exit: 0", false)
	if s := g.Check(); s != "" {
		t.Fatal(s)
	}
}

func TestOnlyRuleWithExternalInput(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "output.txt"), "output")
	external := filepath.Join(t.TempDir(), "input.bin")
	put(t, external, "data")
	g, e := Start(root, "Only modify output.txt; read "+external)
	if e != nil {
		t.Fatal(e)
	}
	if s := g.Check(); s != "" {
		t.Fatal(s)
	}
}

func TestExtensionlessBinaryPreserved(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "program")
	put(t, p, "\x7fELF\x00fixture")
	g, e := Start(root, "Inspect program and report its header")
	if e != nil {
		t.Fatal(e)
	}
	if g.Backups[p] == "" {
		t.Fatal("extensionless binary not preserved")
	}
}
func TestEmptyProtectedDirectoryDeletion(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "inputs")
	if e := os.Mkdir(p, 0700); e != nil {
		t.Fatal(e)
	}
	g, e := Start(root, "Leave "+p+" untouched")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(p); e != nil {
		t.Fatal(e)
	}
	if s := g.Check(); !strings.Contains(s, "directory removed") {
		t.Fatal(s)
	}
}

func TestFilesystemRootIsNotAWorkingTree(t *testing.T) {
	root := filepath.VolumeName(t.TempDir()) + string(filepath.Separator)
	g, e := Start(root, "say hi")
	if e != nil {
		t.Fatal(e)
	}
	if len(g.Initial) != 0 {
		t.Fatalf("walked system root: %d files", len(g.Initial))
	}
	if _, e = Start(root, "Only modify result.txt"); e == nil {
		t.Fatal("tree-wide root scope must require a project")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "input.txt")
	put(t, p, "initial")
	g, e = Start(root, "Leave "+dir+" untouched")
	if e != nil {
		t.Fatal(e)
	}
	put(t, p, "changed")
	if s := g.Check(); !strings.Contains(s, p) {
		t.Fatal(s)
	}
}

func TestExplicitMetadataDirectoryProtection(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".git")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(dir, "metadata")
	put(t, p, "initial")
	g, e := Start(root, "Leave .git untouched")
	if e != nil {
		t.Fatal(e)
	}
	if s := g.Check(); s != "" {
		t.Fatal(s)
	}
	put(t, p, "changed")
	if s := g.Check(); !strings.Contains(s, p) {
		t.Fatal(s)
	}
}

func TestInPlacePreservationNote(t *testing.T) {
	g := &Guard{}
	t.Setenv("EXCALIBUR_IN_PLACE_GUIDANCE", "0")
	old := g.Note()
	t.Setenv("EXCALIBUR_IN_PLACE_GUIDANCE", "")
	current := g.Note()
	for _, want := range []string{"Preserve inputs that must stay unchanged", "For in-place edits, keep the deliverable at its best version so far"} {
		if !strings.Contains(current, want) || strings.Contains(old, want) {
			t.Fatal("in-place note", want)
		}
	}
	if len(current) > 200 {
		t.Fatal("scope note grew", len(current))
	}
	t.Logf("scope guidance: %d -> %d bytes (delta %+d)", len(old), len(current), len(current)-len(old))
}
