package cli

import (
	"context"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestDetachedChildHasNoConsole(t *testing.T) {
	if os.Getenv("EXCALIBUR_TEST_DETACHED_CHILD") == "1" {
		var mode uint32
		handle, err := windows.CreateFile(windows.StringToUTF16Ptr("CONIN$"), windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
		if err == nil {
			defer windows.CloseHandle(handle)
			if windows.GetConsoleMode(handle, &mode) == nil {
				t.Fatal("detached child retained a console")
			}
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDetachedChildHasNoConsole$")
	cmd.Env = append(os.Environ(), "EXCALIBUR_TEST_DETACHED_CHILD=1")
	cmd.SysProcAttr = newChildAttr()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("detached child: %v: %s", err, output)
	}
}
