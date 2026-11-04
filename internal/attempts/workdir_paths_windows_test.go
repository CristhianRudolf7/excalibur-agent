package attempts

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeWindowsWorkdirMap(t *testing.T) {
	real, attempt := t.TempDir(), t.TempDir()
	m := WorkdirMap{Real: real, Attempt: attempt}
	for _, path := range []string{filepath.Join(real, "out.txt"), filepath.ToSlash(strings.ToUpper(real)) + "/out.txt"} {
		if got := m.Path(path); got != filepath.Join(attempt, "out.txt") {
			t.Fatalf("Path(%q) = %q", path, got)
		}
	}
	for _, spelling := range []string{real, strings.ToUpper(real), filepath.ToSlash(real)} {
		command := `copy "` + spelling + `\input.txt" "` + spelling + `\out.txt"`
		want := `copy "` + attempt + `\input.txt" "` + attempt + `\out.txt"`
		if got := m.Command(command); got != want {
			t.Fatalf("Command(%q) = %q, want %q", command, got, want)
		}
		if sibling := `type "` + spelling + `-sibling\out.txt"`; m.Command(sibling) != sibling {
			t.Fatal("rewrote sibling prefix")
		}
	}
	if (WorkdirMap{Real: real, Attempt: strings.ToUpper(real)}).Enabled() {
		t.Fatal("case alias enabled self mapping")
	}
	if (WorkdirMap{Real: "/app", Attempt: attempt}).Enabled() {
		t.Fatal("native attempt enabled a POSIX map without a volume")
	}
}

func TestWindowsAttemptRejectsBaseThroughJunction(t *testing.T) {
	real := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", alias, real).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	_, _, cleanup, err := PrepareWorkspaces(alias, 1, filepath.Join(real, "attempts"))
	if cleanup != nil {
		cleanup()
	}
	if err == nil || !strings.Contains(err.Error(), "lies inside the project directory") {
		t.Fatalf("nested junction work base accepted: %v", err)
	}
}
