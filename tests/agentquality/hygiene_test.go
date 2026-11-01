package agentquality_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Dataset references belong in benchmark plumbing; individual case identities
// belong in external evaluation records, not product source or fixtures.
func TestRepositoryUsesNeutralEvaluationCaseNames(t *testing.T) {
	root := filepath.Join("..", "..")
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = root
	files, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	qualified := regexp.MustCompile(`terminal-bench/([a-z0-9][a-z0-9_-]*)`)
	for _, name := range strings.Split(string(files), "\x00") {
		if name == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range qualified.FindAllSubmatch(raw, -1) {
			if id := string(match[1]); id != "terminal-bench" && id != "terminal-bench-2-1" {
				t.Errorf("%s contains an individual evaluation case reference; use a neutral fixture", name)
			}
		}
	}
}
