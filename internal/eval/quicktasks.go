package eval

// Own quick tasks for cheap regression checks between arms: 25 small
// terminal tasks written from scratch for this harness (general
// engineering knowledge only — file edits, builds, data wrangling,
// local services, scripting). Each task ships its setup files and an
// exact verifier; no network, no paid APIs beyond the model call
// itself, and every task states the executables it needs so the
// runner skips cleanly when a tool is missing.
//
// Diversity (Section 23.5 terminal-vs-tools spirit): scripting (5),
// fix (5), build (4), data (4), services (2), terminal misc (5),
// including one Docker task. All verifiers are deterministic Go
// checks over the work directory.

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// QuickTask is one own terminal task: setup files materialized into
// an isolated work directory, a self-contained prompt for the agent,
// and a deterministic verifier over the resulting work directory.
type QuickTask struct {
	Name     string
	Category string
	Prompt   string
	// Files are written into the work directory before the run.
	Files map[string]string
	// Needs lists executables that must be on PATH; missing tools
	// skip the task instead of failing it.
	Needs []string
	// TimeoutSecs bounds the agent run (default QuickDefaultTimeout).
	TimeoutSecs int
	// Verifies checks the work directory after the run: nil error
	// passes the task.
	Verifies func(workdir string) error
}

// QuickDefaultTimeout bounds one quick task agent run.
const QuickDefaultTimeout = 180

