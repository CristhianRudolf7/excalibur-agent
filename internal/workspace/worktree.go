package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// GitRunner runs git commands. Production uses ExecRunner (the git
// CLI); tests inject a fake or use temp repos with the real CLI.
type GitRunner interface {
	Run(dir string, args ...string) (string, error)
}

// ExecRunner runs the git CLI with a timeout.
type ExecRunner struct {
	Timeout time.Duration
}

// Run implements GitRunner.
func (r ExecRunner) Run(dir string, args ...string) (string, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_PAGER=cat", "PAGER=cat", "TERM=dumb",
		"GIT_AUTHOR_NAME=excalibur", "GIT_AUTHOR_EMAIL=excalibur@localhost",
		"GIT_COMMITTER_NAME=excalibur", "GIT_COMMITTER_EMAIL=excalibur@localhost",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("workspace: git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// IntegrationBranchName names the integration branch of a task.
func IntegrationBranchName(taskID string) string {
	safe := strings.ReplaceAll(taskID, ":", "-")
	return "excalibur/integration/" + safe
}

// ChildBranchName names the branch of one child implementer.
func ChildBranchName(taskID, childID string) string {
	safe := func(s string) string {
		s = strings.ReplaceAll(s, ":", "-")
		return strings.ReplaceAll(s, "/", "-")
	}
	return "excalibur/task/" + safe(taskID) + "/" + safe(childID)
}

// WorktreeManager creates integration branches and per-child worktrees
// outside the project root, in the data directory, so the user tree
// stays clean (Section 16.4 point 1).
type WorktreeManager struct {
	// DataDir is the daemon data root; worktrees live under
	// <DataDir>/worktrees/<project>/<child>.
	DataDir string
	Git     GitRunner
}

// childDir returns the worktree path of a child.
func (m *WorktreeManager) childDir(projectID, childID string) string {
	safe := strings.ReplaceAll(childID, "/", "-")
	return filepath.Join(m.DataDir, "worktrees", projectID, safe)
}

func (m *WorktreeManager) git() GitRunner {
	if m.Git != nil {
		return m.Git
	}
	return ExecRunner{}
}

// HasSubmodules reports whether the repo tracks submodules: such
// projects always use the shared tree because git worktree support
// for submodules is incomplete (Section 16.4 point 1).
func (m *WorktreeManager) HasSubmodules(projectRoot string) bool {
	for _, candidate := range []string{filepath.Join(projectRoot, ".gitmodules")} {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return true
		}
	}
	out, err := m.git().Run(projectRoot, "submodule", "status")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) != ""
}

