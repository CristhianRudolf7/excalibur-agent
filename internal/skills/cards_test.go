package skills

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// Every trigger skill must exist in the builtin catalog with the
// template sections the excerpt builder serves; a renamed skill must
// break this test instead of silently disabling its trigger.
func TestCardTriggersResolve(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, p := range cardPatterns {
		seen[p.skill] = true
	}
	for bin, skill := range cardBin {
		_ = bin
		seen[skill] = true
	}
	for name := range seen {
		e, ok := ix.Get(name)
		if !ok {
			t.Errorf("trigger skill %q is not indexed", name)
			continue
		}
		if e.State == StateArchived {
			t.Errorf("trigger skill %q is archived and cannot load", name)
		}
		// Trigger skills are card-grade by contract: the excerpt
		// builder serves their Procedure/Pitfalls/Verification
		// sections, so a future edit that de-templates one must
		// break here rather than silently degrade cards.
		sections := map[string]bool{}
		for _, s := range SectionNames(e.Body) {
			sections[strings.ToLower(strings.TrimSpace(s))] = true
		}
		for _, want := range []string{"procedure", "pitfalls", "verification"} {
			if !sections[want] {
				t.Errorf("trigger skill %q misses section %q", name, want)
			}
		}
	}
}

// Concrete failures fire exactly one card; unrelated or empty
// output fires none. Precision first: a bare mention inside a
// passing run must never pay for a card on its own (callers only
// invoke matching on failures).
func TestMatchError(t *testing.T) {
	cases := []struct {
		name   string
		output string
		skill  string
	}{
		{"bash missing binary", "/usr/bin/bash: line 1: sqlite3: command not found", "sqlite-recipes"},
		{"hint form", "session: sess-2 alive: false\nhint: command not found: file.", "elf-binary-analysis"},
		{"hint sqlite", "hint: command not found: sqlite3", "sqlite-recipes"},
		{"dash form", "sh: 1: docker: not found", "docker-containers"},
		{"trailing form", "make: command not found", "make-builds"},
		{"quoted binary", "bash: line 1: `jq': command not found", "jq-recipes"},
		{"unknown binary", "/usr/bin/bash: line 1: frobnicate: command not found", ""},
		{"empty", "", ""},
		{"success prose", "all tests passed", ""},
		{"make separator", "Makefile:12: *** missing separator.  Stop.", "make-builds"},
		{"no rule", "make: *** No rule to make target 'foo'.", "make-builds"},
		{"permission", "open /app/out.txt: permission denied", "file-permissions-ownership"},
		{"port in use", "listen tcp :8080: bind: address already in use", "network-ports-debugging"},
		{"tls verify", "certificate verify failed: self signed certificate", "tls-openssl-recipes"},
		{"linker", "/usr/bin/ld: cannot find -lfoo", "linking-errors"},
		{"undefined ref", "undefined reference to `main'", "linking-errors"},
		{"not a repo", "fatal: not a git repository", "git-internals"},
		{"go mod", "go: updates to go.mod needed; to update it: go mod tidy", "go-modules-toolchain"},
		{"npm err", "npm ERR! code E404\nnpm ERR! 404 Not Found", "node-npm-packaging"},
		{"pip no dist", "ERROR: No matching distribution found for frobnicate", "python-pip-uv-venv"},
		{"cmake error", "CMake Error at CMakeLists.txt:3 (project):", "cmake-projects"},
		{"configure error", "configure: error: C compiler cannot create executables", "autotools-projects"},
		{"rustc code", "error[E0308]: mismatched types", "rust-language-notes"},
		{"r error", "Error in library(ols) : there is no package called 'ols'\nExecution halted", "r-language-notes"},
		{"sqlite locked", "Error: database is locked", "sqlite-recipes"},
		{"dns", "curl: (6) Could not resolve host: example.invalid", "dns-troubleshooting"},
		{"systemd", "Job for app.service failed because the control process exited", "systemd-units-services"},
		{"nginx", "nginx: [emerg] unknown directive \"frobnicate\"", "nginx-static-services"},
		{"docker daemon", "Cannot connect to the Docker daemon", "docker-containers"},
		{"postgres role", "psql: FATAL:  role \"app\" does not exist", "postgres-operations"},
		{"jq error", "jq: error: syntax error at line 1", "jq-recipes"},
		{"tar error", "tar: Error is not recoverable: exiting now", "archive-compression-formats"},
		{"regex", "grep: Invalid regular expression", "regex-practical"},
		{"awk", "awk: cmd. line:1: syntax error", "awk-pattern-recipes"},
		{"ssh", "ssh: connect to host 10.0.0.1 port 22: Connection refused", "ssh-client-essentials"},
		{"ffmpeg", "ffmpeg version 6.1\n[matroska] Invalid data found", "ffmpeg-recipes"},
		{"latex", "! LaTeX Error: File `missing.sty' not found.", "latex-documents"},
		{"conda", "PackagesNotFoundError: The following packages are not available", "python-conda-envs"},
		{"qemu", "qemu-system-x86_64: could not open disk image", "qemu-virtual-machines"},
		{"java", "Exception in thread \"main\" java.lang.NullPointerException", "java-language-notes"},
		{"ocaml", "File \"main.ml\", line 3, characters 4-9: Error: unbound value", "ocaml-language-notes"},
		{"cobol", "prog.cbl:5: Error: syntax error", "cobol-language-notes"},
		{"fortran", "Fortran runtime error: Index out of bounds", "fortran-language-notes"},
		{"priority: pattern beats binary", "bash: line 1: make: command not found\nMakefile:2: *** missing separator. Stop.", "make-builds"},
	}
	for _, c := range cases {
		name, _, ok := MatchError(c.output)
		if c.skill == "" {
			if ok {
				t.Errorf("%s: want no card, got %q", c.name, name)
			}
			continue
		}
		if !ok || name != c.skill {
			t.Errorf("%s: want card %q, got %q (ok=%v)", c.name, c.skill, name, ok)
		}
	}
}

