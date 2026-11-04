package envsnap

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

func cgroupsSupported() bool { return false }
func platformDistro() (string, bool) {
	v := windows.RtlGetVersion()
	return fmt.Sprintf("Windows %d.%d (build %d)", v.MajorVersion, v.MinorVersion, v.BuildNumber), true
}
func probeEnvironment() []string {
	env := []string{"LC_ALL=C"}
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		for _, allowed := range []string{"PATH", "SystemRoot", "SystemDrive", "WINDIR", "ComSpec", "PATHEXT", "TEMP", "TMP"} {
			if strings.EqualFold(name, allowed) {
				env = append(env, entry)
				break
			}
		}
	}
	return env
}
func shellCandidates() []string {
	return []string{"pwsh", "powershell", "cmd", "bash", "zsh", "fish", "sh", "dash"}
}
func shellPreferences() []string { return []string{"pwsh", "powershell", "cmd", "bash", "zsh", "sh"} }
func shellFallback() string {
	if shell := os.Getenv("ComSpec"); shell != "" {
		return shell
	}
	return "cmd.exe"
}