// BaseCommit returns the HEAD commit of the project repo.
func (m *WorktreeManager) BaseCommit(projectRoot string) (string, error) {
	out, err := m.git().Run(projectRoot, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// CreateIntegration creates the integration branch of a task at the
// current HEAD of the project repo.
func (m *WorktreeManager) CreateIntegration(projectRoot, taskID string) (string, error) {
	branch := IntegrationBranchName(taskID)
	if _, err := m.git().Run(projectRoot, "rev-parse", "--verify", "refs/heads/"+branch); err == nil {
		return branch, nil
	}
	if _, err := m.git().Run(projectRoot, "branch", branch); err != nil {
		return "", err
	}
	return branch, nil
}

// CreateChild creates the child branch (from the integration branch)
// and its worktree outside the project root. It returns the branch
// and the worktree path.
func (m *WorktreeManager) CreateChild(projectRoot, projectID, taskID, childID, integrationBranch string) (branch, path string, err error) {
	branch = ChildBranchName(taskID, childID)
	path = m.childDir(projectID, childID)
	if _, serr := os.Stat(path); serr == nil {
		_ = os.RemoveAll(path)
	}
	if _, err := m.git().Run(projectRoot, "rev-parse", "--verify", "refs/heads/"+branch); err != nil {
		if _, err := m.git().Run(projectRoot, "branch", branch, integrationBranch); err != nil {
			return "", "", err
		}
	}
	if _, err := m.git().Run(projectRoot, "worktree", "add", path, branch); err != nil {
		return "", "", err
	}
	return branch, path, nil
}

// RemoveChild detaches the child worktree and removes its directory.
func (m *WorktreeManager) RemoveChild(projectRoot, projectID, childID string) error {
	path := m.childDir(projectID, childID)
	_, _ = m.git().Run(projectRoot, "worktree", "remove", "--force", path)
	_ = os.RemoveAll(path)
	return nil
}

// ListWorktrees parses `git worktree list --porcelain`.
func (m *WorktreeManager) ListWorktrees(projectRoot string) ([]string, error) {
	out, err := m.git().Run(projectRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "worktree "); ok {
			paths = append(paths, strings.TrimSpace(rest))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// QueueStatus is the state of one branch in the integration queue.
type QueueStatus string

// Queue states.
const (
	QueuePending   QueueStatus = "pending"
	QueueMerging   QueueStatus = "merging"
	QueueMerged    QueueStatus = "merged"
	QueueReturned  QueueStatus = "returned"
	QueueConflicts QueueStatus = "conflicts"
)

// QueueItem is one child branch awaiting integration.
type QueueItem struct {
	Branch    string
	ChildID   string
	Enqueued  time.Time
	Status    QueueStatus
	Report    string
	Attempted bool
}

// IntegrationReport is the outcome of processing one branch.
type IntegrationReport struct {
	Branch  string
	ChildID string
	Status  QueueStatus
	// Detail carries the rebase log, the conflict description with
	// both diffs and each change intent, the impacted-test results,
	// or the return reason for the child.
	Detail string
}

// TestFunc runs the impacted tests (code graph selection,
// Section 16.4 point 3.3). Production wires the graph-selected test
// command; tests inject fakes.
type TestFunc func(ctx context.Context, workDir string) error

// ResolveFunc attempts a parent-side (or integrator-child) conflict
// resolution with both diffs and the intent of each change. It
// returns resolved=true when the tree is ready to commit.
type ResolveFunc func(ctx context.Context, workDir, oursDiff, theirsDiff, oursIntent, theirsIntent string) (resolved bool, detail string, err error)

// IntegrationQueue holds child branches and integrates them one by
// one onto the integration branch (Section 16.4 point 3): rebase,
// conflict resolution attempt, impacted tests, fast-forward or return
// to the child with the report.
type IntegrationQueue struct {
	mu          sync.Mutex
	items       []*QueueItem
	integration string
	projectRoot string
	mgr         *WorktreeManager
	tests       TestFunc
	resolve     ResolveFunc
	now         func() time.Time
}

// NewIntegrationQueue returns a queue for one task.
func NewIntegrationQueue(mgr *WorktreeManager, projectRoot, integrationBranch string) *IntegrationQueue {
	return &IntegrationQueue{
		integration: integrationBranch,
		projectRoot: projectRoot,
		mgr:         mgr,
		now:         time.Now,
	}
}

// WithQueueTests sets the impacted-test hook.
func (q *IntegrationQueue) WithQueueTests(fn TestFunc) *IntegrationQueue {
	q.tests = fn
	return q
}

// WithQueueResolve sets the conflict-resolution hook.
func (q *IntegrationQueue) WithQueueResolve(fn ResolveFunc) *IntegrationQueue {
	q.resolve = fn
	return q
}

// Enqueue adds a child branch to the tail of the queue.
func (q *IntegrationQueue) Enqueue(childID, branch string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, it := range q.items {
		if it.Branch == branch {
			return
		}
	}
	now := time.Now()
	if q.now != nil {
		now = q.now()
	}
	q.items = append(q.items, &QueueItem{Branch: branch, ChildID: childID, Enqueued: now, Status: QueuePending})
}

// Pending returns the queued branches in order.
func (q *IntegrationQueue) Pending() []QueueItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]QueueItem, 0, len(q.items))
	for _, it := range q.items {
		if it.Status == QueuePending {
			out = append(out, *it)
		}
	}
	return out
}

// Status returns every item with its status and report.
func (q *IntegrationQueue) Status() []QueueItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]QueueItem, len(q.items))
	for i, it := range q.items {
		out[i] = *it
	}
	return out
}

