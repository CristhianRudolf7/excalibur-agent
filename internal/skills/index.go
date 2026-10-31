package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Root names in precedence order, highest first (Section 12.2):
// project skills (.agents/skills inside the project root), user
// skills, installed packages, then the built-in system skills. When
// two roots hold the same name, the higher-precedence one wins and
// the conflict is recorded.
const (
	RootProject   = "project"
	RootUser      = "user"
	RootInstalled = "installed"
	RootBuiltin   = "builtin"
)

// Roots holds the filesystem skill directories backing the index.
// Each entry is the directory that directly contains one folder per
// skill; empty means that root is absent. The built-in root comes
// from the embedded catalog (see builtin.go), not from disk.
type Roots struct {
	Project   string
	User      string
	Installed string
}

// OrderedRoots returns the configured filesystem roots from
// highest to lowest precedence.
func (r Roots) OrderedRoots() []struct {
	Name string
	Dir  string
} {
	return []struct {
		Name string
		Dir  string
	}{
		{RootProject, r.Project},
		{RootUser, r.User},
		{RootInstalled, r.Installed},
	}
}

// Entry is one indexed skill (Section 12.2): name, description,
// group, path, content hash, version, state, platforms and
// usage/result statistics.
type Entry struct {
	Name         string
	Description  string
	Group        string
	Path         string
	Root         string
	Hash         string
	Version      string
	State        State
	Platforms    []string
	Tools        []string
	AllowedTools []string
	Requires     Requires
	Display      Display
	Headings     []string
	Body         string
	// Refs names the references/ files shipped with the skill
	// (Section 12.5 depth material): SKILL.md links them by name
	// and load_skill serves them on demand without embedding them
	// in the prompt.
	Refs []string
}

// Conflict records a shadowing: two roots held the same skill name
// and the higher-precedence root won (Section 12.2).
type Conflict struct {
	Name       string
	WinnerRoot string
	WinnerPath string
	LoserRoot  string
	LoserPath  string
}

// VectorScorer is the optional vector leg of suggestion and search
// (Section 12.4: BM25 plus vectors). Nil means lexical scoring
// only. The memory milestone (H11) plugs the embedding-backed
// scorer in here without changing the selection algorithm.
type VectorScorer interface {
	Score(query string, e *Entry) float64
}

// Index is the RAM skill index: discovery, validation, grouping,
// suggestion and loading (Sections 12.1-12.4). All methods are safe
// for concurrent use.
type Index struct {
	mu          sync.RWMutex
	roots       Roots
	builtin     fs.FS
	entries     map[string]*Entry
	conflicts   []Conflict
	diagnostics []Diagnostic
	stats       map[string]map[string]*modelStats
	disabled    map[string]map[string]bool
	vector      VectorScorer
	toolOK      func(string) bool
}

// modelStats counts task outcomes with and without one skill for
// one model (Section 12.4, last paragraph).
type modelStats struct {
	WithTotal    int
	WithSuccess  int
	WithoutTotal int
	WithoutSucc  int
}

// Option configures an Index.
type Option func(*Index)

// WithVectorScorer plugs the vector leg of suggestion/search.
func WithVectorScorer(v VectorScorer) Option {
	return func(ix *Index) { ix.vector = v }
}

// WithToolCheck enables the excalibur.tools / allowed-tools
// cross-check at validation time.
func WithToolCheck(ok func(string) bool) Option {
	return func(ix *Index) { ix.toolOK = ok }
}

// NewIndex returns an empty index over roots backed by builtin.
// Call Scan to populate it.
func NewIndex(roots Roots, builtin fs.FS, opts ...Option) *Index {
	ix := &Index{
		roots:    roots,
		builtin:  builtin,
		entries:  map[string]*Entry{},
		stats:    map[string]map[string]*modelStats{},
		disabled: map[string]map[string]bool{},
	}
	for _, o := range opts {
		o(ix)
	}
	return ix
}

// groupOK reports whether id is a known catalog group.
func groupOK(id string) bool {
	_, ok := GroupByID(id)
	return ok
}

