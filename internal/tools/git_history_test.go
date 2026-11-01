package tools

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitHistoryRewriteWarnings(t *testing.T) {
	commands := []string{
		"git filter-branch --all", "git filter-repo --path old", "git rebase -i HEAD~3", "git rebase --root", "git rebase --interactive HEAD~2",
		"git reset --hard HEAD~1", "git push --force origin main", "git push -f", "git commit --amend", "git update-ref -d refs/heads/old", "git reflog expire --expire=now --all", "git gc --prune=now", "cd repo && git -C . commit --amend",
	}
	for _, cmd := range commands {
		if got := GitHistoryRewriteWarning(cmd, "Sanitize repository content"); got != GitHistoryWarning {
			t.Errorf("no warning: %s", cmd)
		}
		for _, request := range []string{"Rewrite the repository history", "Remove sensitive data from all commits"} {
			if got := GitHistoryRewriteWarning(cmd, request); got != "" {
				t.Errorf("requested history: %s", got)
			}
		}
	}
	for _, cmd := range []string{"git status", "git reset --hard", "git reset --hard HEAD", "git push origin main", "git rebase main", "echo git filter-branch"} {
		if got := GitHistoryRewriteWarning(cmd, "Edit files"); got != "" {
			t.Errorf("unexpected warning for %s", cmd)
		}
	}
	t.Setenv("EXCALIBUR_GIT_HISTORY_WARNING", "0")
	if GitHistoryRewriteWarning(commands[0], "Edit files") != "" {
		t.Fatal("switch ignored")
	}
}

func TestExecHistoryWarningIsAdvisory(t *testing.T) {
	t.Setenv("EXCALIBUR_GIT_HISTORY_CONFIRM", "0")
	// A shell function simulates git without touching any repository.
	tool := execTestTool(t)
	defer closeSessions(tool.Sessions)
	raw, _ := json.Marshal(map[string]any{"command": "git() { :; }; git commit --amend"})
	for _, request := range []string{"Edit the files", "Rewrite history"} {
		res, err := tool.Invoke(WithRunInstruction(context.Background(), request), Invocation{Arguments: raw})
		if err != nil || res.Status != StatusOK {
			t.Fatalf("warning changed result: %+v %v", res, err)
		}
		if strings.Contains(res.Output, GitHistoryWarning) != (request == "Edit the files") {
			t.Fatal(res.Output)
		}
	}
}

func TestExecHistoryConfirmationBeforeExecution(t *testing.T) {
	for _, requested := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirm", true: "requested"}[requested], func(t *testing.T) {
			dir := t.TempDir()
			git := func(args ...string) string {
				t.Helper()
				cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git: %v: %s", err, out)
				}
				return strings.TrimSpace(string(out))
			}
			git("init")
			git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "original")
			head := git("rev-parse", "HEAD")
			tool := execTestTool(t)
			defer closeSessions(tool.Sessions)
			tool.Workdir = dir
			tool.RunID = "run-one"
			command := "git -c user.name=Test -c user.email=test@example.invalid commit --amend --allow-empty -m changed"
			raw, _ := json.Marshal(map[string]any{"command": command, "workdir": filepath.Clean(dir)})
			request := "Update repository files"
			if requested {
				request = "Rewrite the last commit"
			}
			ctx := WithRunInstruction(context.Background(), request)
			first, err := tool.Invoke(ctx, Invocation{Arguments: raw})
			if err != nil {
				t.Fatal(err)
			}
			if requested {
				if first.Status != StatusOK || git("rev-parse", "HEAD") == head {
					t.Fatalf("requested rewrite did not run: %+v", first)
				}
				return
			}
			if first.Status != StatusToolError || !strings.HasPrefix(first.Output, "not run:") || git("rev-parse", "HEAD") != head {
				t.Fatalf("first call must refuse without executing: %+v", first)
			}
			second, err := tool.Invoke(ctx, Invocation{Arguments: raw})
			if err != nil || second.Status != StatusOK || git("rev-parse", "HEAD") == head || !strings.Contains(second.Output, GitHistoryWarning) {
				t.Fatalf("identical retry must execute with warning: %+v %v", second, err)
			}
			tool.RunID = "run-two"
			third, _ := tool.Invoke(ctx, Invocation{Arguments: raw})
			if !strings.HasPrefix(third.Output, "not run:") {
				t.Fatal("confirmation leaked across runs")
			}
		})
	}
}