// QuickTasks returns the 25 own regression tasks in stable order.
func QuickTasks() []QuickTask {
	return []QuickTask{
		{
			Name: "hello-file", Category: "scripting",
			Prompt:   "Create hello.txt in the current directory containing exactly the text hello.",
			Verifies: checkExactFile("hello.txt", "hello"),
		},
		{
			Name: "csv-sum", Category: "scripting",
			Prompt: "Read sales.csv in the current directory and write total.txt containing only the sum of the amount column as a plain integer (no extra text).",
			Files: map[string]string{
				"sales.csv": "item,amount\napples,12\npears,30\nplums,25\n",
			},
			Verifies: checkExactFile("total.txt", "67"),
		},
		{
			Name: "json-names", Category: "scripting",
			Prompt: "Read users.json in the current directory and write names.txt with the value of every name field, sorted alphabetically, one per line.",
			Files: map[string]string{
				"users.json": "[{\"name\": \"bob\", \"id\": 2}, {\"name\": \"amy\", \"id\": 1}, {\"name\": \"cid\", \"id\": 3}]\n",
			},
			Verifies: checkExactFile("names.txt", "amy\nbob\ncid\n"),
		},
		{
			Name: "log-errors", Category: "scripting",
			Prompt: "Count the lines containing the word ERROR in app.log in the current directory and write error_count.txt containing only that number.",
			Files: map[string]string{
				"app.log": "INFO start\nERROR disk full\nINFO retry\nERROR timeout\nINFO ok\nERROR refused\nINFO done\nERROR overflow\n",
			},
			Verifies: checkExactFile("error_count.txt", "4"),
		},
		{
			Name: "top-word", Category: "scripting",
			Prompt: "Read words.txt in the current directory (one word per line) and write top.txt containing only the most frequent word.",
			Files: map[string]string{
				"words.txt": "apple\nbanana\napple\ncherry\nbanana\napple\n",
			},
			Verifies: checkExactFile("top.txt", "apple"),
		},
		{
			Name: "py-syntax", Category: "fix",
			Prompt: "Fix broken.py in the current directory (it has a syntax error) so that running python3 broken.py prints all-done. Do not change what it prints.",
			Files: map[string]string{
				"broken.py": "def main()\n    print(\"all-done\")\n\nmain()\n",
			},
			Needs:    []string{"python3"},
			Verifies: checkCommandOutput([]string{"python3", "broken.py"}, "all-done\n"),
		},
		{
			Name: "shell-spaces", Category: "fix",
			Prompt: "Fix script.sh in the current directory so that ./script.sh \"my file.txt\" prints the content of my file.txt. The script currently breaks on the space in the file name.",
			Files: map[string]string{
				"script.sh":   "#!/bin/sh\ncat $1\n",
				"my file.txt": "spaced out\n",
			},
			Needs: []string{"sh"},
			Verifies: func(workdir string) error {
				if err := os.Chmod(filepath.Join(workdir, "script.sh"), 0o755); err != nil {
					return err
				}
				return checkCommandOutput([]string{"./script.sh", "my file.txt"}, "spaced out\n")(workdir)
			},
		},
		{
			Name: "go-build", Category: "fix",
			Prompt: "Fix main.go in the current directory (it fails to build) so that go build -o hello . succeeds and ./hello prints built-ok.",
			Files: map[string]string{
				"go.mod":  "module quickhello\n\ngo 1.21\n",
				"main.go": "package main\n\nfunc main() {\nprintln(\"built-ok\")\n}\n",
			},
			Needs: []string{"go"},
			Verifies: func(workdir string) error {
				build := exec.Command("go", "build", "-o", "hello", ".")
				build.Dir = workdir
				if out, err := build.CombinedOutput(); err != nil {
					return fmt.Errorf("go build: %v: %s", err, out)
				}
				return checkCommandOutput([]string{"./hello"}, "built-ok\n")(workdir)
			},
		},
		{
			Name: "makefile-tab", Category: "fix",
			Prompt: "Fix Makefile in the current directory (its recipe lines use spaces instead of a tab) so that make greet prints hi-from-make.",
			Files: map[string]string{
				"Makefile": "greet:\n    echo hi-from-make\n",
			},
			Needs:    []string{"make"},
			Verifies: checkCommandOutput([]string{"make", "-s", "greet"}, "hi-from-make\n"),
		},
		{
			Name: "json-config", Category: "fix",
			Prompt: "Fix config.json in the current directory (it is not valid JSON) keeping host=db.example and port=5432.",
			Files: map[string]string{
				"config.json": "{\"host\": \"db.example\", \"port\": 5432,}\n",
			},
			Verifies: checkJSONFields("config.json", map[string]string{"host": "db.example", "port": "5432"}),
		},
		{
			Name: "c-program", Category: "build",
			Prompt: "Fix hello.c in the current directory (it does not compile) so that cc -o hello hello.c succeeds and ./hello prints c-ok.",
			Files: map[string]string{
				"hello.c": "#include <stdio.h>\nint main(void) {\nprintf(\"c-ok\\n\")\nreturn 0\n}\n",
			},
			Needs: []string{"cc"},
			Verifies: func(workdir string) error {
				build := exec.Command("cc", "-o", "hello", "hello.c")
				build.Dir = workdir
				if out, err := build.CombinedOutput(); err != nil {
					return fmt.Errorf("cc: %v: %s", err, out)
				}
				return checkCommandOutput([]string{"./hello"}, "c-ok\n")(workdir)
			},
		},
		{
			Name: "py-import", Category: "build",
			Prompt: "Fix the import in main.py in the current directory so that python3 main.py prints 42. Do not change what the program computes.",
			Files: map[string]string{
				"pkg/__init__.py": "",
				"pkg/util.py":     "def answer():\n    return 42\n",
				"main.py":         "from util import answer\n\nprint(answer())\n",
			},
			Needs:    []string{"python3"},
			Verifies: checkCommandOutput([]string{"python3", "main.py"}, "42\n"),
		},
		{
			Name: "report-html", Category: "build",
			Prompt: "Read title.txt and data.csv in the current directory and write report.html containing the title in an h1 element and a table with one row per data line (header excluded).",
			Files: map[string]string{
				"title.txt": "Fruit report\n",
				"data.csv":  "fruit,qty\napple,3\npear,7\n",
			},
			Verifies: checkContainsAll("report.html", []string{"<h1>", "Fruit report", "apple", "pear", "3", "7", "<table"}),
		},
		{
			Name: "unpack", Category: "build",
			Prompt: "Extract archive.tar.gz in the current directory into extracted/ and write manifest.txt listing the extracted relative paths (files only), sorted, one per line.",
			Files:  map[string]string{},
			Needs:  []string{"tar"},
			Verifies: func(workdir string) error {
				if err := checkExactFile("manifest.txt", "a.txt\nsub/b.txt\n")(workdir); err != nil {
					return err
				}
				for _, p := range []string{"extracted/a.txt", "extracted/sub/b.txt"} {
					if _, err := os.Stat(filepath.Join(workdir, p)); err != nil {
						return fmt.Errorf("missing %s: %w", p, err)
					}
				}
				return nil
			},
		},
		{
			Name: "sqlite-avg", Category: "data",
			Prompt: "Using python3 and its standard library only, create shop.db from shop.sql in the current directory, then write avg.txt containing the average price over the items table formatted with two decimals.",
			Files: map[string]string{
				"shop.sql": "CREATE TABLE items(name TEXT, price REAL);\nINSERT INTO items VALUES ('a', 10.0);\nINSERT INTO items VALUES ('b', 20.0);\nINSERT INTO items VALUES ('c', 30.0);\n",
			},
			Needs:    []string{"python3"},
			Verifies: checkExactFile("avg.txt", "20.00"),
		},
		{
			Name: "dedup", Category: "data",
			Prompt: "Read dup.csv in the current directory and write unique.txt with the deduplicated lines sorted alphabetically, one per line.",
			Files: map[string]string{
				"dup.csv": "pear\napple\npear\nplum\napple\n",
			},
			Verifies: checkExactFile("unique.txt", "apple\npear\nplum\n"),
		},
		{
			Name: "checksums", Category: "data",
			Prompt: "In the current directory, write SHA256SUMS so that sha256sum -c SHA256SUMS passes for one.txt and two.txt.",
			Files: map[string]string{
				"one.txt": "first\n",
				"two.txt": "second\n",
			},
			Needs:    []string{"sha256sum"},
			Verifies: checkCommandOutput([]string{"sha256sum", "-c", "SHA256SUMS"}, ""),
		},
		{
			Name: "merge-sort", Category: "data",
			Prompt: "Read a.txt and b.txt in the current directory and write merged.txt with the sorted union of their lines (deduplicated), one per line.",
			Files: map[string]string{
				"a.txt": "delta\nalpha\ncharlie\n",
				"b.txt": "bravo\nalpha\ndelta\n",
			},
			Verifies: checkExactFile("merged.txt", "alpha\nbravo\ncharlie\ndelta\n"),
		},
		{
			Name: "http-ping", Category: "services",
			Prompt:   "Write server.py in the current directory using only the python3 standard library: an HTTP server that answers GET /ping with the plain body pong. It must read the port from the PING_PORT environment variable (default 8471). Start it yourself to verify it works, then stop it.",
			Files:    map[string]string{},
			Needs:    []string{"python3", "curl"},
			Verifies: checkHTTPPing("server.py"),
		},
		{
			Name: "run-twice", Category: "services",
			Prompt: "Write run.sh in the current directory: running it appends one non-empty line to hits.log (creating the file when missing). Run it twice yourself.",
			Files:  map[string]string{},
			Needs:  []string{"sh"},
			Verifies: func(workdir string) error {
				raw, err := os.ReadFile(filepath.Join(workdir, "hits.log"))
				if err != nil {
					return fmt.Errorf("hits.log missing: %w", err)
				}
				var lines int
				for _, l := range strings.Split(string(raw), "\n") {
					if strings.TrimSpace(l) != "" {
						lines++
					}
				}
				if lines < 2 {
					return fmt.Errorf("hits.log has %d non-empty lines, want >= 2", lines)
				}
				return nil
			},
		},
		{
			Name: "git-commit", Category: "terminal",
			Prompt: "In the current directory, write notes.txt containing the line remember the milk, then git init, stage and commit it with the message add notes.",
			Files:  map[string]string{},
			Needs:  []string{"git"},
			Verifies: func(workdir string) error {
				log := exec.Command("git", "log", "--format=%s")
				log.Dir = workdir
				out, err := log.Output()
				if err != nil {
					return fmt.Errorf("git log: %w", err)
				}
				if !strings.Contains(string(out), "add notes") {
					return fmt.Errorf("commit message missing: %q", out)
				}
				status := exec.Command("git", "status", "--porcelain")
				status.Dir = workdir
				sout, err := status.Output()
				if err != nil {
					return fmt.Errorf("git status: %w", err)
				}
				if strings.TrimSpace(string(sout)) != "" {
					return fmt.Errorf("work tree not clean: %q", sout)
				}
				raw, err := os.ReadFile(filepath.Join(workdir, "notes.txt"))
				if err != nil {
					return err
				}
				if !strings.Contains(string(raw), "remember the milk") {
					return fmt.Errorf("notes.txt content wrong: %q", raw)
				}
				return nil
			},
		},
		{
			Name: "replace", Category: "terminal",
			Prompt: "In the current directory, replace every occurrence of TODO with DONE in all .txt files.",
			Files: map[string]string{
				"a.txt": "TODO first\nkeep\nTODO second\n",
				"b.txt": "nothing to do\nTODO third\n",
			},
			Verifies: func(workdir string) error {
				for _, name := range []string{"a.txt", "b.txt"} {
					raw, err := os.ReadFile(filepath.Join(workdir, name))
					if err != nil {
						return err
					}
					if strings.Contains(string(raw), "TODO") {
						return fmt.Errorf("%s still contains TODO", name)
					}
				}
				raw, err := os.ReadFile(filepath.Join(workdir, "a.txt"))
				if err != nil {
					return err
				}
				if !strings.Contains(string(raw), "DONE first") || !strings.Contains(string(raw), "keep") {
					return fmt.Errorf("a.txt wrong: %q", raw)
				}
				return nil
			},
		},
		{
			Name: "big-files", Category: "terminal",
			Prompt: "In the current directory there are three data files. Write largest.txt with their file names sorted largest-first, one per line.",
			Files: map[string]string{
				"small.dat":  strings.Repeat("s", 100),
				"medium.dat": strings.Repeat("m", 3000),
				"large.dat":  strings.Repeat("l", 5000),
			},
			Verifies: checkExactFile("largest.txt", "large.dat\nmedium.dat\nsmall.dat\n"),
		},
		{
			Name: "today", Category: "terminal",
			Prompt: "Write today.txt in the current directory containing today's UTC date in YYYY-MM-DD format and nothing else.",
			Files:  map[string]string{},
			Verifies: func(workdir string) error {
				raw, err := os.ReadFile(filepath.Join(workdir, "today.txt"))
				if err != nil {
					return err
				}
				want := time.Now().UTC().Format("2006-01-02")
				if strings.TrimSpace(string(raw)) != want {
					return fmt.Errorf("today.txt = %q, want %q", strings.TrimSpace(string(raw)), want)
				}
				return nil
			},
		},
		{
			Name: "docker-build-run", Category: "docker",
			Prompt: "Build the image tagged quickapp from the Dockerfile in the current directory (docker build -t quickapp .) and make sure docker run --rm quickapp prints docker-ok.",
			Files: map[string]string{
				"Dockerfile": "FROM python:3.11-slim\nCOPY app.py /app.py\nCMD [\"python3\", \"/app.py\"]\n",
				"app.py":     "print(\"docker-ok\")\n",
			},
			Needs:       []string{"docker"},
			TimeoutSecs: 300,
			Verifies:    checkCommandOutput([]string{"docker", "run", "--rm", "quickapp"}, "docker-ok\n"),
		},
	}
}

