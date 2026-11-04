package platform

import (
	"golang.org/x/sys/windows"
	"testing"
)

func TestServiceCommandWindowsArgv(t *testing.T) {
	executable := `C:\Program Files\A & B\excalibur.exe`
	args, err := windows.DecomposeCommandLine(windowsDaemonCommand(executable))
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 3 || args[0] != executable || args[1] != "daemon" || args[2] != "start" {
		t.Fatalf("task argv = %q", args)
	}
}
