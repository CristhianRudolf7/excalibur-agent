// Required-artifact gate: extraction, filesystem checks and
// background-session checks (Sections 7.3, 7.5).
package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func artifactKeys(reqs []ArtifactRequirement) map[string]ArtifactKind {
	out := map[string]ArtifactKind{}
	for _, r := range reqs {
		key := r.Path
		if r.Kind == ArtifactPort {
			key = "port"
		}
		if r.Kind == ArtifactURL {
			key = r.URL
		}
		if prev, ok := out[key]; ok && prev != r.Kind {
			continue
		}
		out[key] = r.Kind
	}
	return out
}

func TestExtractInstallPaths(t *testing.T) {
	req := "Find and download the source archives, extract them to `/app/povray-2.2`, then compile and install to `/usr/local/bin/povray`."
	reqs := ExtractArtifactRequirements(req, 8)
	keys := artifactKeys(reqs)
	if keys["/app/povray-2.2"] == "" {
		t.Fatalf("missing /app/povray-2.2 in %+v", reqs)
	}
	if got := keys["/usr/local/bin/povray"]; got != "" {
		t.Fatalf("path outside allowed roots became a requirement: %q (in %+v)", got, reqs)
	}
}

func TestExtractCalledFile(t *testing.T) {
	req := "Put the function in a file called `/app/run.py` so that it can be imported using `from run import run_tasks`."
	reqs := ExtractArtifactRequirements(req, 8)
	keys := artifactKeys(reqs)
	if got := keys["/app/run.py"]; got != ArtifactOutput && got != ArtifactPath {
		t.Fatalf("/app/run.py kind = %q, want output/path (in %+v)", got, reqs)
	}
	// The import line is usage, not content and not a path.
	for _, r := range reqs {
		if strings.Contains(r.Path, " ") {
			t.Fatalf("multi-word span became a requirement: %+v", r)
		}
		if len(r.Expect) != 0 {
			t.Fatalf("import usage became content expectation: %+v", r)
		}
	}
}

func TestExtractMoveFileSkipsExample(t *testing.T) {
	req := "Write the best move for white to play to /app/move.txt in the form [src][dst], for example, e2e4 or h1h8."
	reqs := ExtractArtifactRequirements(req, 8)
	keys := artifactKeys(reqs)
	if keys["/app/move.txt"] == "" {
		t.Fatalf("missing /app/move.txt in %+v", reqs)
	}
	for _, r := range reqs {
		if r.Path == "e2e4" || r.Path == "h1h8" {
			t.Fatalf("example move became a requirement: %+v", r)
		}
		if len(r.Expect) != 0 {
			t.Fatalf("bracket pattern became content: %+v", r)
		}
	}
}

func TestExtractServerPortAndURL(t *testing.T) {
	req := "Set up a pypi server on port 8080 locally. It should be possible to install with `pip install --index-url http://localhost:8080/simple vectorops==0.1.0`."
	reqs := ExtractArtifactRequirements(req, 8)
	var port, url bool
	for _, r := range reqs {
		if r.Kind == ArtifactPort && r.Port == 8080 {
			port = true
		}
		if r.Kind == ArtifactURL && strings.Contains(r.URL, "localhost:8080") {
			url = true
		}
	}
	if !port || !url {
		t.Fatalf("missing port/url requirements in %+v", reqs)
	}
}

func TestExtractCSVOutputWithHeader(t *testing.T) {
	req := "Write a CSV file /app/summary.csv with the following structure (including the header):\nperiod,severity,count\ntoday,ERROR,<count>"
	reqs := ExtractArtifactRequirements(req, 8)
	var found *ArtifactRequirement
	for i, r := range reqs {
		if r.Path == "/app/summary.csv" {
			found = &reqs[i]
		}
	}
	if found == nil {
		t.Fatalf("missing /app/summary.csv in %+v", reqs)
	}
	if found.Kind != ArtifactOutput {
		t.Fatalf("kind = %q, want output", found.Kind)
	}
	if len(found.Expect) == 0 || found.Expect[0] != "period,severity,count" {
		t.Fatalf("header expectation = %v, want period,severity,count", found.Expect)
	}
}

func TestExtractSkipsPatternProse(t *testing.T) {
	req := "Each log file name follows the pattern YYYY-MM-DD_<source>.log (e.g., 2025-08-10_db.log), indicating the date of the logs."
	reqs := ExtractArtifactRequirements(req, 8)
	for _, r := range reqs {
		if strings.Contains(r.Path, "2025-08-10") || strings.Contains(r.Path, "YYYY") ||
			strings.Contains(r.Path, "<") || strings.Contains(r.Path, "source") {
			t.Fatalf("pattern prose became a requirement: %+v", r)
		}
	}
}

