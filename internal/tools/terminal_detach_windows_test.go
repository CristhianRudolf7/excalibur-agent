package tools

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

func init() {
	// Use the test executable instead of requiring a Python installation.
	detachedTestServerCommand = func(port int) string {
		executable := os.Args[0]
		switch windowsShellName(DetectShell()) {
		case "bash", "sh", "zsh":
			executable = shellQuote(filepath.ToSlash(executable))
		case "powershell", "pwsh":
			executable = "& " + shellQuote(executable)
		default:
			executable = `"` + executable + `"`
		}
		return fmt.Sprintf("%s -test.run=^TestWindowsDetachedHTTPHelper$ -- %d", executable, port)
	}
}

func TestWindowsDetachedHTTPHelper(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	port, err := strconv.Atoi(os.Args[len(os.Args)-1])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("EXCALIBUR_DETACH_WORKDIR"), "fixture-parent.pid"), []byte(strconv.Itoa(os.Getppid())), 0600); err != nil {
		t.Fatal(err)
	}
	requested := make(chan struct{}, 1)
	server := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", port), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "detached server ready")
		select {
		case requested <- struct{}{}:
		default:
		}
	})}
	// Exit after serving the survival check, and bound the helper's lifetime if
	// the parent test fails before making its request.
	safego.Go("windows-detached-http-cleanup", nil, func() {
		select {
		case <-requested:
			time.Sleep(time.Second)
		case <-time.After(30 * time.Second):
		}
		_ = server.Close()
	})
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
	os.Exit(0)
}

// Wait for the shell to release its working directory before TempDir cleanup.
func prepareDetachedServerCleanup(t *testing.T, workdir string) {
	t.Helper()
	t.Cleanup(func() {
		raw, err := os.ReadFile(filepath.Join(workdir, "fixture-parent.pid"))
		if err != nil {
			t.Errorf("detached fixture parent: %v", err)
			return
		}
		pid, err := strconv.Atoi(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		defer windows.CloseHandle(h)
		state, err := windows.WaitForSingleObject(h, 5000)
		if err != nil || state != windows.WAIT_OBJECT_0 {
			t.Errorf("detached fixture shell still running: state=%d error=%v", state, err)
		}
	})
}
