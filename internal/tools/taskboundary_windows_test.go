package tools

import "testing"

func TestWindowsTaskBoundaryIntegration(t *testing.T) {
	restore := SetExecScope(ExecScope{Active: true, HarnessPaths: []string{`D:\Harness\logs`, `\\server\share\state`}})
	defer restore()
	t.Setenv(TaskBoundarySwitch, "1")
	for _, command := range []string{`type "C:\Program Files\private"`, `type \\?\C:\Windows\System32\config`, `type \\server\share\state\log`, `C:\bin\nsenter.exe`} {
		if err := CheckCommandBoundary(command); err == nil {
			t.Errorf("accepted %s", command)
		}
	}
	if err := CheckCommandBoundary(`type C:\workspace\file`); err != nil {
		t.Fatal(err)
	}
}