func TestExtractSkipsForExamplePaths(t *testing.T) {
	cases := []string{
		"For example, you might read /tmp/foo for inspiration before writing the report.",
		"Tools such as /var/data/cache can help, but the deliverable stands alone.",
		"For instance, see /opt/legacy/app for how it was done before.",
	}
	for _, req := range cases {
		if reqs := ExtractArtifactRequirements(req, 8); len(reqs) != 0 {
			t.Fatalf("example prose produced requirements for %q: %+v", req, reqs)
		}
	}
}

func TestExtractRelativeAndBareFiles(t *testing.T) {
	req := "Create the python file /app/filter.py that removes scripts. Also update ./tools/run.sh and `docs/notes.md`."
	reqs := ExtractArtifactRequirements(req, 8)
	keys := artifactKeys(reqs)
	found := map[string]bool{}
	for k := range keys {
		found[k] = true
	}
	if !found["/app/filter.py"] {
		t.Fatalf("missing /app/filter.py in %+v", reqs)
	}
	if !found["./tools/run.sh"] && !found["run.sh"] {
		t.Fatalf("missing run.sh in %+v", reqs)
	}
	if !found["docs/notes.md"] && !found["notes.md"] {
		t.Fatalf("missing notes.md in %+v", reqs)
	}
}

func TestExtractBareNameNeedsImperative(t *testing.T) {
	req := "The report mentions config.yaml among other files, but nothing needs to change."
	if reqs := ExtractArtifactRequirements(req, 8); len(reqs) != 0 {
		t.Fatalf("passing prose mention became a requirement: %+v", reqs)
	}
}

func TestArtifactExtractDedupesAndCaps(t *testing.T) {
	req := "/app/a.py /app/a.py `/app/a.py` and port 8080 on port 8080, see http://localhost:8080/x"
	reqs := ExtractArtifactRequirements(req, 2)
	if len(reqs) != 2 {
		t.Fatalf("drafts = %d, want cap 2 (%+v)", len(reqs), reqs)
	}
	seen := map[string]bool{}
	for i, r := range reqs {
		if want := "A" + itoa(i+1); r.ID != want {
			t.Fatalf("ID = %q, want %q", r.ID, want)
		}
		key := string(r.Kind) + r.Path + r.URL
		if seen[key] {
			t.Fatalf("duplicate requirement %+v", r)
		}
		seen[key] = true
	}
	if got := ExtractArtifactRequirements("", 8); len(got) != 0 {
		t.Fatalf("empty request should yield no requirements, got %+v", got)
	}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return "x"
}

func TestExtractInvalidPortSkipped(t *testing.T) {
	req := "Listen on port 99999 for the test harness."
	for _, r := range ExtractArtifactRequirements(req, 8) {
		if r.Kind == ArtifactPort {
			t.Fatalf("out-of-range port became a requirement: %+v", r)
		}
	}
}

// fakeFS is an in-memory filesystem for gate tests.
type fakeFS struct {
	files map[string]ArtifactFS
	text  map[string]string
}

func (f *fakeFS) stat(path string) (ArtifactFS, error) {
	fs, ok := f.files[path]
	if !ok {
		return ArtifactFS{}, os.ErrNotExist
	}
	return fs, nil
}

func (f *fakeFS) read(path string) (string, error) {
	s, ok := f.text[path]
	if !ok {
		return "", os.ErrNotExist
	}
	return s, nil
}

func okHistory(args, output string) []HistoryTool {
	return []HistoryTool{{Name: "exec", Args: args, Output: output}}
}

