package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func initGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	run("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "base")
	return dir
}

// Section 16.6 row 4: three children in worktrees with a planted
// conflict; rebase integrates the clean ones, the conflict returns
// to the child with both diffs, impacted tests gate the merge, and
// the integration branch fast-forwards.
func TestIntegrationQueueConflictAndFastForward(t *testing.T) {
	repo := initGitRepo(t)
	data := t.TempDir()
	mgr := &WorktreeManager{DataDir: data}
	task := "task-1"
	integration, err := mgr.CreateIntegration(repo, task)
	if err != nil {
		t.Fatal(err)
	}
	// Child 1: clean change.
	b1, p1, err := mgr.CreateChild(repo, "proj", task, "child1", integration)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p1, "c1.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, p1, "add", "-A")
	gitIn(t, p1, "commit", "-qm", "c1")
	// Child 2: conflicting change to the same line of base.txt.
	b2, p2, err := mgr.CreateChild(repo, "proj", task, "child2", integration)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p2, "base.txt"), []byte("child2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, p2, "add", "-A")
	gitIn(t, p2, "commit", "-qm", "c2")
	// Advance the integration branch the other way to force the
	// conflict. The advance is committed on the user branch first,
	// then merged into integration (capture its id before the
	// checkout moves HEAD).
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("integration\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-qm", "advance integration")
	advance := gitHead(t, repo)
	gitIn(t, repo, "checkout", "-q", integration)
	gitIn(t, repo, "merge", "--ff-only", "-q", advance)

	q := NewIntegrationQueue(mgr, repo, integration)
	testsRan := 0
	q.WithQueueTests(func(ctx context.Context, workDir string) error {
		testsRan++
		return nil
	})
	q.Enqueue("child1", b1)
	q.Enqueue("child2", b2)
	reps, err := q.ProcessAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(reps) != 2 {
		t.Fatalf("want 2 reports, got %d", len(reps))
	}
	byBranch := map[string]IntegrationReport{}
	for _, r := range reps {
		byBranch[r.Branch] = r
	}
	if byBranch[b1].Status != QueueMerged {
		t.Fatalf("clean child must merge: %+v", byBranch[b1])
	}
	if byBranch[b2].Status != QueueReturned {
		t.Fatalf("conflicting child must return: %+v", byBranch[b2])
	}
	if !strings.Contains(byBranch[b2].Detail, "ours:") && !strings.Contains(byBranch[b2].Detail, "rebase") {
		t.Fatalf("return must carry diffs: %q", byBranch[b2].Detail)
	}
	if testsRan != 1 {
		t.Fatalf("impacted tests must gate clean merges only, ran %d", testsRan)
	}
	// The integration branch fast-forwarded to child1.
	head := gitHead(t, repo)
	childHead := gitBranchHead(t, repo, b1)
	if head != childHead {
		t.Fatalf("integration must fast-forward to child1: %q vs %q", head, childHead)
	}
	// Propose without touching the user branch.
	p, err := mgr.Propose(repo, task, integration)
	if err != nil {
		t.Fatal(err)
	}
	if p.IntegrationBranch != integration || p.HeadCommit == "" {
		t.Fatalf("bad proposal: %+v", p)
	}
}

// Failing impacted tests return the branch to the child.
func TestIntegrationQueueTestsGate(t *testing.T) {
	repo := initGitRepo(t)
	data := t.TempDir()
	mgr := &WorktreeManager{DataDir: data}
	integration, err := mgr.CreateIntegration(repo, "t2")
	if err != nil {
		t.Fatal(err)
	}
	b1, p1, err := mgr.CreateChild(repo, "proj", "t2", "c1", integration)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p1, "new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, p1, "add", "-A")
	gitIn(t, p1, "commit", "-qm", "new")
	q := NewIntegrationQueue(mgr, repo, integration)
	q.WithQueueTests(func(ctx context.Context, workDir string) error {
		return errors.New("impacted suite red")
	})
	q.Enqueue("c1", b1)
	more, rep, err := q.ProcessNext(context.Background())
	if err != nil || !more {
		t.Fatal(more, err)
	}
	if rep.Status != QueueReturned || !strings.Contains(rep.Detail, "red") {
		t.Fatalf("failing tests must return with the report: %+v", rep)
	}
}

// Section 16.4: merging into the user branch needs explicit user
// confirmation, otherwise nothing changes.
func TestConfirmMergeNeedsUser(t *testing.T) {
	repo := initGitRepo(t)
	mgr := &WorktreeManager{DataDir: t.TempDir()}
	integration, err := mgr.CreateIntegration(repo, "t3")
	if err != nil {
		t.Fatal(err)
	}
	userBranch := gitCurrentBranch(t, repo)
	if err := mgr.ConfirmMerge(repo, integration, userBranch, false); !errors.Is(err, ErrNeedConfirmation) {
		t.Fatalf("want ErrNeedConfirmation, got %v", err)
	}
	if err := mgr.ConfirmMerge(repo, integration, userBranch, true); err != nil {
		t.Fatal(err)
	}
}

// Section 16.6: projects with submodules use the shared tree.
func TestSubmodulesForceShared(t *testing.T) {
	repo := initGitRepo(t)
	mgr := &WorktreeManager{DataDir: t.TempDir()}
	if mgr.HasSubmodules(repo) {
		t.Fatal("plain repo must not report submodules")
	}
	// A .gitmodules file alone is enough to force shared mode (the
	// conservative reading of Section 16.4).
	if err := os.WriteFile(filepath.Join(repo, ".gitmodules"), []byte("[submodule]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !mgr.HasSubmodules(repo) {
		t.Fatal("repo with .gitmodules must force shared mode")
	}
	if got := ModeFor(ModeRequest{ParallelImplementers: 3, HasGit: true, HasSubmodules: true}); got != ModeShared {
		t.Fatalf("want shared, got %s", got)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func gitHead(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func gitBranchHead(t *testing.T, dir, branch string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", branch)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func gitCurrentBranch(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

var _ = time.Second