// checkExactFile passes when path holds exactly want after trimming
// trailing newlines (agents may or may not end the file with one).
func checkExactFile(path, want string) func(string) error {
	return func(workdir string) error {
		raw, err := os.ReadFile(filepath.Join(workdir, path))
		if err != nil {
			return fmt.Errorf("%s missing: %w", path, err)
		}
		got := strings.TrimRight(string(raw), "\n")
		if got != strings.TrimRight(want, "\n") {
			return fmt.Errorf("%s = %q, want %q", path, got, want)
		}
		return nil
	}
}

// checkCommandOutput runs argv in the work directory and passes when
// it exits 0 and (when want is non-empty) prints exactly want.
func checkCommandOutput(argv []string, want string) func(string) error {
	return func(workdir string) error {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = workdir
		out, err := cmd.Output()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return fmt.Errorf("%s failed: %v: %s", strings.Join(argv, " "), err, ee.Stderr)
			}
			return fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
		}
		if want != "" && string(out) != want {
			return fmt.Errorf("%s printed %q, want %q", strings.Join(argv, " "), out, want)
		}
		return nil
	}
}

// checkJSONFields passes when path parses as a JSON object carrying
// the wanted string fields.
func checkJSONFields(path string, want map[string]string) func(string) error {
	return func(workdir string) error {
		raw, err := os.ReadFile(filepath.Join(workdir, path))
		if err != nil {
			return fmt.Errorf("%s missing: %w", path, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("%s invalid: %w", path, err)
		}
		for k, v := range want {
			if fmt.Sprintf("%v", doc[k]) != v {
				return fmt.Errorf("%s field %s = %v, want %s", path, k, doc[k], v)
			}
		}
		return nil
	}
}

// checkContainsAll passes when path exists and contains every needle.
func checkContainsAll(path string, needles []string) func(string) error {
	return func(workdir string) error {
		raw, err := os.ReadFile(filepath.Join(workdir, path))
		if err != nil {
			return fmt.Errorf("%s missing: %w", path, err)
		}
		for _, n := range needles {
			if !strings.Contains(string(raw), n) {
				return fmt.Errorf("%s missing %q", path, n)
			}
		}
		return nil
	}
}

// checkHTTPPing passes when server.py starts an HTTP server answering
// /ping with pong. The verifier picks a free port and injects it via
// PING_PORT, so parallel runs never collide.
func checkHTTPPing(server string) func(string) error {
	return func(workdir string) error {
		path := filepath.Join(workdir, server)
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("%s missing: %w", server, err)
		}
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		cmd := exec.Command("python3", server)
		cmd.Dir = workdir
		cmd.Env = append(os.Environ(), fmt.Sprintf("PING_PORT=%d", port))
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start %s: %w", server, err)
		}
		defer func() {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}()
		url := fmt.Sprintf("http://127.0.0.1:%d/ping", port)
		var last string
		for i := 0; i < 25; i++ {
			time.Sleep(200 * time.Millisecond)
			out, err := exec.Command("curl", "-fsS", url).Output()
			if err == nil {
				last = strings.TrimSpace(string(out))
				if last == "pong" {
					return nil
				}
				return fmt.Errorf("/ping answered %q, want pong", last)
			}
		}
		return fmt.Errorf("%s never answered /ping (last %q)", server, last)
	}
}

// QuickTaskSetup expands pseudo-files that need tooling: the unpack
// task's archive is built from plain payload files at setup time, so
// no binary fixture is committed to the repository.
func quickSetupFiles(t QuickTask, workdir string) error {
	if t.Name == "unpack" {
		payload := map[string]string{
			"payload/a.txt":     "alpha\n",
			"payload/sub/b.txt": "beta\n",
		}
		for name, content := range payload {
			p := filepath.Join(workdir, name)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
				return err
			}
		}
		cmd := exec.Command("tar", "-czf", "archive.tar.gz", "-C", "payload", "a.txt", "sub/b.txt")
		cmd.Dir = workdir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("build fixture archive: %v: %s", err, out)
		}
		if err := os.RemoveAll(filepath.Join(workdir, "payload")); err != nil {
			return err
		}
		return nil
	}
	return nil
}

// QuickTaskNames lists task names in stable order (for --list).
func QuickTaskNames() []string {
	tasks := QuickTasks()
	names := make([]string, 0, len(tasks))
	for _, t := range tasks {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	return names
}
