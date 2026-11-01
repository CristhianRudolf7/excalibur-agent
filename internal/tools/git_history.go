package tools

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const GitHistoryWarning = "warning: this rewrites git history; the instruction does not ask for that, and other checks may compare commits or files"

type runInstructionKey struct{}
type gitHistoryRunKey struct{}

// WithGitHistoryRunID scopes history confirmations when a tool is shared by runs.
func WithGitHistoryRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, gitHistoryRunKey{}, runID)
}

// WithRunInstruction supplies literal task context to advisory exec checks.
func WithRunInstruction(ctx context.Context, instruction string) context.Context {
	return context.WithValue(ctx, runInstructionKey{}, instruction)
}

var historyInstruction = regexp.MustCompile(`(?i)\b(?:history|commits?|rewrit\w*)\b`)

// GitHistoryRewriteWarning is advisory only. It never changes execution or
// result status and applies equally to interactive and autonomous callers.
func GitHistoryRewriteWarning(command, instruction string) string {
	if off("EXCALIBUR_GIT_HISTORY_WARNING") {
		return ""
	}
	if gitHistoryRewriteForm(command, instruction) != "" {
		return GitHistoryWarning
	}
	return ""
}

func gitHistoryRewriteForm(command, instruction string) string {
	if historyInstruction.MatchString(instruction) {
		return ""
	}
	for _, stage := range execStageSplit(command) {
		name, args := stageName(stage)
		if filepath.Base(name) != "git" {
			continue
		}
		args = args[1:]
		for len(args) > 0 && strings.HasPrefix(args[0], "-") {
			flag := args[0]
			args = args[1:]
			if flag == "-C" || flag == "-c" || flag == "--git-dir" || flag == "--work-tree" {
				if len(args) > 0 {
					args = args[1:]
				}
			}
		}
		if len(args) == 0 {
			continue
		}
		sub, flags := args[0], args[1:]
		has := func(want string) bool {
			for _, f := range flags {
				if strings.Trim(f, "\"'") == want {
					return true
				}
			}
			return false
		}
		rewrite := false
		switch sub {
		case "filter-branch", "filter-repo":
			rewrite = true
		case "rebase":
			rewrite = has("-i") || has("--interactive") || has("--root")
		case "reset":
			if has("--hard") {
				for _, f := range flags {
					if !strings.HasPrefix(f, "-") && f != "HEAD" {
						rewrite = true
						break
					}
				}
			}
		case "push":
			rewrite = has("--force") || has("-f") || has("--force-with-lease")
		case "commit":
			rewrite = has("--amend")
		case "update-ref":
			rewrite = has("-d")
		case "reflog":
			rewrite = len(flags) > 0 && flags[0] == "expire"
		case "gc":
			rewrite = has("--prune=now")
		}
		if rewrite {
			return "git " + strings.Join(args, " ")
		}
	}
	return ""
}

// historyConfirmations belongs to one exec tool and scopes acknowledgements to
// its run, workdir and exact command. A different command needs its own retry.
type historyConfirmations struct {
	mu   sync.Mutex
	seen map[string]bool
}

func (t *ExecTool) historyRefusal(ctx context.Context, command, workdir, instruction string) string {
	if off("EXCALIBUR_GIT_HISTORY_CONFIRM") {
		return ""
	}
	form := gitHistoryRewriteForm(command, instruction)
	if form == "" {
		return ""
	}
	runID, _ := ctx.Value(gitHistoryRunKey{}).(string)
	if runID == "" {
		runID = t.RunID
	}
	key := runID + "\x00" + workdir + "\x00" + command
	t.historyConfirm.mu.Lock()
	defer t.historyConfirm.mu.Unlock()
	if t.historyConfirm.seen[key] {
		return ""
	}
	if t.historyConfirm.seen == nil {
		t.historyConfirm.seen = make(map[string]bool)
	}
	t.historyConfirm.seen[key] = true
	return "not run: this command rewrites git history (" + form + "), which the instruction does not ask for and which destroys the original commits other checks may rely on. If you are sure it is required, run the exact same command again."
}
