package skills

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// The ablation switch defaults to helpful: unset env means auto,
// explicit values select off/on/auto, and unknown values stay safe
// by falling back to auto.
func TestLibraryModeParsing(t *testing.T) {
	if DefaultLibraryMode() != LibraryAuto {
		t.Fatalf("default mode must be auto, got %q", DefaultLibraryMode())
	}
	for in, want := range map[string]LibraryMode{
		"": LibraryAuto, "auto": LibraryAuto, "AUTO": LibraryAuto,
		"on": LibraryOn, "On": LibraryOn,
		"off": LibraryOff, "OFF": LibraryOff,
	} {
		got := LibraryModeFromEnv(func(string) string { return in })
		if got != want {
			t.Errorf("env %q: want %q, got %q", in, want, got)
		}
		if _, err := ParseLibraryMode(in); in != "" && err != nil && want != LibraryAuto {
			t.Errorf("ParseLibraryMode(%q) unexpected error: %v", in, err)
		}
	}
	if _, err := ParseLibraryMode("sometimes"); err == nil {
		t.Error("unknown mode must error")
	}
	if LibraryModeFromEnv(nil) != LibraryAuto {
		t.Error("nil getenv must default to auto")
	}
	if LibraryOff.CatalogEnabled() || !LibraryOn.CatalogEnabled() || !LibraryAuto.CatalogEnabled() {
		t.Error("catalog must be enabled in on/auto only")
	}
	if !LibraryAuto.AutoSuggestEnabled() || LibraryOn.AutoSuggestEnabled() || LibraryOff.AutoSuggestEnabled() {
		t.Error("auto-suggest must run in auto mode only")
	}
}

// Off disables everything, on wires the catalog without injecting
// suggestions, auto adds the task-start suggestions.
func TestLibraryModeGating(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	cfg := CatalogConfig{WindowTokens: 200000, Pertinent: DefaultPertinentForRole("programming")}
	if got := ix.RenderCatalogForMode(cfg, LibraryOff); got != "" {
		t.Errorf("off must render no catalog, got %d chars", len(got))
	}
	if got := ix.RenderCatalogForMode(cfg, LibraryOn); got == "" {
		t.Error("on must render the catalog")
	}
	if got := ix.SuggestForMode("debug failing test", "m", 3, LibraryOff); len(got) != 0 {
		t.Error("off must not suggest")
	}
	if got := ix.SuggestForMode("debug failing test", "m", 3, LibraryOn); len(got) != 0 {
		t.Error("on must not auto-suggest")
	}
	if got := ix.SuggestForMode("debug failing test", "m", 3, LibraryAuto); len(got) == 0 {
		t.Error("auto must suggest")
	}
	if got := ix.SuggestWithEnvForMode("debug failing test", EnvSignals{}, "m", 3, LibraryOn); len(got) != 0 {
		t.Error("on must not env-suggest")
	}
	if got := ix.SearchForMode("rust", "", LibraryOff); len(got) != 0 {
		t.Error("off must not search")
	}
	if got := ix.SearchForMode("rust", "", LibraryOn); len(got) == 0 {
		t.Error("on must search")
	}
	if _, err := ix.LoadForMode("shell-power-use", LibraryOff); err == nil {
		t.Error("off must refuse loads")
	} else if !strings.Contains(err.Error(), CodeLibraryOff) {
		t.Errorf("off load must cite %q, got %v", CodeLibraryOff, err)
	}
	if _, err := ix.LoadForMode("shell-power-use", LibraryOn); err != nil {
		t.Errorf("on must load: %v", err)
	}
}

