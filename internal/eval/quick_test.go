package eval

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func requireTools(t *testing.T, needs []string) {
	t.Helper()
	for _, n := range needs {
		if _, err := exec.LookPath(n); err != nil {
			t.Skipf("missing tool %s", n)
		}
	}
}

func setupQuickWorkdir(t *testing.T, task QuickTask) string {
	t.Helper()
	workdir := t.TempDir()
	for name, content := range task.Files {
		p := filepath.Join(workdir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := quickSetupFiles(task, workdir); err != nil {
		t.Fatal(err)
	}
	return workdir
}

func writeFile(t *testing.T, workdir, name, content string) {
	t.Helper()
	p := filepath.Join(workdir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// referenceSolution applies the intended fix by construction (what a
// correct agent run produces), so the verifier positive path is
// exercised without a model call.
func referenceSolution(t *testing.T, task QuickTask, workdir string) {
	t.Helper()
	switch task.Name {
	case "hello-file":
		writeFile(t, workdir, "hello.txt", "hello\n")
	case "csv-sum":
		writeFile(t, workdir, "total.txt", "67\n")
	case "json-names":
		writeFile(t, workdir, "names.txt", "amy\nbob\ncid\n")
	case "log-errors":
		writeFile(t, workdir, "error_count.txt", "4\n")
	case "top-word":
		writeFile(t, workdir, "top.txt", "apple\n")
	case "py-syntax":
		writeFile(t, workdir, "broken.py", "def main():\n    print(\"all-done\")\n\nmain()\n")
	case "shell-spaces":
		writeFile(t, workdir, "script.sh", "#!/bin/sh\ncat \"$1\"\n")
		if err := os.Chmod(filepath.Join(workdir, "script.sh"), 0o755); err != nil {
			t.Fatal(err)
		}
	case "go-build":
		writeFile(t, workdir, "main.go", "package main\n\nimport \"fmt\"\n\nfunc main() {\nfmt.Println(\"built-ok\")\n}\n")
	case "makefile-tab":
		writeFile(t, workdir, "Makefile", "greet:\n\techo hi-from-make\n")
	case "json-config":
		writeFile(t, workdir, "config.json", "{\"host\": \"db.example\", \"port\": 5432}\n")
	case "c-program":
		writeFile(t, workdir, "hello.c", "#include <stdio.h>\nint main(void) {\nprintf(\"c-ok\\n\");\nreturn 0;\n}\n")
	case "py-import":
		writeFile(t, workdir, "main.py", "from pkg.util import answer\n\nprint(answer())\n")
	case "report-html":
		writeFile(t, workdir, "report.html", "<html><h1>Fruit report</h1><table><tr><td>apple</td><td>3</td></tr><tr><td>pear</td><td>7</td></tr></table></html>\n")
	case "unpack":
		cmd := exec.Command("tar", "-xzf", "archive.tar.gz")
		cmd.Dir = workdir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tar extract: %v: %s", err, out)
		}
		if err := os.MkdirAll(filepath.Join(workdir, "extracted"), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"a.txt", "sub/b.txt"} {
			raw, err := os.ReadFile(filepath.Join(workdir, f))
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, workdir, "extracted/"+f, string(raw))
			if err := os.Remove(filepath.Join(workdir, f)); err != nil {
				t.Fatal(err)
			}
		}
		writeFile(t, workdir, "manifest.txt", "a.txt\nsub/b.txt\n")
	case "sqlite-avg":
		writeFile(t, workdir, "avg.txt", "20.00\n")
	case "dedup":
		writeFile(t, workdir, "unique.txt", "apple\npear\nplum\n")
	case "checksums":
		cmd := exec.Command("sha256sum", "one.txt", "two.txt")
		cmd.Dir = workdir
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, workdir, "SHA256SUMS", string(out))
	case "merge-sort":
		writeFile(t, workdir, "merged.txt", "alpha\nbravo\ncharlie\ndelta\n")
	case "http-ping":
		writeFile(t, workdir, "server.py", "import os\nfrom http.server import BaseHTTPRequestHandler, HTTPServer\n\nclass H(BaseHTTPRequestHandler):\n    def do_GET(self):\n        if self.path == \"/ping\":\n            body = b\"pong\"\n            self.send_response(200)\n            self.send_header(\"Content-Length\", str(len(body)))\n            self.end_headers()\n            self.wfile.write(body)\n        else:\n            self.send_response(404)\n            self.end_headers()\n    def log_message(self, *a):\n        pass\n\nHTTPServer((\"127.0.0.1\", int(os.environ.get(\"PING_PORT\", \"8471\"))), H).serve_forever()\n")
	case "run-twice":
		writeFile(t, workdir, "run.sh", "#!/bin/sh\ndate -u +%FT%TZ >> hits.log\n")
		if err := os.Chmod(filepath.Join(workdir, "run.sh"), 0o755); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			cmd := exec.Command("sh", "run.sh")
			cmd.Dir = workdir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("run.sh: %v: %s", err, out)
			}
		}
	case "git-commit":
		for _, argv := range [][]string{
			{"init"}, {"config", "user.email", "t@t"},
			{"config", "user.name", "t"}, {"add", "notes.txt"},
			{"commit", "-m", "add notes"},
		} {
			if task.Name == "git-commit" && argv[0] == "add" {
				writeFile(t, workdir, "notes.txt", "remember the milk\n")
			}
			cmd := exec.Command("git", argv...)
			cmd.Dir = workdir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v: %s", argv, err, out)
			}
		}
	case "replace":
		for _, name := range []string{"a.txt", "b.txt"} {
			raw, err := os.ReadFile(filepath.Join(workdir, name))
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, workdir, name, strings.ReplaceAll(string(raw), "TODO", "DONE"))
		}
	case "big-files":
		writeFile(t, workdir, "largest.txt", "large.dat\nmedium.dat\nsmall.dat\n")
	case "today":
		writeFile(t, workdir, "today.txt", time.Now().UTC().Format("2006-01-02")+"\n")
	case "docker-build-run":
		cmd := exec.Command("docker", "build", "-t", "quickapp", ".")
		cmd.Dir = workdir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("docker build: %v: %s", err, out)
		}
	default:
		t.Fatalf("no reference solution for %s", task.Name)
	}
}

