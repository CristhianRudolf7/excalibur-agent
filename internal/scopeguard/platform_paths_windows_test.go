package scopeguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsScopePaths(t *testing.T) {
	if got, want := originalsDir(), filepath.Join(os.TempDir(), "excalibur-originals"); got != want {
		t.Fatalf("%q != %q", got, want)
	}
	paths := []string{`C:\Users\test\data.db`, `D:\Users\test\data.db`, `\\server\share\data.db`}
	seen := map[string]bool{}
	for _, path := range paths {
		got := backupRelativePath(path)
		if filepath.IsAbs(got) || strings.Contains(got, ":") || seen[got] {
			t.Fatal(got)
		}
		seen[got] = true
	}
	if inputExecutable(0755) {
		t.Fatal("Windows execute bits must be ignored")
	}
	if fingerprintMode(0755) != fingerprintMode(0644) {
		t.Fatal("unstable permission fingerprint")
	}
	if fingerprintMode(0444) == fingerprintMode(0644) {
		t.Fatal("read-only transition was lost")
	}
}
func TestWindowsScopeResumeAndBackup(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.db")
	if err := os.WriteFile(path, []byte("input"), 0600); err != nil {
		t.Fatal(err)
	}
	g, err := StartRun(root, "Do not modify source.db", "windows-resume")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(g.StatePath))
	if backup := g.Backups[path]; backup == "" {
		t.Fatal("missing backup")
	} else {
		defer os.RemoveAll(filepath.Dir(backup))
		data, err := os.ReadFile(backup)
		if err != nil || string(data) != "input" {
			t.Fatalf("%q %v", data, err)
		}
	}
	if _, err = StartRun(root, "Do not modify source.db", "windows-resume"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if g.Check() == "" {
		t.Fatal("protected change was missed")
	}
}

func TestWindowsInstructionPathSeparators(t *testing.T) {
	for _, path := range []string{`C:\workspace\inputs`, `D:/workspace/inputs`, `\\server\share\inputs`, `.\tests`, `sub\input.db`} {
		got := pathNames("Leave " + path + " untouched")
		if len(got) != 1 || got[0] != path {
			t.Errorf("%q: %v", path, got)
		}
		if got := constraintPaths("Leave " + path + " untouched"); len(got) != 1 || got[0] != path {
			t.Errorf("constraint %q: %v", path, got)
		}
	}
	for _, path := range []string{`C:\workspace\tests`, `C:/workspace/tests`, `sub\verify.py`} {
		if !checkPath.MatchString(path) {
			t.Errorf("check directory or file missed: %s", path)
		}
	}
}
