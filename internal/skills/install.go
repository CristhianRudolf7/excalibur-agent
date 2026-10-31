package skills

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Staged entries copied on activation. Anything else a package
// ships is ignored, and nothing is ever executed during install
// (Section 12.8, step 1: download to a temporary folder without
// running anything).
var stagedEntries = []string{"SKILL.md", "scripts", "references", "assets"}

// Source stages one skill package into a temporary directory for
// validation. Staging never executes package content.
type Source interface {
	Stage() (dir string, cleanup func(), err error)
}

// DirSource stages a local directory (an unpacked registry entry,
// repository checkout or file).
type DirSource struct {
	// Dir is the package directory.
	Dir string
}

// Stage implements Source.
func (s DirSource) Stage() (string, func(), error) {
	tmp, err := os.MkdirTemp("", "skill-install-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	if err := copySkillTree(s.Dir, tmp); err != nil {
		cleanup()
		return "", nil, err
	}
	return tmp, cleanup, nil
}

// TarGzSource stages a .tar.gz package file (a registry download
// or an archive): extraction is confined to the temporary
// directory and rejects absolute paths, traversal and special
// entries.
type TarGzSource struct {
	// Path is the archive file.
	Path string
}

// Stage implements Source.
func (s TarGzSource) Stage() (string, func(), error) {
	f, err := os.Open(s.Path)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", nil, fmt.Errorf("skills: not a gzip archive: %w", err)
	}
	defer gz.Close()
	tmp, err := os.MkdirTemp("", "skill-install-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			cleanup()
			return "", nil, fmt.Errorf("skills: bad archive: %w", err)
		}
		name := filepath.Clean("/" + hdr.Name)[1:]
		if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(hdr.Name) {
			cleanup()
			return "", nil, fmt.Errorf("skills: archive entry escapes the package: %q", hdr.Name)
		}
		dst := filepath.Join(tmp, name)
		if !strings.HasPrefix(dst, tmp+string(os.PathSeparator)) && dst != tmp {
			cleanup()
			return "", nil, fmt.Errorf("skills: archive entry escapes the package: %q", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o755); err != nil {
				cleanup()
				return "", nil, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				cleanup()
				return "", nil, err
			}
			out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				cleanup()
				return "", nil, err
			}
			_, err = io.CopyN(out, tr, hdr.Size)
			cerr := out.Close()
			if err != nil || cerr != nil {
				cleanup()
				return "", nil, fmt.Errorf("skills: cannot extract %q", hdr.Name)
			}
		default:
			cleanup()
			return "", nil, fmt.Errorf("skills: unsupported archive entry %q", hdr.Name)
		}
	}
	return tmp, cleanup, nil
}

// InstallSummary is the user-facing summary of an install
// (Section 12.8, step 3): what the skill holds (instructions,
// scripts, references), which tools it uses and which
// requirements are missing.
type InstallSummary struct {
	Name          string
	Version       string
	Group         string
	Description   string
	Scripts       []string
	References    []string
	Assets        []string
	Tools         []string
	UnknownTools  []string
	MissingBin    []string
	MissingEnv    []string
	VersionNote   string
	InstalledPath string
}

