package attempts

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Workspace is one attempt's isolated copy of the real working
// directory. Attempts never write to the real directory: their tool
// calls are mapped onto Dir through Map.
type Workspace struct {
	// Index is the attempt number (0-based).
	Index int
	// Root is the attempt-owned tree removed after the fan-out.
	Root string
	// Dir is the attempt's working directory: the real directory's
	// layout under Root (for /app: <Root>/app), or Root itself when
	// the fan-out has no real directory.
	Dir string
	// Map translates the real directory onto Dir. Disabled when the
	// fan-out has no real directory.
	Map WorkdirMap
}

// noopCleanup is returned with errors so callers can always defer it.
func noopCleanup() {}

// PrepareWorkspaces creates n isolated full copies of the real working
// directory src and returns them with the baseline manifest every copy
// starts from. Each copy sits under its own attempt root at the real
// directory's path layout, so the workdir mapping is a prefix swap
// (/app becomes <root>/app). The copy is complete (ignored and
// untracked files included, .git included so the agent's own git
// commands see the same repository); only the attempt-local harness
// state is skipped.
//
// An empty src yields n empty directories with no mapping. A blank
// baseTmp uses a fresh temporary directory. The cleanup removes every
// root the call created.
func PrepareWorkspaces(src string, n int, baseTmp string) ([]Workspace, Manifest, func(), error) {
	if n < 1 {
		n = 1
	}
	base, ownBase, err := workBase(baseTmp)
	if err != nil {
		return nil, nil, noopCleanup, err
	}
	var roots []string
	cleanup := func() {
		for _, r := range roots {
			_ = os.RemoveAll(r)
		}
		if ownBase {
			_ = os.RemoveAll(base)
		}
	}
	fail := func(err error) ([]Workspace, Manifest, func(), error) {
		cleanup()
		return nil, nil, noopCleanup, err
	}

	if strings.TrimSpace(src) == "" {
		ws := make([]Workspace, 0, n)
		for i := 0; i < n; i++ {
			root := filepath.Join(base, fmt.Sprintf("attempt-%d", i))
			if err := os.MkdirAll(root, 0o755); err != nil {
				return fail(fmt.Errorf("attempts: mkdir attempt root: %w", err))
			}
			roots = append(roots, root)
			ws = append(ws, Workspace{Index: i, Root: root, Dir: root})
		}
		return ws, Manifest{}, cleanup, nil
	}

	realDir, err := filepath.Abs(src)
	if err != nil {
		return fail(fmt.Errorf("attempts: resolve project directory: %w", err))
	}
	if filepath.Dir(realDir) == realDir {
		return fail(fmt.Errorf("attempts: refusing to copy the filesystem root %q", realDir))
	}
	if isWithinPath(realDir, base) {
		return fail(fmt.Errorf("attempts: work base %q lies inside the project directory %q", base, realDir))
	}
	baseline, err := Scan(realDir)
	if err != nil {
		return fail(err)
	}
	// The layout below the attempt root mirrors the real path without
	// its volume name (no-op on unix, required on windows).
	layout := realDir[len(filepath.VolumeName(realDir)):]
	ws := make([]Workspace, 0, n)
	for i := 0; i < n; i++ {
		root := filepath.Join(base, fmt.Sprintf("attempt-%d", i))
		if err := os.MkdirAll(root, 0o755); err != nil {
			return fail(fmt.Errorf("attempts: mkdir attempt root: %w", err))
		}
		roots = append(roots, root)
		dir := filepath.Join(root, layout)
		if err := CopyDir(realDir, dir); err != nil {
			return fail(fmt.Errorf("attempts: copy workdir for attempt %d: %w", i, err))
		}
		ws = append(ws, Workspace{Index: i, Root: root, Dir: dir, Map: WorkdirMap{Real: realDir, Attempt: dir}})
	}
	return ws, baseline, cleanup, nil
}