// hashContent returns the hex SHA-256 of raw SKILL.md bytes: the
// pin used to detect later changes (Sections 12.2, 12.8).
func hashContent(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// candidate is one skill directory found in a root.
type candidate struct {
	root string
	dir  string
	name string
}

// listDir returns one candidate per immediate child directory of
// dir, or nil when the root is absent.
func listDir(root, dir string) []candidate {
	if dir == "" {
		return nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []candidate
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		out = append(out, candidate{root: root, dir: filepath.Join(dir, e.Name()), name: e.Name()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// listBuiltin returns one candidate per embedded skill directory.
func listBuiltin(fsys fs.FS) []candidate {
	if fsys == nil {
		return nil
	}
	ents, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil
	}
	var out []candidate
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		// Each group directory holds one folder per skill.
		sub, err := fs.ReadDir(fsys, e.Name())
		if err != nil {
			continue
		}
		for _, s := range sub {
			if !s.IsDir() {
				continue
			}
			out = append(out, candidate{root: RootBuiltin, dir: e.Name() + "/" + s.Name(), name: s.Name()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// readCandidate loads the raw SKILL.md bytes of c.
func (ix *Index) readCandidate(c candidate) ([]byte, error) {
	if c.root == RootBuiltin {
		return fs.ReadFile(ix.builtin, c.dir+"/SKILL.md")
	}
	return os.ReadFile(filepath.Join(c.dir, "SKILL.md"))
}

// listRefs names the references/ files shipped with candidate c, in
// sorted order. A missing references directory means no depth
// material; only regular files are listed (no recursion).
func (ix *Index) listRefs(c candidate) []string {
	var names []string
	if c.root == RootBuiltin {
		if ix.builtin == nil {
			return nil
		}
		ents, err := fs.ReadDir(ix.builtin, c.dir+"/references")
		if err != nil {
			return nil
		}
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			n := e.Name()
			if strings.HasPrefix(n, ".") {
				continue
			}
			names = append(names, n)
		}
	} else {
		ents, err := os.ReadDir(filepath.Join(c.dir, "references"))
		if err != nil {
			return nil
		}
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			n := e.Name()
			if strings.HasPrefix(n, ".") {
				continue
			}
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// readRef loads one references/ file of an indexed entry. e must
// come from entries (its Path/Root locate the file).
func (ix *Index) readRef(e *Entry, ref string) ([]byte, error) {
	if e.Root == RootBuiltin {
		return fs.ReadFile(ix.builtin, e.Path+"/references/"+ref)
	}
	return os.ReadFile(filepath.Join(e.Path, "references", ref))
}

// Scan (re)builds the index from every root. Later changes are
// picked up by the watcher (watch.go) or by calling Scan again:
// a changed content hash invalidates the entry, revalidates it
// and refreshes the lexical state. Scan is deterministic: roots
// resolve in precedence order and names sort before merging.
func (ix *Index) Scan() error {
	var cands []candidate
	for _, r := range ix.roots.OrderedRoots() {
		cands = append(cands, listDir(r.Name, r.Dir)...)
	}
	cands = append(cands, listBuiltin(ix.builtin)...)

	type loaded struct {
		c    candidate
		raw  []byte
		file SkillFile
	}
	byName := map[string][]loaded{}
	var diags []Diagnostic
	for _, c := range cands {
		raw, err := ix.readCandidate(c)
		if err != nil {
			diags = append(diags, Diagnostic{Skill: c.name, Path: c.dir, Code: CodeMissingFile, Detail: "no SKILL.md in the skill directory"})
			continue
		}
		sf, err := ParseSkillFile(raw)
		if err != nil {
			diags = append(diags, Diagnostic{Skill: c.name, Path: c.dir, Code: CodeBadFrontmatter, Detail: err.Error()})
			continue
		}
		byName[c.name] = append(byName[c.name], loaded{c: c, raw: raw, file: sf})
	}

	entries := make(map[string]*Entry, len(byName))
	var conflicts []Conflict
	for name, ls := range byName {
		// Highest precedence first: the configured root order is
		// already project > user > installed, with builtin last.
		winner := ls[0]
		for _, l := range ls[1:] {
			if precedes(l.c.root, winner.c.root) {
				winner = l
			}
		}
		toolOK := ix.toolOK
		file := Validate(winner.c.name, winner.c.dir, winner.file, groupOK, toolOK)
		fm := winner.file.Frontmatter
		st := State(fm.Metadata.Excalibur.State)
		if st == "" {
			st = StateActive
		}
		e := &Entry{
			Name:         ifValidName(name, fm.Name),
			Description:  strings.TrimSpace(fm.Description),
			Group:        fm.Metadata.Excalibur.Group,
			Path:         winner.c.dir,
			Root:         winner.c.root,
			Hash:         hashContent(winner.raw),
			Version:      fm.Metadata.Excalibur.Version,
			State:        st,
			Platforms:    append([]string(nil), fm.Metadata.Excalibur.Platforms...),
			Tools:        append([]string(nil), fm.Metadata.Excalibur.Tools...),
			AllowedTools: append([]string(nil), fm.AllowedTools...),
			Requires:     fm.Metadata.Excalibur.Requires,
			Display:      fm.Metadata.Excalibur.Display,
			Headings:     append([]string(nil), winner.file.Headings...),
			Body:         winner.file.Body,
			Refs:         ix.listRefs(winner.c),
		}
		entries[name] = e
		// Diagnostics of the winner stay visible even though the
		// entry exists (an invalid skill is excluded below).
		for _, d := range file {
			d.Skill = name
			d.Path = winner.c.dir
			diags = append(diags, d)
		}
		if len(file) > 0 {
			delete(entries, name)
		}
		for _, l := range ls {
			if l.c.dir == winner.c.dir {
				continue
			}
			conflicts = append(conflicts, Conflict{
				Name: name, WinnerRoot: winner.c.root, WinnerPath: winner.c.dir,
				LoserRoot: l.c.root, LoserPath: l.c.dir,
			})
		}
	}
	sort.Slice(diags, func(i, j int) bool {
		if diags[i].Skill != diags[j].Skill {
			return diags[i].Skill < diags[j].Skill
		}
		return diags[i].Code < diags[j].Code
	})
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Name != conflicts[j].Name {
			return conflicts[i].Name < conflicts[j].Name
		}
		return conflicts[i].LoserRoot < conflicts[j].LoserRoot
	})

	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.entries = entries
	ix.conflicts = conflicts
	ix.diagnostics = diags
	return nil
}

// precedes reports whether root a outranks root b.
func precedes(a, b string) bool {
	rank := map[string]int{RootProject: 0, RootUser: 1, RootInstalled: 2, RootBuiltin: 3}
	ra, oka := rank[a]
	rb, okb := rank[b]
	if !oka || !okb {
		return false
	}
	return ra < rb
}

// ifValidName keeps the canonical directory name when the
// frontmatter name is unusable, so diagnostics still point at a
// stable key.
func ifValidName(dir, fm string) string {
	if validName(fm) {
		return fm
	}
	return dir
}

// Get returns the entry for name, or false when absent (unknown or
// excluded as invalid).
func (ix *Index) Get(name string) (*Entry, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	e, ok := ix.entries[name]
	if !ok {
		return nil, false
	}
	return e.clone(), true
}

// Names returns the indexed skill names in sorted order.
func (ix *Index) Names() []string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	out := make([]string, 0, len(ix.entries))
	for n := range ix.entries {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// All returns a snapshot copy of every entry, sorted by name.
func (ix *Index) All() []*Entry {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	out := make([]*Entry, 0, len(ix.entries))
	for _, e := range ix.entries {
		out = append(out, e.clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Diagnostics returns the visible validation findings of the last
// scan in stable order (Section 12.1).
func (ix *Index) Diagnostics() []Diagnostic {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return append([]Diagnostic(nil), ix.diagnostics...)
}

// Conflicts returns the recorded shadowing conflicts in stable
// order (Section 12.2).
func (ix *Index) Conflicts() []Conflict {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return append([]Conflict(nil), ix.conflicts...)
}

// SetState moves a skill between lifecycle states (Section 12.2:
// active, draft, obsolete, archived).
func (ix *Index) SetState(name string, st State) error {
	if !ValidState(st) {
		return &Diagnostic{Skill: name, Code: CodeBadState, Detail: "unknown state " + string(st)}
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	e, ok := ix.entries[name]
	if !ok {
		return &Diagnostic{Skill: name, Code: CodeMissingFile, Detail: "skill is not indexed"}
	}
	e.State = st
	return nil
}

func (e *Entry) clone() *Entry {
	if e == nil {
		return nil
	}
	c := *e
	c.Platforms = append([]string(nil), e.Platforms...)
	c.Tools = append([]string(nil), e.Tools...)
	c.AllowedTools = append([]string(nil), e.AllowedTools...)
	c.Requires.Bin = append([]string(nil), e.Requires.Bin...)
	c.Requires.Env = append([]string(nil), e.Requires.Env...)
	c.Headings = append([]string(nil), e.Headings...)
	c.Refs = append([]string(nil), e.Refs...)
	return &c
}
