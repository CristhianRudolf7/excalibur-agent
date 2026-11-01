package scopeguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProvidedChecksProvenance(t *testing.T) {
	root := t.TempDir()
	g, err := Start(root, "Compute the result")
	if err != nil {
		t.Fatal(err)
	}
	g.ObserveCall("exec", `{"command":"python3 - <<'PY'\nassert actual == 9, 'details'\nPY"}`, "AssertionError", true)
	g.ObserveCall("exec", `{"command":"python3 - <<'PY'\nassert actual == 3, 'new details'\nPY"}`, "", false)
	p := filepath.Join(root, "test_ad_hoc.py")
	put(t, p, "assert actual == 9\n")
	g.Observe(true)
	put(t, p, "assert actual == 3\n")
	if reason := g.Check(); reason != "" {
		t.Fatal(reason)
	}
}

func TestProvidedAssertionFingerprint(t *testing.T) {
	for _, tc := range []struct {
		name, after string
		bad         bool
	}{
		{"message", `assert abs(actual - 9) < 0.01, f"missing {f}"`, false},
		{"tab", "assert\tabs(actual - 9) < 0.01, 'new diagnostic'", false},
		{"format", "# assert obsolete == 1\nassert abs( actual - 9 ) < 0.01, 'old' # comment", false},
		{"expected", `assert abs(actual - 3) < 0.01, 'old'`, true},
		{"tolerance", `assert abs(actual - 9) < 1.0, 'old'`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			p := filepath.Join(root, "test_values.py")
			put(t, p, `assert abs(actual - 9) < 0.01, 'old'`)
			g, err := Start(root, "Fix the implementation")
			if err != nil {
				t.Fatal(err)
			}
			g.Observe(true)
			put(t, p, tc.after)
			if got := strings.Contains(g.Check(), "test-tampering"); got != tc.bad {
				t.Fatalf("violation=%v: %s", got, g.Check())
			}
		})
	}
}

func TestNamedCheckDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tests")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	g, err := Start(root, "Run tests in ./tests")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "test_new.py")
	put(t, p, "assert value == 9")
	g.Observe(true)
	put(t, p, "assert value == 3")
	if !strings.Contains(g.Check(), "test-tampering") {
		t.Fatal(g.Check())
	}
}

func TestProvidedToleranceAssignment(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "test_accuracy.py")
	put(t, p, "tolerance = 0.01\nassert error < tolerance")
	g, err := Start(root, "Fix accuracy")
	if err != nil {
		t.Fatal(err)
	}
	g.Observe(true)
	put(t, p, "tolerance = 1.0\nassert error < tolerance")
	if !strings.Contains(g.Check(), "test-tampering") {
		t.Fatal(g.Check())
	}
}

func TestCommentsDoNotIntroduceExpectations(t *testing.T) {
	if got := checkFingerprint("value = 1 # expected later\n# assert x == 2"); got != "" {
		t.Fatal(got)
	}
}