func (q *IntegrationQueue) git() GitRunner {
	if q.mgr != nil && q.mgr.Git != nil {
		return q.mgr.Git
	}
	return ExecRunner{}
}

// ProcessNext integrates the head of the queue. It returns false when
// the queue is empty.
func (q *IntegrationQueue) ProcessNext(ctx context.Context) (bool, IntegrationReport, error) {
	q.mu.Lock()
	var item *QueueItem
	for _, it := range q.items {
		if it.Status == QueuePending {
			item = it
			break
		}
	}
	if item == nil {
		q.mu.Unlock()
		return false, IntegrationReport{}, nil
	}
	item.Status = QueueMerging
	q.mu.Unlock()

	rep, err := q.integrate(ctx, item)
	q.mu.Lock()
	item.Status = rep.Status
	item.Report = rep.Detail
	item.Attempted = true
	q.mu.Unlock()
	return true, rep, err
}

// ProcessAll drains the queue head to tail.
func (q *IntegrationQueue) ProcessAll(ctx context.Context) ([]IntegrationReport, error) {
	var out []IntegrationReport
	for {
		more, rep, err := q.ProcessNext(ctx)
		if err != nil {
			return out, err
		}
		if !more {
			return out, nil
		}
		out = append(out, rep)
	}
}

// workDirForBranch returns the worktree checking out branch, or root
// when the branch is not checked out anywhere: git refuses to rebase
// a branch checked out in another worktree, so the rebase must run
// inside its own checkout.
func (q *IntegrationQueue) workDirForBranch(branch string) string {
	g := q.git()
	out, err := g.Run(q.projectRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return q.projectRoot
	}
	var cur string
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "worktree "); ok {
			cur = strings.TrimSpace(rest)
			continue
		}
		if rest, ok := strings.CutPrefix(line, "branch "); ok {
			if strings.TrimSpace(rest) == "refs/heads/"+branch && cur != "" {
				return cur
			}
		}
	}
	return q.projectRoot
}

