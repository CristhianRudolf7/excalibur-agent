package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// prepareWorktree sets up the working directory of a task (Section
// 3.6: with worktree, the task works in its own worktree and branch
// and the delegating tree never changes). It returns the directory to
// run in and the branch name ("" when no worktree). Without git the
// task runs in the indicated directory.
func prepareWorktree(store Store, rec *TaskRecord) (dir, branch string, err error) {
	if !rec.Worktree || rec.ProjectDir == "" {
		return rec.ProjectDir, "", nil
	}
	if _, err := exec.LookPath("git"); err != nil {
		return rec.ProjectDir, "", nil
	}
	if st, err := os.Stat(filepath.Join(rec.ProjectDir, ".git")); err != nil || !st.IsDir() {
		return rec.ProjectDir, "", nil
	}
	branch = "excalibur/" + string(rec.TaskID)
	wtDir := filepath.Join(store.Base, "worktrees", string(rec.TaskID))
	if st, err := os.Stat(wtDir); err == nil && st.IsDir() {
		return wtDir, branch, nil
	}
	if out, err := gitRun(rec.ProjectDir, "worktree", "add", "-b", branch, wtDir); err != nil {
		// The branch may exist from a resumed task: attach to the
		// existing worktree instead of failing.
		if out2, err2 := gitRun(rec.ProjectDir, "worktree", "add", wtDir, branch); err2 != nil {
			return "", "", fmt.Errorf("cli: git worktree add: %v (%s) (%s)", err, out, out2)
		}
		return wtDir, branch, nil
	}
	return wtDir, branch, nil
}

// worktreeDiff summarizes the task branch against its base: the diff
// stat and the changed file list reported in the delivery result.
func worktreeDiff(workDir string, rec *TaskRecord) (stat string, files []string) {
	if workDir == "" || rec.ProjectDir == "" {
		return "", nil
	}
	// Compare the worktree against the original checkout.
	base := rec.ProjectDir
	out, err := gitRun(workDir, "diff", "--stat", "HEAD")
	if err == nil && strings.TrimSpace(out) != "" {
		stat = strings.TrimSpace(out)
	}
	// Changed files: prefer the worktree diff, fall back to status.
	nameOut, err := gitRun(workDir, "diff", "--name-only", "HEAD")
	if err != nil || strings.TrimSpace(nameOut) == "" {
		nameOut, _ = gitRun(workDir, "status", "--porcelain")
	}
	_ = base
	seen := map[string]bool{}
	for _, line := range strings.Split(nameOut, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		name := fields[len(fields)-1]
		if !seen[name] {
			seen[name] = true
			files = append(files, name)
		}
	}
	return stat, files
}

func gitRun(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