// workBase returns the directory that holds the attempt roots and
// whether this call created it.
func workBase(baseTmp string) (string, bool, error) {
	own := strings.TrimSpace(baseTmp) == ""
	if own {
		tmp, err := os.MkdirTemp("", "excalibur-attempts-*")
		if err != nil {
			return "", false, fmt.Errorf("attempts: mkdir work base: %w", err)
		}
		baseTmp = tmp
	} else if err := os.MkdirAll(baseTmp, 0o755); err != nil {
		return "", false, fmt.Errorf("attempts: mkdir work base: %w", err)
	}
	abs, err := filepath.Abs(baseTmp)
	if err != nil {
		return "", false, fmt.Errorf("attempts: resolve work base: %w", err)
	}
	return abs, own, nil
}

// CopyDir copies the tree at src to dst: every directory, regular
// file and symlink, with permission bits preserved. The attempt-local
// harness state (.excalibur) is skipped; .git is copied. Unreadable
// files and special files are skipped, as Scan skips them, so the
// baseline and the copy always describe the same tree.
func CopyDir(src, dst string) error {
	resolved, err := filepath.EvalSymlinks(src)
	if err != nil {
		return err
	}
	type dirPerm struct {
		path string
		perm fs.FileMode
	}
	var dirs []dirPerm
	err = filepath.WalkDir(resolved, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == resolved {
				return err
			}
			return nil
		}
		rel, rerr := filepath.Rel(resolved, path)
		if rerr != nil {
			return rerr
		}
		if rel != "." && isHarnessState(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		mode := info.Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			dirs = append(dirs, dirPerm{path: target, perm: mode.Perm()})
		case mode&fs.ModeSymlink != 0:
			link, lerr := os.Readlink(path)
			if lerr != nil {
				return nil
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return os.Symlink(link, target)
		case mode.IsRegular():
			in, oerr := os.Open(path)
			if oerr != nil {
				return nil
			}
			defer in.Close()
			return copyStream(in, target, mode.Perm())
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Directory modes last and deepest first, so a read-only directory
	// still receives its children.
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Chmod(dirs[i].path, dirs[i].perm); err != nil {
			return err
		}
	}
	return nil
}

// copyFile copies one regular file with its permission bits, replacing
// dst. Any failure is returned.
func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return copyStream(in, dst, perm)
}

func copyStream(in io.Reader, dst string, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, cerr := io.Copy(out, in)
	if cerr2 := out.Close(); cerr == nil {
		cerr = cerr2
	}
	if cerr != nil {
		return cerr
	}
	// The umask may have stripped bits at creation.
	return os.Chmod(dst, perm)
}

// applyHook lets tests fail an apply at a chosen path. Production
// leaves it nil.
var applyHook func(rel string) error

// applyOp is one step of an apply, in execution order.
type applyOp struct {
	kind string // "remove", "mkdir", "write" or "chmod"
	rel  string
	want Entry // winner entry (mkdir, write, chmod)
	prev Entry // real-directory entry before the apply
	had  bool  // prev is set
}

// planApply orders the changes so that every step is safe: removals
// first (deepest entries first, so directories empty out), then
// directory creation and file writes (parents first), then directory
// modes (deepest first, so read-only directories still take children).
func planApply(ch Changes, winner, prev Manifest) []applyOp {
	var removes, mkdirs, writes, chmods []applyOp
	for i := len(ch.Deleted) - 1; i >= 0; i-- {
		rel := ch.Deleted[i]
		p, had := prev[rel]
		removes = append(removes, applyOp{kind: "remove", rel: rel, prev: p, had: had})
	}
	upserts := append(append([]string{}, ch.Changed...), ch.Added...)
	sort.Strings(upserts)
	for _, rel := range upserts {
		want := winner[rel]
		p, had := prev[rel]
		switch {
		case want.Kind == "dir" && had && p.Kind == "dir":
			chmods = append(chmods, applyOp{kind: "chmod", rel: rel, want: want, prev: p, had: true})
		case want.Kind == "dir":
			mkdirs = append(mkdirs, applyOp{kind: "mkdir", rel: rel, want: want, prev: p, had: had})
		default:
			writes = append(writes, applyOp{kind: "write", rel: rel, want: want, prev: p, had: had})
		}
	}
	for i, j := 0, len(chmods)-1; i < j; i, j = i+1, j-1 {
		chmods[i], chmods[j] = chmods[j], chmods[i]
	}
	ops := make([]applyOp, 0, len(removes)+len(mkdirs)+len(writes)+len(chmods))
	ops = append(ops, removes...)
	ops = append(ops, mkdirs...)
	ops = append(ops, writes...)
	ops = append(ops, chmods...)
	return ops
}

