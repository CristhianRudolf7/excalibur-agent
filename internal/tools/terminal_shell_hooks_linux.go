package tools

import (
	"os"
	"os/exec"
	"path/filepath"
)

func detectShellOS() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		base := filepath.Base(sh)
		if base == "bash" || base == "zsh" || base == "fish" || base == "sh" {
			if _, err := exec.LookPath(sh); err == nil {
				return sh
			}
			if _, err := exec.LookPath(base); err == nil {
				return base
			}
		}
	}
	for _, cand := range []string{"bash", "zsh", "sh"} {
		if p, err := exec.LookPath(cand); err == nil {
			return p
		}
	}
	return defaultShell()
}

func prepareShellCommand(c *exec.Cmd, shell, script string) {}
