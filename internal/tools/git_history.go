package tools

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
)

const GitHistoryWarning = "warning: this rewrites git history; the instruction does not ask for that, and other checks may compare commits or files"

type runInstructionKey struct{}

// WithRunInstruction supplies literal task context to advisory exec checks.
func WithRunInstruction(ctx context.Context, instruction string) context.Context {
	return context.WithValue(ctx, runInstructionKey{}, instruction)
}

var historyInstruction = regexp.MustCompile(`(?i)\b(?:history|commits?|rewrit\w*)\b`)

// GitHistoryRewriteWarning is advisory only. It never changes execution or
// result status and applies equally to interactive and autonomous callers.
func GitHistoryRewriteWarning(command, instruction string) string {
	if off("EXCALIBUR_GIT_HISTORY_WARNING") || historyInstruction.MatchString(instruction) {
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
			return GitHistoryWarning
		}
	}
	return ""
}
