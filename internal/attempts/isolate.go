package attempts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// PrepareCopies creates N isolated working copies of src and returns
// their paths plus a cleanup func. It prefers a git worktree per
// attempt when src is a git repository and the git CLI is available;
// otherwise it falls back to a filesystem overlay copy. An empty src
// yields N fresh empty directories. The caller applies only the winner
// back to src with ApplyWinner.
func PrepareCopies(src string, n int, baseTmp string) (dirs []string, cleanup func(), err error) {
	if n < 1 {
		n = 1
	}
	if strings.TrimSpace(src) == "" {
		return prepareEmpty(n, baseTmp)
	}
	if isGitRepo(src) && gitAvailable() {
		dirs, cleanup, err = prepareWorktrees(src, n, baseTmp)
		if err == nil {
			return dirs, cleanup, nil
		}
		// Fall through to the copy overlay when worktrees fail
		// (read-only mount, missing HEAD, worktree limits).
	}
	return prepareCopies(src, n, baseTmp)
}

func prepareEmpty(n int, baseTmp string) ([]string, func(), error) {
	var dirs []string
	var created []string
	base := baseTmp
	if base != "" {
		if err := os.MkdirAll(base, 0o755); err != nil {
			return nil, func() {}, fmt.Errorf("attempts: mkdir work base: %w", err)
		}
	}
	for i := 0; i < n; i++ {
		var dir string
		var err error
		if base != "" {
			dir = filepath.Join(base, fmt.Sprintf("attempt-%d", i))
			err = os.MkdirAll(dir, 0o755)
		} else {
			dir, err = os.MkdirTemp("", fmt.Sprintf("excalibur-attempt-%d-*", i))
		}
		if err != nil {
			cleanupDirs(created)
			return nil, func() {}, err
		}
		dirs = append(dirs, dir)
		created = append(created, dir)
	}
	return dirs, func() { cleanupDirs(created) }, nil
}

func prepareCopies(src string, n int, baseTmp string) ([]string, func(), error) {
	base := baseTmp
	if base == "" {
		tmp, err := os.MkdirTemp("", "excalibur-attempts-work-*")
		if err != nil {
			return nil, func() {}, err
		}
		base = tmp
	} else if err := os.MkdirAll(base, 0o755); err != nil {
		return nil, func() {}, fmt.Errorf("attempts: mkdir work base: %w", err)
	}
	var dirs []string
	for i := 0; i < n; i++ {
		dst := filepath.Join(base, fmt.Sprintf("attempt-%d", i))
		_ = os.RemoveAll(dst)
		if err := CopyDir(src, dst); err != nil {
			cleanupDirs(dirs)
			return nil, func() {}, err
		}
		dirs = append(dirs, dst)
	}
	created := append([]string(nil), dirs...)
	return dirs, func() { cleanupDirs(created) }, nil
}

func cleanupDirs(dirs []string) {
	for _, d := range dirs {
		_ = os.RemoveAll(d)
	}
}

func isGitRepo(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil || !st.IsDir() {
		// Worktrees and bare checkouts keep a .git file; treat any
		// present .git entry as a repo marker and let git confirm.
		if err == nil || !os.IsNotExist(err) {
			return true
		}
		if _, ferr := os.Stat(filepath.Join(dir, ".git")); ferr == nil {
			return true
		}
		return false
	}
	return true
}

func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