// InstallRecord pins one installed skill by hash and version
// (Section 12.8, step 5): a later content change is detected,
// revalidated and recorded as a new version.
type InstallRecord struct {
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Hash      string    `json:"hash"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DriftKind classifies one installed-content drift finding.
type DriftKind string

// Drift kinds.
const (
	DriftChanged  DriftKind = "changed"
	DriftRejected DriftKind = "rejected"
	DriftRemoved  DriftKind = "removed"
)

// Drift is one installed skill whose disk content no longer
// matches its pin.
type Drift struct {
	Kind        DriftKind
	Name        string
	OldHash     string
	NewHash     string
	Detail      string
	Diagnostics []Diagnostic
}

const installsFile = ".excalibur-installs.json"

// Install validates a staged package and activates it in the
// installed-packages root (Section 12.8): format validation with
// a visible diagnosis on failure, checks of the declared
// requires and tools, a summary for the user, activation, and
// hash pinning. Nothing staged is ever executed.
func (ix *Index) Install(stagedDir, installedRoot string, toolOK func(string) bool) (InstallSummary, error) {
	skillDir, err := locateSkillDir(stagedDir)
	if err != nil {
		return InstallSummary{}, err
	}
	raw, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		return InstallSummary{}, &Diagnostic{Skill: filepath.Base(skillDir), Path: skillDir, Code: CodeMissingFile, Detail: "no SKILL.md in the staged package"}
	}
	sf, err := ParseSkillFile(raw)
	if err != nil {
		return InstallSummary{}, &Diagnostic{Skill: filepath.Base(skillDir), Path: skillDir, Code: CodeBadFrontmatter, Detail: err.Error()}
	}
	// The staged directory name is meaningless (a temporary
	// folder), so the skill name always comes from the frontmatter
	// (Section 12.1: the name must be usable on its own). The
	// directory/name match is enforced when the skill lands in a
	// real root at Scan time.
	if !validName(sf.Frontmatter.Name) {
		return InstallSummary{}, &Diagnostic{Skill: filepath.Base(skillDir), Path: skillDir, Code: CodeBadName, Detail: fmt.Sprintf("frontmatter name %q must be 1-64 chars of lowercase letters, digits and hyphens, without leading, trailing or repeated hyphens", sf.Frontmatter.Name)}
	}
	name := sf.Frontmatter.Name
	// Install validates the format (Section 12.1: name,
	// description, compatibility, group, state). Unknown tools
	// are not a rejection: tools land from other subsystems, so
	// they are reported in the summary for the user instead.
	if diags := Validate(name, skillDir, sf, groupOK, nil); len(diags) > 0 {
		return InstallSummary{}, diags[0]
	}
	sum := summarize(name, skillDir, sf, toolOK)
	if installedRoot == "" {
		return InstallSummary{}, fmt.Errorf("skills: no installed-packages root configured")
	}
	dest := filepath.Join(installedRoot, name)
	note := "new"
	if prev, ok := readRecord(installedRoot, name); ok {
		h := hashContent(raw)
		if prev.Hash == h {
			note = "same-hash"
		} else {
			note = "updated"
		}
		_ = prev
	}
	if err := os.MkdirAll(installedRoot, 0o755); err != nil {
		return InstallSummary{}, err
	}
	tmpDest := dest + ".new"
	_ = os.RemoveAll(tmpDest)
	if err := copyInstallTree(skillDir, tmpDest); err != nil {
		return InstallSummary{}, err
	}
	// Replacing an existing version must be atomic: move the old
	// directory aside first, then rename into place.
	prevDir := dest + ".prev"
	_ = os.RemoveAll(prevDir)
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, prevDir); err != nil {
			_ = os.RemoveAll(tmpDest)
			return InstallSummary{}, err
		}
	}
	if err := os.Rename(tmpDest, dest); err != nil {
		_ = os.Rename(prevDir, dest)
		_ = os.RemoveAll(tmpDest)
		return InstallSummary{}, err
	}
	_ = os.RemoveAll(prevDir)
	sum.VersionNote = note
	sum.InstalledPath = dest
	if err := writeRecord(installedRoot, InstallRecord{Name: name, Version: sum.Version, Hash: hashContent(raw), UpdatedAt: time.Now().UTC()}); err != nil {
		return InstallSummary{}, err
	}
	if err := ix.Scan(); err != nil {
		return InstallSummary{}, err
	}
	return sum, nil
}

// locateSkillDir finds the skill directory inside a staged
// package: either the stage root itself (SKILL.md at top) or its
// single subdirectory.
func locateSkillDir(staged string) (string, error) {
	if st, err := os.Stat(filepath.Join(staged, "SKILL.md")); err == nil && !st.IsDir() {
		return staged, nil
	}
	ents, err := os.ReadDir(staged)
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, e := range ents {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(staged, e.Name()))
		}
	}
	if len(dirs) == 1 {
		if _, err := os.Stat(filepath.Join(dirs[0], "SKILL.md")); err == nil {
			return dirs[0], nil
		}
	}
	return "", &Diagnostic{Skill: filepath.Base(staged), Path: staged, Code: CodeMissingFile, Detail: "staged package holds no single skill directory with SKILL.md"}
}

// summarize builds the user-facing install summary, checking the
// declared requires (binaries on PATH, environment variables)
// and tools.
func summarize(name, dir string, sf SkillFile, toolOK func(string) bool) InstallSummary {
	fm := sf.Frontmatter
	sum := InstallSummary{
		Name: name, Version: fm.Metadata.Excalibur.Version, Group: fm.Metadata.Excalibur.Group,
		Description: strings.TrimSpace(fm.Description),
		Tools:       append([]string(nil), fm.Metadata.Excalibur.Tools...),
	}
	sum.Scripts = listFiles(filepath.Join(dir, "scripts"))
	sum.References = listFiles(filepath.Join(dir, "references"))
	sum.Assets = listFiles(filepath.Join(dir, "assets"))
	for _, b := range fm.Metadata.Excalibur.Requires.Bin {
		if _, err := exec.LookPath(b); err != nil {
			sum.MissingBin = append(sum.MissingBin, b)
		}
	}
	for _, e := range fm.Metadata.Excalibur.Requires.Env {
		if _, ok := os.LookupEnv(e); !ok {
			sum.MissingEnv = append(sum.MissingEnv, e)
		}
	}
	if toolOK != nil {
		for _, t := range fm.Metadata.Excalibur.Tools {
			if !toolOK(t) {
				sum.UnknownTools = append(sum.UnknownTools, t)
			}
		}
	}
	return sum
}

// listFiles returns the relative file names under dir, or nil when
// the optional folder is absent.
func listFiles(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// CheckInstalled detects content drift of installed skills:
// changed hashes are revalidated and, when valid, recorded as new
// versions; invalid content is reported with its diagnosis and
// left untouched (Section 12.8, step 5; Section 12.2 invalidation).
func (ix *Index) CheckInstalled(installedRoot string, toolOK func(string) bool) []Drift {
	records := readAllRecords(installedRoot)
	ents, err := os.ReadDir(installedRoot)
	if err != nil {
		return nil
	}
	var out []Drift
	seen := map[string]bool{}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		seen[name] = true
		raw, err := os.ReadFile(filepath.Join(installedRoot, name, "SKILL.md"))
		if err != nil {
			continue
		}
		rec, ok := records[name]
		if !ok {
			continue
		}
		if hashContent(raw) == rec.Hash {
			continue
		}
		sf, err := ParseSkillFile(raw)
		if err != nil {
			out = append(out, Drift{Kind: DriftRejected, Name: name, OldHash: rec.Hash, NewHash: hashContent(raw),
				Detail:      "changed content fails validation; pin kept",
				Diagnostics: []Diagnostic{{Skill: name, Code: CodeBadFrontmatter, Detail: err.Error()}}})
			continue
		}
		if diags := Validate(name, filepath.Join(installedRoot, name), sf, groupOK, toolOK); len(diags) > 0 {
			out = append(out, Drift{Kind: DriftRejected, Name: name, OldHash: rec.Hash, NewHash: hashContent(raw),
				Detail: "changed content fails validation; pin kept", Diagnostics: diags})
			continue
		}
		_ = writeRecord(installedRoot, InstallRecord{Name: name, Version: sf.Frontmatter.Metadata.Excalibur.Version, Hash: hashContent(raw), UpdatedAt: time.Now().UTC()})
		out = append(out, Drift{Kind: DriftChanged, Name: name, OldHash: rec.Hash, NewHash: hashContent(raw), Detail: "content change revalidated and recorded as a new version"})
	}
	for name := range records {
		if !seen[name] {
			out = append(out, Drift{Kind: DriftRemoved, Name: name, Detail: "installed skill directory is gone"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func recordsPath(root string) string { return filepath.Join(root, installsFile) }

func readAllRecords(root string) map[string]InstallRecord {
	out := map[string]InstallRecord{}
	data, err := os.ReadFile(recordsPath(root))
	if err != nil {
		return out
	}
	var list []InstallRecord
	if err := json.Unmarshal(data, &list); err != nil {
		return out
	}
	for _, r := range list {
		out[r.Name] = r
	}
	return out
}

func readRecord(root, name string) (InstallRecord, bool) {
	r, ok := readAllRecords(root)[name]
	return r, ok
}

func writeRecord(root string, rec InstallRecord) error {
	all := readAllRecords(root)
	all[rec.Name] = rec
	list := make([]InstallRecord, 0, len(all))
	for _, r := range all {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(recordsPath(root), append(data, '\n'), 0o644)
}

// copySkillTree copies a candidate tree into a staging area,
// skipping symlinks so staged content cannot escape.
func copySkillTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "." {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		return copyFile(p, out, 0o644)
	})
}

// copyInstallTree copies only the known skill entries on
// activation (SKILL.md plus the optional folders).
func copyInstallTree(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range stagedEntries {
		s := filepath.Join(src, e)
		st, err := os.Lstat(s)
		if err != nil {
			if e == "SKILL.md" {
				return err
			}
			continue
		}
		if st.Mode()&fs.ModeSymlink != 0 {
			continue
		}
		if st.IsDir() {
			if err := copySkillTree(s, filepath.Join(dst, e)); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(s, filepath.Join(dst, e), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	cerr := out.Close()
	if err != nil {
		return err
	}
	return cerr
}