// applier executes a plan against the real directory, keeping the
// backups that rollback needs.
type applier struct {
	winnerDir string
	orig      string
	backup    string
}

// ApplyChanges promotes the winner's changes, relative to baseline,
// onto the real directory origDir. Only changed, added and deleted
// paths move: unchanged files, the harness state and .git are never
// touched, and changes that other processes made in origDir after the
// baseline are left alone unless the winner changed the same path.
//
// Every entry is backed up before it is replaced. After the copy,
// origDir is re-scanned and each applied path is compared by
// fingerprint with the winner. Any failure restores the real directory
// to its state before the call and returns the error. It returns the
// applied relative paths, sorted.
func ApplyChanges(winnerDir, origDir string, baseline Manifest) ([]string, error) {
	if strings.TrimSpace(winnerDir) == "" || strings.TrimSpace(origDir) == "" {
		return nil, fmt.Errorf("attempts: apply needs both directories")
	}
	winner, err := Scan(winnerDir)
	if err != nil {
		return nil, fmt.Errorf("attempts: scan winner: %w", err)
	}
	ch := Diff(baseline, winner)
	if ch.Total() == 0 {
		return nil, nil
	}
	prev, err := Scan(origDir)
	if err != nil {
		return nil, fmt.Errorf("attempts: scan real directory: %w", err)
	}
	backup, err := os.MkdirTemp("", "excalibur-attempts-undo-*")
	if err != nil {
		return nil, fmt.Errorf("attempts: mkdir undo dir: %w", err)
	}
	defer os.RemoveAll(backup)
	ap := &applier{winnerDir: winnerDir, orig: origDir, backup: backup}

	ops := planApply(ch, winner, prev)
	if err := preflightReplace(ops, prev); err != nil {
		return nil, fmt.Errorf("attempts: apply: %w", err)
	}
	var done []applyOp
	abort := func(cause error) ([]string, error) {
		if rerr := ap.rollback(done); rerr != nil {
			return nil, fmt.Errorf("attempts: apply: %w (rollback: %v)", cause, rerr)
		}
		return nil, fmt.Errorf("attempts: apply: %w", cause)
	}
	for _, op := range ops {
		if applyHook != nil {
			if err := applyHook(op.rel); err != nil {
				return abort(err)
			}
		}
		done = append(done, op)
		if err := ap.run(op); err != nil {
			return abort(fmt.Errorf("%s %s: %w", op.kind, op.rel, err))
		}
	}
	post, err := Scan(origDir)
	if err != nil {
		return abort(fmt.Errorf("re-scan real directory: %w", err))
	}
	for _, rel := range append(append([]string{}, ch.Changed...), ch.Added...) {
		if post[rel] != winner[rel] {
			return abort(fmt.Errorf("verify %s: fingerprint differs from the winner", rel))
		}
	}
	for _, rel := range ch.Deleted {
		if _, ok := post[rel]; ok {
			return abort(fmt.Errorf("verify %s: still present after delete", rel))
		}
	}
	applied := ch.Paths()
	if err := checkApplied(ch, applied); err != nil {
		return abort(err)
	}
	return applied, nil
}