func TestGateMissingEmptyUnexecutable(t *testing.T) {
	reqs := []ArtifactRequirement{
		{ID: "A1", Kind: ArtifactOutput, Path: "/app/run.py", Detail: "output", Source: "put it in /app/run.py"},
		{ID: "A2", Kind: ArtifactExecutable, Path: "/usr/local/bin/povray", Detail: "executable", Source: "install to /usr/local/bin/povray"},
	}
	fs := &fakeFS{files: map[string]ArtifactFS{}, text: map[string]string{}}
	got := CheckArtifactFiles(reqs, nil, "", fs.stat, fs.read)
	if len(got) != 2 {
		t.Fatalf("missing files should block twice, got %v", got)
	}
	fs.files["/app/run.py"] = ArtifactFS{Exists: true, Size: 0}
	got = CheckArtifactFiles(reqs, nil, "", fs.stat, fs.read)
	if len(got) != 2 || !strings.Contains(got[0]+got[1], "empty") {
		t.Fatalf("empty output should block as empty, got %v", got)
	}
	fs.files["/app/run.py"] = ArtifactFS{Exists: true, Size: 12, NonEmpty: true}
	fs.files["/usr/local/bin/povray"] = ArtifactFS{Exists: true, Size: 100, NonEmpty: true}
	got = CheckArtifactFiles(reqs, nil, "", fs.stat, fs.read)
	if len(got) != 1 || !strings.Contains(got[0], "not executable") {
		t.Fatalf("unexecutable install should block, got %v", got)
	}
	fs.files["/usr/local/bin/povray"] = ArtifactFS{Exists: true, Size: 100, NonEmpty: true, Executable: true}
	if got := CheckArtifactFiles(reqs, nil, "", fs.stat, fs.read); len(got) != 0 {
		t.Fatalf("installed artifacts should pass, got %v", got)
	}
}

func TestGateContentExpectation(t *testing.T) {
	reqs := []ArtifactRequirement{
		{ID: "A1", Kind: ArtifactOutput, Path: "/app/summary.csv", Expect: []string{"period,severity,count"}, Source: "write /app/summary.csv"},
	}
	fs := &fakeFS{
		files: map[string]ArtifactFS{"/app/summary.csv": {Exists: true, Size: 40, NonEmpty: true}},
		text:  map[string]string{"/app/summary.csv": "oops,wrong,header\n"},
	}
	if got := CheckArtifactFiles(reqs, nil, "", fs.stat, fs.read); len(got) != 1 || !strings.Contains(got[0], "content missing") {
		t.Fatalf("wrong header should block, got %v", got)
	}
	fs.text["/app/summary.csv"] = "period,severity,count\ntoday,ERROR,3\n"
	if got := CheckArtifactFiles(reqs, nil, "", fs.stat, fs.read); len(got) != 0 {
		t.Fatalf("right header should pass, got %v", got)
	}
}

func TestGatePortNeedsHistoryEvidence(t *testing.T) {
	reqs := []ArtifactRequirement{{ID: "A1", Kind: ArtifactPort, Port: 8080, Source: "server on port 8080"}}
	if got := CheckArtifactFiles(reqs, okHistory("ls /app", "session: sess-1 alive: false\ndone"), "", nil, nil); len(got) != 1 {
		t.Fatalf("untouched port should block, got %v", got)
	}
	history := okHistory("pip install --index-url http://localhost:8080/simple vectorops", "session: sess-2 alive: false\nSuccessfully installed")
	if got := CheckArtifactFiles(reqs, history, "", nil, nil); len(got) != 0 {
		t.Fatalf("exercised port should pass, got %v", got)
	}
}

func TestGateURLNeedsHistoryEvidence(t *testing.T) {
	reqs := []ArtifactRequirement{{ID: "A1", Kind: ArtifactURL, URL: "http://localhost:8080/simple", Source: "index url"}}
	if got := CheckArtifactFiles(reqs, nil, "", nil, nil); len(got) != 1 {
		t.Fatalf("untouched URL should block, got %v", got)
	}
}

func TestGateResolvesRelativeUnderWorkdir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "out.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	reqs := []ArtifactRequirement{{ID: "A1", Kind: ArtifactOutput, Path: "out.txt", Source: "write out.txt"}}
	if got := CheckArtifactFiles(reqs, nil, dir, nil, nil); len(got) != 0 {
		t.Fatalf("workdir-relative file should pass, got %v", got)
	}
	if got := CheckArtifactFiles(reqs, nil, t.TempDir(), nil, nil); len(got) != 1 {
		t.Fatalf("wrong workdir should block, got %v", got)
	}
}

func TestGateCapsPendings(t *testing.T) {
	var reqs []ArtifactRequirement
	for i := 0; i < 10; i++ {
		reqs = append(reqs, ArtifactRequirement{ID: "A1", Kind: ArtifactPath, Path: "/app/missing", Source: "s"})
	}
	got := CheckArtifactFiles(reqs, nil, "", nil, nil)
	if len(got) > 6 {
		t.Fatalf("pendings uncapped: %d lines", len(got))
	}
}

func TestGateSwitchOff(t *testing.T) {
	cfg := DefaultArtifactConfig()
	cfg.Enable = false
	req := ArtifactRequest{Request: "Create /app/never.py.", History: okHistory("x", "y")}
	if got := ApplyArtifactGate(cfg, req); len(got) != 0 {
		t.Fatalf("switch off must pass through, got %v", got)
	}
}