// integrate rebases one branch onto the integration branch, attempts
// resolution on conflicts, runs the impacted tests and fast-forwards
// or returns the branch to the child.
func (q *IntegrationQueue) integrate(ctx context.Context, item *QueueItem) (IntegrationReport, error) {
	g := q.git()
	rep := IntegrationReport{Branch: item.Branch, ChildID: item.ChildID}
	if err := ctx.Err(); err != nil {
		return rep, err
	}
	// 1. Rebase onto the integration branch inside the branch's own
	// checkout (a branch checked out in a worktree cannot be
	// rebased from elsewhere).
	workDir := q.workDirForBranch(item.Branch)
	rebaseOut, err := g.Run(workDir, "rebase", q.integration, item.Branch)
	if err != nil {
		// 2. Conflict: collect both diffs and attempt resolution.
		oursDiff, _ := g.Run(workDir, "diff", q.integration+"..."+item.Branch)
		_, _ = g.Run(workDir, "rebase", "--abort")
		theirsDiff := rebaseOut
		if q.resolve != nil {
			ok, detail, rerr := q.resolve(ctx, workDir, oursDiff, theirsDiff, "integration:"+q.integration, "child:"+item.Branch)
			if rerr != nil {
				rep.Status = QueueReturned
				rep.Detail = "resolution error: " + rerr.Error() + "\nrebase: " + rebaseOut
				return rep, nil
			}
			if !ok {
				rep.Status = QueueReturned
				rep.Detail = "conflicts need the child: " + detail + "\nours:\n" + oursDiff + "\ntheirs:\n" + theirsDiff
				return rep, nil
			}
			rep.Detail = "resolved by parent: " + detail
		} else {
			rep.Status = QueueReturned
			rep.Detail = "rebase conflicts; returned to child with diffs\nours:\n" + oursDiff + "\ntheirs:\n" + theirsDiff
			return rep, nil
		}
	} else if strings.TrimSpace(rebaseOut) != "" {
		rep.Detail = "rebase: " + strings.TrimSpace(rebaseOut)
	}
	// 3. Impacted tests.
	if q.tests != nil {
		if terr := q.tests(ctx, workDir); terr != nil {
			rep.Status = QueueReturned
			if rep.Detail != "" {
				rep.Detail += "\n"
			}
			rep.Detail += "impacted tests failed; returned to child: " + terr.Error()
			return rep, nil
		}
		if rep.Detail != "" {
			rep.Detail += "\n"
		}
		rep.Detail += "impacted tests passed"
	}
	// 4. Fast-forward the integration branch from the checkout
	// that already holds it (or check it out in the main tree).
	// The rebase ran in the child worktree, so the merge must not
	// run there: the same branch cannot be checked out twice.
	mergeDir := q.workDirForBranch(q.integration)
	if mergeDir == q.projectRoot {
		if _, err := g.Run(mergeDir, "checkout", "-q", q.integration); err != nil {
			rep.Status = QueueReturned
			rep.Detail += "\ncheckout integration failed: " + err.Error()
			return rep, nil
		}
	}
	if _, err := g.Run(mergeDir, "merge", "--ff-only", "-q", item.Branch); err != nil {
		rep.Status = QueueConflicts
		rep.Detail += "\nfast-forward failed (diverged after tests?): " + err.Error()
		return rep, nil
	}
	rep.Status = QueueMerged
	if rep.Detail == "" {
		rep.Detail = "fast-forwarded " + q.integration + " to " + item.Branch
	} else {
		rep.Detail += "\nfast-forwarded " + q.integration + " to " + item.Branch
	}
	return rep, nil
}

// Proposal is the end-of-task offer to the user (Section 16.4
// point 4): the integration branch is proposed, never merged into
// the user main branch, pushed or rebased without confirmation
// (Section 6.4).
type Proposal struct {
	TaskID            string
	IntegrationBranch string
	BaseCommit        string
	HeadCommit        string
	Files             []string
}

// Propose builds the proposal without touching the user branch.
func (m *WorktreeManager) Propose(projectRoot, taskID, integrationBranch string) (*Proposal, error) {
	g := m.git()
	head, err := g.Run(projectRoot, "rev-parse", integrationBranch)
	if err != nil {
		return nil, err
	}
	base, err := g.Run(projectRoot, "merge-base", integrationBranch, "HEAD")
	if err != nil {
		// No common base (fresh repo without HEAD history): use the
		// integration branch itself as both ends.
		base = head
	}
	filesOut, _ := g.Run(projectRoot, "diff", "--name-only", strings.TrimSpace(base), strings.TrimSpace(head))
	var files []string
	for _, f := range strings.Split(filesOut, "\n") {
		if f = strings.TrimSpace(f); f != "" {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	return &Proposal{
		TaskID:            taskID,
		IntegrationBranch: integrationBranch,
		BaseCommit:        strings.TrimSpace(base),
		HeadCommit:        strings.TrimSpace(head),
		Files:             files,
	}, nil
}

// ErrNeedConfirmation is returned when a merge, push or rebase of the
// user branch is attempted without explicit user confirmation.
var ErrNeedConfirmation = fmt.Errorf("workspace: merging into the user branch needs user confirmation")

// ConfirmMerge merges the integration branch into target only with
// explicit confirmation; otherwise it returns ErrNeedConfirmation
// and changes nothing.
func (m *WorktreeManager) ConfirmMerge(projectRoot, integrationBranch, target string, confirmed bool) error {
	if !confirmed {
		return ErrNeedConfirmation
	}
	if _, err := m.git().Run(projectRoot, "checkout", "-q", target); err != nil {
		return err
	}
	_, err := m.git().Run(projectRoot, "merge", "--no-ff", "-q", integrationBranch)
	return err
}
