package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func windowsShellName(shell string) string {
	name := strings.ToLower(filepath.Base(strings.ReplaceAll(shell, `\`, "/")))
	return strings.TrimSuffix(name, ".exe")
}

func shellArgs(shell, script string) []string {
	switch windowsShellName(shell) {
	case "bash", "sh", "zsh":
		return []string{"-c", script}
	case "pwsh", "powershell":
		return []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script}
	default:
		return []string{"/d", "/s", "/c", script}
	}
}

func defaultShell() string {
	if shell := os.Getenv("ComSpec"); shell != "" {
		return shell
	}
	return `C:\Windows\System32\cmd.exe`
}

func detectShellOS() string { return selectWindowsShell(os.Getenv, exec.LookPath) }

// selectWindowsShell takes its environment and resolver explicitly so discovery
// can be tested without depending on the runner's installed shells.
func selectWindowsShell(getenv func(string) string, lookup func(string) (string, error)) string {
	systemRoot := getenv("SystemRoot")
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	wsl := filepath.Join(systemRoot, "System32", "bash.exe")
	resolve := func(candidate string) string {
		p, err := lookup(candidate)
		if err != nil || strings.EqualFold(filepath.Clean(p), filepath.Clean(wsl)) {
			return ""
		}
		return p
	}
	if sh := getenv("SHELL"); sh != "" {
		switch windowsShellName(sh) {
		case "bash", "zsh", "sh":
			if p := resolve(sh); p != "" {
				return p
			}
			if p := resolve(windowsShellName(sh) + ".exe"); p != "" {
				return p
			}
		}
	}
	candidates := []string{"bash.exe"}
	if p := getenv("ProgramFiles"); p != "" {
		candidates = append(candidates, filepath.Join(p, "Git", "bin", "bash.exe"))
	}
	if p := getenv("LocalAppData"); p != "" {
		candidates = append(candidates, filepath.Join(p, "Programs", "Git", "bin", "bash.exe"))
	}
	candidates = append(candidates, "pwsh.exe", "powershell.exe")
	for _, candidate := range candidates {
		if p := resolve(candidate); p != "" {
			return p
		}
	}
	if p := getenv("ComSpec"); p != "" {
		return p
	}
	if p := resolve("cmd.exe"); p != "" {
		return p
	}
	return `C:\Windows\System32\cmd.exe`
}

// cmd.exe parses its own command line, not CommandLineToArgvW quoting. Keep
// script quotes and metacharacters verbatim inside the /s /c outer quotes.
func prepareShellCommand(c *exec.Cmd, shell, script string) {
	if windowsShellName(shell) != "cmd" {
		return
	}
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.CmdLine = syscall.EscapeArg(shell) + ` /d /s /c "` + script + `"`
}
