package tasks

import "testing"

func TestWindowsExecutableExtensions(t *testing.T) {
	for _, tt := range []struct {
		path, pathext string
		want          bool
	}{
		{`C:\build\runner.EXE`, "", true}, {`C:\build\run.cmd`, "", true},
		{`\\server\share\run.Com`, "", true}, {"run.bat", "", true},
		{"run", "", false}, {"report.txt", "", false}, {"script.ps1", "", false},
		{`C:\with.exe\report.txt`, "", false},
		{"runner.exe", ".CMD;.PY", false}, {"script.Py", ".CMD; .PY ", true},
	} {
		if got := windowsExecutableExtension(tt.path, tt.pathext); got != tt.want {
			t.Errorf("%q PATHEXT=%q: got %v, want %v", tt.path, tt.pathext, got, tt.want)
		}
	}
}