// The new reference library follows the skill template: every new
// skill states when to use it, a procedure, pitfalls and
// verification steps; every linked references/ file exists.
func TestNewSkillsFollowTemplate(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	newGroups := map[string]bool{
		"terminal": true, "packaging": true, "build": true, "languages": true,
		"networking": true, "containers": true, "databases": true, "data": true,
		"ml": true, "media": true,
	}
	count := 0
	for _, e := range ix.All() {
		if !newGroups[e.Group] {
			continue
		}
		count++
		sections := map[string]bool{}
		for _, s := range SectionNames(e.Body) {
			sections[normHeading(s)] = true
		}
		for _, want := range []string{"when to use", "procedure", "pitfalls", "verification"} {
			if !sections[want] {
				t.Errorf("skill %q misses section %q (has %v)", e.Name, want, SectionNames(e.Body))
			}
		}
		if e.Body != "" && strings.Contains(e.Body, "references/") && len(e.Refs) == 0 {
			t.Errorf("skill %q links references/ files but ships none", e.Name)
		}
		if e.Description == "" || len(e.Description) > MaxShortDescription+40 {
			t.Errorf("skill %q description should stay catalog-compact, got %d chars", e.Name, len(e.Description))
		}
		if len(e.Tools) == 0 || !hasTool(e.Tools, "exec") {
			t.Errorf("skill %q should declare exec, got %v", e.Name, e.Tools)
		}
		for _, tool := range e.Tools {
			if !KnownTool(tool) {
				t.Errorf("skill %q declares unknown tool %q", e.Name, tool)
			}
		}
	}
	if count != 54 {
		t.Errorf("want 54 skills in the new groups, got %d", count)
	}
}

func hasTool(list []string, want string) bool {
	for _, x := range list {
		if x == want {
			return true
		}
	}
	return false
}

// Environment signals steer suggestions: a Rust checkout surfaces
// cargo skills, a Python file surfaces pip skills, with no
// instruction text at all.
func TestSuggestWithEnv(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		query string
		env   EnvSignals
		want  string
	}{
		{"rust checkout", "build and test the project",
			EnvSignals{Files: []string{"Cargo.toml", "src/main.rs"}, Bins: []string{"cargo"}}, "rust-cargo-workflows"},
		{"python offline", "install dependencies without network",
			EnvSignals{Files: []string{"requirements.txt", "app.py"}, Bins: []string{"pip"}}, "python-pip-uv-venv"},
		{"go checkout", "run the test suite",
			EnvSignals{Files: []string{"go.mod", "main.go"}}, "go-modules-toolchain"},
		{"node checkout", "install and build",
			EnvSignals{Files: []string{"package.json"}}, "node-npm-packaging"},
		{"cmake project", "configure and compile",
			EnvSignals{Files: []string{"CMakeLists.txt", "main.cpp"}}, "cmake-projects"},
		{"tls debug", "certificate verify failed",
			EnvSignals{Bins: []string{"openssl", "curl"}}, "tls-openssl-recipes"},
		{"env only", "",
			EnvSignals{Files: []string{"docker-compose.yml"}}, "docker-containers"},
	}
	for _, c := range cases {
		got := ix.SuggestWithEnv(c.query, c.env, "cheap-model", 3)
		found := false
		for _, s := range got {
			if s.Name == c.want {
				found = true
				break
			}
		}
		if !found {
			names := []string{}
			for _, s := range got {
				names = append(names, s.Name)
			}
			t.Errorf("%s: want %q in top 3, got %v", c.name, c.want, names)
		}
	}
}

// The synonym leg helps terse model phrasing: "py packaging offline
// wheels" is Python/pip/uv vocabulary even though no catalog text
// says "py".
func TestSuggestSynonyms(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	got := ix.Suggest("py packaging offline wheels", "cheap-model", 3)
	found := false
	for _, s := range got {
		if s.Name == "python-pip-uv-venv" {
			found = true
		}
	}
	if !found {
		names := []string{}
		for _, s := range got {
			names = append(names, s.Name)
		}
		t.Errorf("synonym query should suggest python-pip-uv-venv, got %v", names)
	}
	if got := ix.Search("py packaging", ""); len(got) == 0 {
		t.Error("synonym search should find something")
	}
}

