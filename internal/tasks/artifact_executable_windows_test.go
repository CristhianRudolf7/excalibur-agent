package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsDefaultStatExecutable(t *testing.T) {
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	for _, tt := range []struct {
		name string
		want bool
	}{
		{"runner.exe", true}, {"run.CMD", true}, {"report.txt", false}, {"runner", false},
	} {
		name := filepath.Join(t.TempDir(), tt.name)
		if err := os.WriteFile(name, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		stat, err := DefaultStat(name)
		if err != nil || stat.Executable != tt.want {
			t.Fatalf("%q: executable=%v, want %v, err=%v", name, stat.Executable, tt.want, err)
		}
	}
}