// TestQuickVerifiers runs every own task verifier against its
// reference solution (must pass) and against an empty directory
// (must fail): exact checks with no model in the loop.
func TestQuickVerifiers(t *testing.T) {
	tasks := QuickTasks()
	if len(tasks) != 25 {
		t.Fatalf("quick suite has %d tasks, want 25", len(tasks))
	}
	seen := map[string]bool{}
	for _, task := range tasks {
		if seen[task.Name] {
			t.Fatalf("duplicate task %s", task.Name)
		}
		seen[task.Name] = true
		if task.Prompt == "" || task.Verifies == nil || task.Category == "" {
			t.Fatalf("task %s incomplete", task.Name)
		}
	}
	for _, task := range tasks {
		t.Run(task.Name, func(t *testing.T) {
			requirePOSIXQuickVerifier(t, task.Name)
			requireTools(t, task.Needs)
			if task.Category == "docker" {
				if err := exec.Command("docker", "info").Run(); err != nil {
					t.Skip("no docker daemon")
				}
			}
			workdir := setupQuickWorkdir(t, task)
			referenceSolution(t, task, workdir)
			if err := task.Verifies(workdir); err != nil {
				t.Fatalf("reference solution rejected: %v", err)
			}
			// The docker image tag is daemon-global state: once any
			// reference solution builds it, an "empty directory"
			// still verifies (the image exists). Absence is covered
			// by the verifier failing on unknown images instead.
			if task.Category == "docker" {
				return
			}
			empty := t.TempDir()
			if err := task.Verifies(empty); err == nil {
				t.Fatal("verifier passes on an empty directory")
			}
		})
	}
}

func TestQuickRunnerFakeExec(t *testing.T) {
	dir := t.TempDir()
	runner := QuickRunner{
		Dir: dir, Concurrency: 2, Filter: "hello-file",
		Arm: "test-arm", Switches: map[string]string{"K": "v"},
		Exec: func(ctx context.Context, bin, prompt, workdir, logsDir string, env []string, timeout time.Duration) (int, error) {
			if err := os.WriteFile(workdir+"/hello.txt", []byte("hello\n"), 0o644); err != nil {
				return 1, err
			}
			return 0, nil
		},
	}
	outcomes, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || !outcomes[0].Passed {
		t.Fatalf("outcomes = %+v", outcomes)
	}
	if err := runner.WriteResults(outcomes); err != nil {
		t.Fatal(err)
	}
	trials, err := LoadRunTrials(dir)
	if err != nil || len(trials) != 1 || !trials[0].Passed() {
		t.Fatalf("trials = %+v, %v", trials, err)
	}
	man := LoadRunManifest(dir)
	if man.Arm != "test-arm" || man.Switches["K"] != "v" || man.Dataset != "quick/local" {
		t.Fatalf("manifest = %+v", man)
	}
	if man.ArmProfileHash == "" || man.ArmProfileHash == man.ProfileHash {
		t.Fatal("arm profile hash must differ from the frozen hash")
	}
}