// preflightReplace refuses a plan that would replace or remove a
// directory still holding entries the winner did not change (typically
// a command that escaped the workdir mapping). Such a step cannot
// complete, and discovering it mid-apply would leave earlier steps to
// roll back, so it is refused before the real directory changes.
func preflightReplace(ops []applyOp, prev Manifest) error {
	gone := map[string]bool{}
	for _, op := range ops {
		switch {
		case op.kind == "remove" && op.had:
			gone[op.rel] = true
		case op.kind == "write" && op.had:
			gone[op.rel] = true
		case op.kind == "mkdir" && op.had && op.prev.Kind != "dir":
			gone[op.rel] = true
		}
	}
	for _, op := range ops {
		if !op.had || op.prev.Kind != "dir" || (op.kind != "remove" && op.kind != "write") {
			continue
		}
		prefix := op.rel + string(filepath.Separator)
		for rel := range prev {
			if strings.HasPrefix(rel, prefix) && !gone[rel] {
				return fmt.Errorf("cannot replace directory %s: %s is not part of the winner's changes", op.rel, rel)
			}
		}
	}
	return nil
}

// checkApplied is the zero-files guard: a winner that changed files
// must apply at least one of them, or the promotion failed silently.
func checkApplied(ch Changes, applied []string) error {
	if ch.Files > 0 && len(applied) == 0 {
		return fmt.Errorf("applied 0 file(s) for %d changed file(s)", ch.Files)
	}
	return nil
}

func (ap *applier) dst(rel string) string { return filepath.Join(ap.orig, rel) }

// keep copies the current content of a file the op is about to replace
// into the undo directory. Directories and links need only their
// recorded fingerprint.
func (ap *applier) keep(op applyOp) error {
	if !op.had || op.prev.Kind != "file" {
		return nil
	}
	return copyFile(ap.dst(op.rel), filepath.Join(ap.backup, op.rel), op.prev.Mode)
}

func (ap *applier) run(op applyOp) error {
	dst := ap.dst(op.rel)
	switch op.kind {
	case "remove":
		if !op.had {
			return nil
		}
		if err := ap.keep(op); err != nil {
			return err
		}
		return os.Remove(dst)
	case "mkdir":
		if op.had && op.prev.Kind != "dir" {
			if err := ap.keep(op); err != nil {
				return err
			}
			if err := os.Remove(dst); err != nil {
				return err
			}
		}
		// Modes are set by the chmod phase, after the children.
		return os.MkdirAll(dst, 0o755)
	case "write":
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if op.had {
			if err := ap.keep(op); err != nil {
				return err
			}
			if err := os.Remove(dst); err != nil {
				return err
			}
		}
		return ap.materialize(op)
	case "chmod":
		return os.Chmod(dst, op.want.Mode)
	}
	return fmt.Errorf("attempts: unknown apply step %q", op.kind)
}

func (ap *applier) materialize(op applyOp) error {
	dst := ap.dst(op.rel)
	if op.want.Kind == "link" {
		return os.Symlink(op.want.Sum, dst)
	}
	return copyFile(filepath.Join(ap.winnerDir, op.rel), dst, op.want.Mode)
}

// rollback undoes the done steps in reverse order: created entries are
// removed and replaced entries are restored from the undo directory.
func (ap *applier) rollback(done []applyOp) error {
	var errs []string
	for i := len(done) - 1; i >= 0; i-- {
		op := done[i]
		dst := ap.dst(op.rel)
		var err error
		switch op.kind {
		case "remove":
			err = ap.restore(op)
		case "mkdir":
			err = os.Remove(dst)
			if err != nil && !os.IsNotExist(err) {
				break
			}
			err = ap.restore(op)
		case "write":
			err = os.Remove(dst)
			if err != nil && !os.IsNotExist(err) {
				break
			}
			err = ap.restore(op)
		case "chmod":
			if op.had {
				err = os.Chmod(dst, op.prev.Mode)
			}
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s %s: %v", op.kind, op.rel, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// restore recreates the real-directory entry an op replaced or removed.
func (ap *applier) restore(op applyOp) error {
	if !op.had {
		return nil
	}
	dst := ap.dst(op.rel)
	switch op.prev.Kind {
	case "file":
		return copyFile(filepath.Join(ap.backup, op.rel), dst, op.prev.Mode)
	case "link":
		return os.Symlink(op.prev.Sum, dst)
	case "dir":
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		return os.Chmod(dst, op.prev.Mode)
	}
	return nil
}