// Suggestion reasons cite content words, not chatty filler, and a
// realistic small-model instruction surfaces the task-relevant
// skills.
func TestSuggestQualityMakefileTask(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	query := "Create hello.py that prints hello, add a Makefile with a run target that runs it, then run make run and report the exact output"
	env := EnvSignals{Bins: []string{"make", "python3", "gcc", "git"}}
	got := ix.SuggestWithEnv(query, env, "cheap-model", 3)
	names := []string{}
	for _, s := range got {
		names = append(names, s.Name)
		if s.Why == "" {
			t.Errorf("suggestion %q needs a reason", s.Name)
		}
		for _, w := range []string{" a,", ", a", " with,", " it", ", it,"} {
			if strings.Contains(","+s.Why+",", w) {
				t.Errorf("reason cites filler %q: %q", strings.Trim(w, " ,"), s.Why)
			}
		}
	}
	want := map[string]bool{"make-builds": true, "python-pip-uv-venv": true, "python-language-notes": true, "shell-power-use": true}
	found := false
	for _, n := range names {
		if want[n] {
			found = true
		}
	}
	if !found {
		t.Errorf("makefile/python task should surface a task-relevant skill, got %v", names)
	}
}

// A git task in a fresh checkout surfaces git skills in the top 3
// even though a filename collides with an unrelated skill name.
func TestSuggestQualityGitTask(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	query := "Create notes.txt with one line hello-notes, commit it with git using a fixed message, then show git log --oneline and report the commit line"
	env := EnvSignals{Files: []string{".git"}, Bins: []string{"make", "cargo", "sqlite3", "jq", "tmux", "node"}}
	got := ix.SuggestWithEnv(query, env, "cheap-model", 3)
	names := []string{}
	for _, s := range got {
		names = append(names, s.Name)
	}
	git := map[string]bool{"git-internals": true, "git-history-surgery": true, "version-control-hygiene": true}
	hits := 0
	for _, n := range names {
		if git[n] {
			hits++
		}
	}
	if hits < 2 {
		t.Errorf("git task should surface mostly git skills in top 3, got %v", names)
	}
}

// Focused loads serve one section or one reference file; unknown
// names guide instead of failing silently.
func TestLoadSectionAndRef(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	full, err := ix.Load("shell-power-use")
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Sections) < 4 || len(full.Refs) == 0 {
		t.Fatalf("shell-power-use should expose sections and refs, got %+v", full)
	}
	sec, err := ix.LoadSection("shell-power-use", "pitfalls")
	if err != nil {
		t.Fatal(err)
	}
	if sec.Section != "Pitfalls" {
		t.Errorf("want section Pitfalls, got %q", sec.Section)
	}
	if len(sec.Body) >= len(full.Body) {
		t.Error("section body must be shorter than the full body")
	}
	if strings.Contains(sec.Body, "## Procedure") {
		t.Error("section body must not leak other sections")
	}
	if _, err := ix.LoadSection("shell-power-use", "frobnicate"); err == nil {
		t.Error("unknown section must error")
	} else if !strings.Contains(err.Error(), CodeBadSection) {
		t.Errorf("section error must cite %q, got %v", CodeBadSection, err)
	}
	ref, err := ix.LoadRef("shell-power-use", "pipeline-recipes")
	if err != nil {
		t.Fatal(err)
	}
	if ref.RefName != "pipeline-recipes.md" || !strings.Contains(ref.Body, "xargs") {
		t.Errorf("ref load should serve pipeline-recipes.md, got %+v", ref)
	}
	if _, err := ix.LoadRef("shell-power-use", "missing"); err == nil {
		t.Error("unknown ref must error")
	} else if !strings.Contains(err.Error(), CodeBadRef) {
		t.Errorf("ref error must cite %q, got %v", CodeBadRef, err)
	}
}

// References also work for project/user-installed skills on disk,
// not just the embedded catalog.
func TestLoadRefFromDisk(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "disk-skill")
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	front := "---\nname: disk-skill\ndescription: Disk skill for ref tests.\nmetadata:\n  excalibur:\n    group: planning\n    version: 1.0.0\n---\n# disk\n\n## Alpha\n\nFirst.\n\n## Beta\n\nSecond.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(front), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "references", "notes.md"), []byte("# notes\n\nDepth.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ix := NewIndex(Roots{User: base}, nil)
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	sec, err := ix.LoadSection("disk-skill", "beta")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sec.Body, "Second.") {
		t.Errorf("section load should serve Beta, got %q", sec.Body)
	}
	ref, err := ix.LoadRef("disk-skill", "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ref.Body, "Depth.") {
		t.Errorf("ref load should serve notes.md, got %q", ref.Body)
	}
}