func TestQuickRunnerFailuresAndSkips(t *testing.T) {
	dir := t.TempDir()
	runner := QuickRunner{
		Dir: dir, Concurrency: 4, MissingBins: map[string]bool{"python3": true},
		Exec: func(ctx context.Context, bin, prompt, workdir, logsDir string, env []string, timeout time.Duration) (int, error) {
			if strings.Contains(workdir, "hello-file") {
				return 0, nil // agent did nothing: verifier fails
			}
			return 3, nil // harness failure
		},
	}
	outcomes, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]QuickOutcome{}
	for _, oc := range outcomes {
		byName[oc.Task] = oc
	}
	if len(outcomes) != 25 {
		t.Fatalf("outcomes = %d, want 25", len(outcomes))
	}
	if byName["hello-file"].Passed || byName["hello-file"].ExitCode != 0 {
		t.Fatalf("empty agent work must fail the verifier: %+v", byName["hello-file"])
	}
	if byName["csv-sum"].Passed {
		t.Fatalf("nonzero exit must fail: %+v", byName["csv-sum"])
	}
	if !byName["py-syntax"].Skipped {
		t.Fatalf("missing python3 must skip: %+v", byName["py-syntax"])
	}
	if err := runner.WriteResults(outcomes); err != nil {
		t.Fatal(err)
	}
	rep, err := Aggregate(mustLoadTrials(t, dir), 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Failures[FailVerifier] == 0 || rep.Failures[FailHarness] == 0 {
		t.Fatalf("failure split wrong: %v", rep.Failures)
	}
}

func mustLoadTrials(t *testing.T, dir string) []TrialOutcome {
	t.Helper()
	trials, err := LoadRunTrials(dir)
	if err != nil {
		t.Fatal(err)
	}
	return trials
}

// TestQuickCompareAcrossArms pins the ablation loop: two quick dirs
// with different arms compare, and the switch diff names the change.
func TestQuickCompareAcrossArms(t *testing.T) {
	mkRunner := func(dir, arm string, writeHello bool) QuickRunner {
		return QuickRunner{
			Dir: dir, Concurrency: 2, Filter: "hello-file", Arm: arm,
			Switches: map[string]string{"EXCALIBUR_X": arm},
			Exec: func(ctx context.Context, bin, prompt, workdir, logsDir string, env []string, timeout time.Duration) (int, error) {
				if writeHello {
					_ = os.WriteFile(workdir+"/hello.txt", []byte("hello\n"), 0o644)
				}
				return 0, nil
			},
		}
	}
	dirA, dirB := t.TempDir(), t.TempDir()
	ra, rb := mkRunner(dirA, "a-off", false), mkRunner(dirB, "a-on", true)
	oa, err := ra.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ob, err := rb.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := ra.WriteResults(oa); err != nil {
		t.Fatal(err)
	}
	if err := rb.WriteResults(ob); err != nil {
		t.Fatal(err)
	}
	rep, err := CompareRuns(dirA, dirB, 1, 500, 7, 0.99)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Diff.Mean != 1 {
		t.Fatalf("paired diff = %v, want 1", rep.Diff.Mean)
	}
	if rep.SwitchDiff["EXCALIBUR_X"] != [2]string{"a-off", "a-on"} {
		t.Fatalf("switch diff = %v", rep.SwitchDiff)
	}
}

// These fixtures deliberately verify Linux executable names and shebangs as
// used by the evaluation container; native Windows cannot execute that ABI.
func requirePOSIXQuickVerifier(t *testing.T, name string) {
	t.Helper()
	if runtime.GOOS == "windows" && (name == "shell-spaces" || name == "go-build" || name == "c-program") {
		t.Skip("evaluation container fixture requires POSIX shebang or extensionless executable launch")
	}
}
