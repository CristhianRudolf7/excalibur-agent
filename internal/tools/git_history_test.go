package tools

import (
	"context"
	"encoding/json"
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