// CollectEnvSignals reads top-level project files and probes PATH
// without failing on unreadable input.
func TestCollectEnvSignals(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"Cargo.toml", "src", "README.md"} {
		if strings.Contains(f, ".") {
			if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		} else if err := os.Mkdir(filepath.Join(dir, f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	look := func(name string) (string, error) {
		if name == "cargo" || name == "make" {
			return "/usr/bin/" + name, nil
		}
		return "", os.ErrNotExist
	}
	env := CollectEnvSignals(dir, look)
	if len(env.Files) != 3 {
		t.Errorf("want 3 project entries, got %v", env.Files)
	}
	if len(env.Bins) != 2 {
		t.Errorf("want 2 probed binaries, got %v", env.Bins)
	}
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	got := ix.SuggestWithEnv("", env, "m", 3)
	found := false
	for _, s := range got {
		if s.Name == "rust-cargo-workflows" {
			found = true
		}
	}
	if !found {
		t.Errorf("project signals alone should surface rust-cargo-workflows, got %+v", got)
	}
	if got := CollectEnvSignals(filepath.Join(dir, "missing"), nil); len(got.Files) != 0 {
		t.Errorf("missing dir must yield no files, got %v", got.Files)
	}
}

// Compact catalogs stay grouped and budgeted but carry names only.
func TestCompactCatalog(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	out := ix.RenderCatalog(CatalogConfig{
		WindowTokens: 200000, Pertinent: DefaultPertinentForRole("programming"), Compact: true,
	})
	if !strings.Contains(out, "[planning]") {
		t.Fatal("compact catalog must keep group headers")
	}
	for _, ln := range strings.Split(out, "\n") {
		if !strings.HasPrefix(ln, "- ") {
			continue
		}
		if strings.Contains(ln, ": ") {
			t.Errorf("compact skill lines must carry no description: %q", ln)
		}
	}
	if got := EstimateTokens(out); got >= EstimateTokens(ix.RenderCatalog(CatalogConfig{WindowTokens: 200000, Pertinent: DefaultPertinentForRole("programming")})) {
		t.Error("compact catalog must be smaller than the full one")
	}
	if got := EstimateTokens(out); got > CatalogBudget(200000) {
		t.Errorf("compact catalog %d tokens exceeds budget %d", got, CatalogBudget(200000))
	}
}

// load_skill serves sections and refs through the tool surface; an
// empty search query lists the group instead of erroring.
func TestSkillToolsSectionsAndRefs(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, load := SkillTools(ix, nil)[0], SkillTools(ix, nil)[1]
	res, err := load.Invoke(ctx, tools.Invocation{Tool: "load_skill", Arguments: json.RawMessage(`{"name":"shell-power-use","section":"pitfalls"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != tools.StatusOK || !strings.Contains(res.Output, "Unquoted variables") {
		t.Errorf("section load must serve Pitfalls, got %+v", res)
	}
	if !strings.Contains(res.Output, "Other sections of") {
		t.Errorf("section load must point at sibling sections: %q", res.Output)
	}
	res, err = load.Invoke(ctx, tools.Invocation{Tool: "load_skill", Arguments: json.RawMessage(`{"name":"shell-power-use","ref":"pipeline-recipes.md"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != tools.StatusOK || !strings.Contains(res.Output, "xargs") {
		t.Errorf("ref load must serve the reference, got %+v", res)
	}
	res, err = load.Invoke(ctx, tools.Invocation{Tool: "load_skill", Arguments: json.RawMessage(`{"name":"shell-power-use","section":"nope"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != tools.StatusInputError || !strings.Contains(res.Output, CodeBadSection) {
		t.Errorf("bad section must be an input error citing %q, got %+v", CodeBadSection, res)
	}
	search := SkillTools(ix, nil)[0]
	res, err = search.Invoke(ctx, tools.Invocation{Tool: "search_skills", Arguments: json.RawMessage(`{"group":"terminal"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != tools.StatusOK || !strings.Contains(res.Output, "shell-power-use") {
		t.Errorf("query-less search must list the terminal group, got %+v", res)
	}
}