func prepareWorktrees(src string, n int, baseTmp string) ([]string, func(), error) {
	base := baseTmp
	if base == "" {
		tmp, err := os.MkdirTemp("", "excalibur-attempts-wt-*")
		if err != nil {
			return nil, func() {}, err
		}
		base = tmp
	} else if err := os.MkdirAll(base, 0o755); err != nil {
		return nil, func() {}, fmt.Errorf("attempts: mkdir worktree base: %w", err)
	}
	var dirs []string
	for i := 0; i < n; i++ {
		dst := filepath.Join(base, fmt.Sprintf("attempt-%d", i))
		_ = removeWorktree(src, dst)
		_ = os.RemoveAll(dst)
		cmd := exec.Command("git", "worktree", "add", "--detach", dst, "HEAD")
		cmd.Dir = src
		if out, err := cmd.CombinedOutput(); err != nil {
			for _, d := range dirs {
				_ = removeWorktree(src, d)
				_ = os.RemoveAll(d)
			}
			return nil, func() {}, fmt.Errorf("attempts: git worktree add: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		dirs = append(dirs, dst)
	}
	created := append([]string(nil), dirs...)
	return dirs, func() {
		for _, d := range created {
			_ = removeWorktree(src, d)
			_ = os.RemoveAll(d)
		}
	}, nil
}

func removeWorktree(src, dst string) error {
	cmd := exec.Command("git", "worktree", "remove", "--force", dst)
	cmd.Dir = src
	_ = cmd.Run()
	return nil
}

// CopyDir overlays src onto dst, skipping the .git directory so attempt
// copies stay lightweight and never confuse the winner apply with git
// state. Symlinks are skipped; other entries keep their permission
// bits.
func CopyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// Never copy attempt-local state into another attempt.
		if rel == ".excalibur" || strings.HasPrefix(rel, ".excalibur"+string(filepath.Separator)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return nil
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode().Perm())
	if err != nil {
		return err
	}
	_, cerr := io.Copy(out, in)
	cerr2 := out.Close()
	if cerr != nil {
		return cerr
	}
	return cerr2
}

// ApplyWinner overlays only the winner's changes onto the original
// working directory: new and modified files are copied, files the
// winner deleted are removed. The .git directory and .excalibur state
// are never touched. It returns the applied relative paths, sorted.
func ApplyWinner(winnerDir, origDir string) ([]string, error) {
	if strings.TrimSpace(origDir) == "" || strings.TrimSpace(winnerDir) == "" {
		return nil, fmt.Errorf("attempts: apply needs both directories")
	}
	changed, added, deleted, err := DiffDirs(origDir, winnerDir)
	if err != nil {
		return nil, err
	}
	var applied []string
	applyOne := func(rel string) error {
		src := filepath.Join(winnerDir, rel)
		dst := filepath.Join(origDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		// Reuse the copy helper for permission-preserving copies.
		if err := copyFileStrict(src, dst); err != nil {
			return err
		}
		applied = append(applied, rel)
		return nil
	}
	for _, rel := range changed {
		if err := applyOne(rel); err != nil {
			return applied, err
		}
	}
	for _, rel := range added {
		if err := applyOne(rel); err != nil {
			return applied, err
		}
	}
	for _, rel := range deleted {
		// Never prune version-control or harness state.
		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			continue
		}
		if rel == ".excalibur" || strings.HasPrefix(rel, ".excalibur/") {
			continue
		}
		_ = os.Remove(filepath.Join(origDir, rel))
		applied = append(applied, rel+" (removed)")
	}
	sort.Strings(applied)
	return applied, nil
}

func copyFileStrict(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("attempts: copy dir as file %q", src)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode().Perm())
	if err != nil {
		return err
	}
	_, cerr := io.Copy(out, in)
	if cerr2 := out.Close(); cerr == nil {
		cerr = cerr2
	}
	return cerr
}

// DiffDirs compares orig against other, returning changed, added and
// deleted relative paths (other minus orig), sorted. The .git and
// .excalibur trees are excluded: isolation never versions them.
func DiffDirs(orig, other string) (changed, added, deleted []string, err error) {
	origFiles, err := fileHashes(orig)
	if err != nil {
		return nil, nil, nil, err
	}
	otherFiles, err := fileHashes(other)
	if err != nil {
		return nil, nil, nil, err
	}
	for rel, h := range otherFiles {
		oh, ok := origFiles[rel]
		switch {
		case !ok:
			added = append(added, rel)
		case oh != h:
			changed = append(changed, rel)
		}
	}
	for rel := range origFiles {
		if _, ok := otherFiles[rel]; !ok {
			deleted = append(deleted, rel)
		}
	}
	sort.Strings(changed)
	sort.Strings(added)
	sort.Strings(deleted)
	return changed, added, deleted, nil
}

func fileHashes(root string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil || rel == "." {
			return nil
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == ".excalibur" || strings.HasPrefix(rel, ".excalibur"+string(filepath.Separator)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		sum := sha256.Sum256(data)
		out[rel] = "sha256:" + hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DiffStatLine renders a capped one-line summary of file changes for
// the trial record.
func DiffStatLine(changed, added, deleted []string) string {
	total := len(changed) + len(added) + len(deleted)
	if total == 0 {
		return "no changes"
	}
	names := append(append(append([]string{}, changed...), added...), deleted...)
	sort.Strings(names)
	if len(names) > 5 {
		names = append(names[:5], fmt.Sprintf("…+%d more", len(names)-5))
	}
	return fmt.Sprintf("%d changed, %d added, %d deleted: %s", len(changed), len(added), len(deleted), strings.Join(names, ", "))
}
