package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsDetachedShellOutput(t *testing.T) {
	for _, shell := range []string{defaultShell(), DetectShell()} {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "output.log")
			if err := os.WriteFile(path, []byte("existing output\n"), 0600); err != nil {
				t.Fatal(err)
			}
			log, err := openSessionLog(path)
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			c := exec.Command(shell, shellArgs(shell, "echo detached-probe")...)
			c.SysProcAttr = detachedSysProcAttr()
			c.Stdout, c.Stderr = log, log
			prepareShellCommand(c, shell, "echo detached-probe")
			err = c.Run()
			data, readErr := os.ReadFile(path)
			if err != nil || readErr != nil || !strings.HasPrefix(string(data), "existing output\n") || !strings.Contains(string(data), "detached-probe") {
				t.Fatalf("run=%v read=%v output=%q", err, readErr, data)
			}
		})
	}
}