// MatchError fires at most one card per failure: the first
// (most-specific) pattern wins; output never yields two skills.
func TestMatchErrorSingleCard(t *testing.T) {
	out := "psql: FATAL: role \"app\" does not exist\npermission denied\nmake: *** missing separator. Stop."
	name, _, ok := MatchError(out)
	if !ok {
		t.Fatal("compound failure must fire a card")
	}
	// First pattern in table order wins deterministically: the
	// make rule sorts before the permission and psql rules.
	if name != "make-builds" {
		t.Errorf("compound failure must fire the first-table card, got %q", name)
	}
}

// Card excerpts stay within the 40-line/4000-char budget, carry
// commands, pitfalls and verification, and point at the full skill.
func TestCardExcerpt(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sqlite-recipes", "make-builds", "tls-openssl-recipes", "r-language-notes", "docker-containers"} {
		card, ok := ix.CardExcerpt(name, "test failure", 0)
		if !ok {
			t.Fatalf("excerpt for %q must build", name)
		}
		lines := strings.Split(strings.TrimRight(card, "\n"), "\n")
		if len(lines) > CardMaxLines {
			t.Errorf("%s: excerpt %d lines exceeds %d", name, len(lines), CardMaxLines)
		}
		if len(card) > CardMaxChars+200 {
			t.Errorf("%s: excerpt %d chars exceeds budget", name, len(card))
		}
		for _, want := range []string{"## procedure", "## pitfalls", "## verification", "load_skill"} {
			if !strings.Contains(strings.ToLower(card), want) {
				t.Errorf("%s: excerpt must carry %q:\n%s", name, want, card)
			}
		}
		if !strings.Contains(card, "`") {
			t.Errorf("%s: card must carry exact commands in backticks", name)
		}
	}
	if _, ok := ix.CardExcerpt("no-such-skill", "x", 0); ok {
		t.Error("unknown skill must not build an excerpt")
	}
	if card, ok := ix.ErrorCardFor("Error: database is locked", 0); !ok || !strings.Contains(card, "sqlite-recipes") {
		t.Errorf("ErrorCardFor must match and excerpt, got %q ok=%v", card, ok)
	}
	if _, ok := ix.ErrorCardFor("all green", 0); ok {
		t.Error("clean output must not fire a card")
	}
}

// The on-demand pointer stays a one-liner: discovery without bloat.
func TestCardsPointerBrevity(t *testing.T) {
	p := CardsPointer()
	if len(strings.Split(strings.TrimRight(p, "\n"), "\n")) > 3 {
		t.Errorf("pointer must stay short, got %q", p)
	}
	if len(p) > 400 {
		t.Errorf("pointer must stay under 400 chars, got %d", len(p))
	}
}

// An unscoped empty search no longer dumps the whole catalog: the
// model gets guidance with the group list instead. Group-scoped
// empty queries still list the group.
func TestSearchEmptyQueryGuidance(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	search := SkillTools(ix, nil)[0]
	res, err := search.Invoke(ctx, tools.Invocation{Tool: "search_skills", Arguments: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != tools.StatusOK {
		t.Fatalf("guidance must succeed, got %+v", res)
	}
	if strings.Contains(res.Output, "- sqlite-recipes") {
		t.Errorf("empty query must not dump the catalog: %q", res.Output[:200])
	}
	if !strings.Contains(res.Output, "terminal") {
		t.Errorf("guidance must list groups, got %q", res.Output)
	}
	res, err = search.Invoke(ctx, tools.Invocation{Tool: "search_skills", Arguments: json.RawMessage(`{"group":"terminal"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != tools.StatusOK || !strings.Contains(res.Output, "shell-power-use") {
		t.Errorf("group listing must still work, got %+v", res)
	}
}