func TestGateLegacyUntouched(t *testing.T) {
	cfg := DefaultArtifactConfig()
	if got := ApplyArtifactGate(cfg, ArtifactRequest{}); len(got) != 0 {
		t.Fatalf("empty legacy input must pass, got %v", got)
	}
}

func TestLiveSessionsLifecycle(t *testing.T) {
	history := []HistoryTool{
		{Name: "exec", Args: "sleep 60", Output: "session: sess-1 alive: true next_offset: 0"},
		{Name: "exec_io", Args: `{"action":"read","session":"sess-1"}`, Output: "exited: false next_offset: 10\nstill going"},
		{Name: "exec_io", Args: `{"action":"close","session":"sess-1"}`, Output: "closed sess-1"},
	}
	if live := LiveSessions(history); len(live) != 0 {
		t.Fatalf("closed session should not be live: %v", live)
	}
	running := []HistoryTool{
		{Name: "exec", Args: "server", Output: "session: sess-5 alive: true next_offset: 0"},
	}
	if live := LiveSessions(running); len(live) != 1 || live[0] != "sess-5" {
		t.Fatalf("live = %v, want [sess-5]", live)
	}
	settled := append(running, HistoryTool{
		Name: "exec", Args: "ps", Output: "session: sess-5 alive: false\ndone",
	})
	if live := LiveSessions(settled); len(live) != 0 {
		t.Fatalf("settled session should not be live: %v", live)
	}
}

func TestBackgroundBlocksUnverifiedServer(t *testing.T) {
	history := []HistoryTool{
		{Name: "exec", Args: "pypi-server", Output: "session: sess-5 alive: true next_offset: 0"},
	}
	req := "Set up a pypi server on port 8080 locally."
	if got := CheckBackgroundSessions(history, req, "done"); len(got) != 1 || !strings.Contains(got[0], "unverified") {
		t.Fatalf("starting server should block as unverified, got %v", got)
	}
}

func TestBackgroundPassesVerifiedPersistentServer(t *testing.T) {
	history := []HistoryTool{
		{Name: "exec", Args: "start server", Output: "session: sess-5 alive: true next_offset: 0"},
		{Name: "exec", Args: "curl -s http://localhost:8080/simple", Output: "session: sess-6 alive: false\n200 OK"},
	}
	req := "Set up a pypi server on port 8080 locally."
	if got := CheckBackgroundSessions(history, req, "server up"); len(got) != 0 {
		t.Fatalf("verified persistent server should pass, got %v", got)
	}
}

func TestBackgroundPassesDeclaredPersistent(t *testing.T) {
	history := []HistoryTool{
		{Name: "exec", Args: "start server", Output: "session: sess-5 alive: true next_offset: 0"},
		{Name: "exec_io", Args: `{"action":"wait_for","session":"sess-5"}`, Output: "matched; exited: false next_offset: 4\nServing on :8080"},
	}
	if got := CheckBackgroundSessions(history, "run the job", "server left running intentionally on port 8080"); len(got) != 0 {
		t.Fatalf("declared persistent server should pass, got %v", got)
	}
}

func TestBackgroundBlocksUndeclaredVerified(t *testing.T) {
	history := []HistoryTool{
		{Name: "exec", Args: "make -j", Output: "session: sess-1 alive: true next_offset: 0"},
		{Name: "exec_io", Args: `{"action":"read","session":"sess-1"}`, Output: "exited: false next_offset: 5\nbuilding"},
	}
	if got := CheckBackgroundSessions(history, "Build the project with make.", "done"); len(got) != 1 || !strings.Contains(got[0], "stop it") {
		t.Fatalf("one-shot live session should ask to stop, got %v", got)
	}
}

func TestArtifactConfigEnvOverrides(t *testing.T) {
	t.Setenv("EXCALIBUR_ARTIFACT_GATE", "0")
	t.Setenv("EXCALIBUR_ARTIFACT_MAX", "3")
	cfg := DefaultArtifactConfig()
	if cfg.Enable {
		t.Fatal("env must disable the gate")
	}
	if cfg.MaxArtifacts != 3 {
		t.Fatalf("max = %d, want 3", cfg.MaxArtifacts)
	}
	t.Setenv("EXCALIBUR_ARTIFACT_GATE", "on")
	if cfg := DefaultArtifactConfig(); !cfg.Enable {
		t.Fatal("env on must enable the gate")
	}
}
