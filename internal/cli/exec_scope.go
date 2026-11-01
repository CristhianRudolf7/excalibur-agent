package cli

import (
	"os"
	"path/filepath"
)

// ExecScopePaths lists the harness's own log and state locations that the
// exec scope keeps out of the agent's reach (item 93): the trial log
// directory passed with --logs-dir, the EXCALIBUR_LOGS directory, the Harbor
// mount /logs/agent, and the excalibur user state directory. Relative paths
// are made absolute. The filesystem root and the temporary directory are
// never listed, because refusing them would refuse the task itself.
func ExecScopePaths(logsFlag string) []string {
	candidates := []string{logsFlag, os.Getenv("EXCALIBUR_LOGS"), "/logs/agent"}
	if dir, err := userStateDir(); err == nil {
		candidates = append(candidates, dir)
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range candidates {
		if c == "" {
			continue
		}
		abs, err := filepath.Abs(c)
		if err != nil || abs == "/" || abs == filepath.Clean(os.TempDir()) || seen[abs] {
			continue
		}
		seen[abs] = true
		out = append(out, abs)
	}
	return out
}
