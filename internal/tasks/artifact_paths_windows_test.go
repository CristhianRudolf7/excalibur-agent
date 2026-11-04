package tasks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeWindowsArtifactPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "future", "report.csv")
	for _, spelling := range []string{path, strings.ToUpper(path), filepath.ToSlash(path)} {
		reqs := ExtractArtifactRequirementsIn("Create "+spelling, 8, strings.ToLower(dir))
		found := false
		for _, req := range reqs {
			if req.Path == spelling && HardArtifactRequirement(req) {
				found = true
			}
		}
		if !found {
			t.Fatalf("native absolute output missing for %q: %+v", spelling, reqs)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("header\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := ApplyArtifactGate(DefaultArtifactConfig(), ArtifactRequest{Request: "Create " + strings.ToUpper(path), Workdir: dir}); len(got) != 0 {
		t.Fatalf("existing output refused: %v", got)
	}
}

func TestWindowsQuotedArtifactAndRelativeEvidence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Mixed Case Project")
	path := filepath.Join(dir, "Result File.py")
	request := "Create `" + path + "` with the result."
	reqs := ExtractArtifactRequirementsIn(request, 8, dir)
	if len(reqs) == 0 || reqs[0].Path != path || !HardArtifactRequirement(reqs[0]) {
		t.Fatalf("quoted path lost: %+v", reqs)
	}
	drafts := ExtractDraftCriteria(request, 8)
	write, _ := json.Marshal(map[string]string{"path": path})
	check, _ := json.Marshal(map[string]string{"command": `python3 "Result File.py"`})
	history := []HistoryTool{{Name: "write_file", Args: string(write), Output: "written"}, {Name: "exec", Args: string(check), Output: "exit: 0\nvalid"}}
	if ok, omissions := DraftCoverage(drafts, "", history); !ok {
		t.Fatalf("basename coverage missing: %v", omissions)
	}
	if reasons := CheckStrongExercise(history, drafts[:1], 1); len(reasons) > 0 {
		t.Fatalf("relative check missing: %v", reasons)
	}
}

func TestWindowsUNCArtifactUnderWorkdir(t *testing.T) {
	root := `\\server\share\Project`
	path := `\\SERVER\SHARE\project\result.csv`
	reqs := ExtractArtifactRequirementsIn("Create `"+path+"`", 8, root)
	if len(reqs) == 0 || reqs[0].Path != path || !HardArtifactRequirement(reqs[0]) {
		t.Fatalf("UNC output missing: %+v", reqs)
	}
}

func TestWindowsNativeExecutableEvidence(t *testing.T) {
	for _, command := range []string{`C:\work\worker.exe input.csv`, `.\worker.EXE input.csv`, `"C:\My Project\worker.exe" input.csv`, `worker.cmd input.csv`} {
		if !ExercisesDeliverable(command) {
			t.Errorf("native execution rejected: %s", command)
		}
	}
	for _, command := range []string{`C:\work\worker.exe --help`, `.\worker.exe -h`, `"C:\My Project\worker.exe" -h`, `.\worker.exe --version`, `C:\mingw\bin\gcc.exe main.c`, `C:\tools\python.exe -m py_compile worker.py`, `type check.txt`, `Get-Content check.txt`} {
		if ExercisesDeliverable(command) {
			t.Errorf("observation or compilation accepted: %s", command)
		}
	}
}
