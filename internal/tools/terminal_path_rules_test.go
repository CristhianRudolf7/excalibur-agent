package tools

import (
	"reflect"
	"testing"
)

func TestWindowsBoundaryPathsPortable(t *testing.T) {
	roots := []string{`C:\Windows`, `C:\Program Files`, `C:\Program Files (x86)`, `C:\ProgramData`}
	harness := []string{`D:\Harness\logs`, `\\server\share\state`}
	for _, tt := range []struct {
		path    string
		outside bool
	}{
		{`C:\Windows\System32\config`, true}, {`/c/Windows/System32/config`, true}, {`/cygdrive/c/Windows/file`, true}, {`C:\work\.. \Windows\file`, true}, {`C:\..\Windows\file`, true}, {`\Windows\file`, true}, {`c:/WINDOWS/system32`, true},
		{`C:\Windows.\System32`, true}, {`C:\Windows::$INDEX_ALLOCATION`, true},
		{`C:\work\..\Windows\file`, true}, {`C:\Program Files\Git`, true},
		{`C:\Program Files (x86)\tool`, true}, {`C:\ProgramData\file`, true},
		{`\\?\C:\Windows\file`, true}, {`\\?\UNC\server\share\state\log`, true},
		{`\\server\share\state\log`, true}, {`D:\HARNESS\logs\secret`, true},
		{`\\.\PhysicalDrive0`, true}, {`\\?\GLOBALROOT\Device\HarddiskVolume1`, true},
		{`C:\workspace\file`, false}, {`C:\WindowsBackup\file`, false},
		{`\\server\share\project\file`, false}, {`relative\file`, false},
	} {
		if got := windowsPathOutsideBoundary(tt.path, harness, roots); got != tt.outside {
			t.Errorf("%s: %v", tt.path, got)
		}
	}
}

func TestWindowsBoundaryWordsPortable(t *testing.T) {
	got := windowsBoundaryWords(`type "C:\Program Files\secret" & type '\\?\UNC\server\share\state\log'`)
	want := []string{"type", `C:\Program Files\secret`, "type", `\\?\UNC\server\share\state\log`}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

func TestCommandBasePortable(t *testing.T) {
	for _, name := range []string{`C:\bin\GREP.EXE`, `C:/bin/grep.cmd`, `\\host\share\grep.bat`, `grep.ps1`} {
		if got := commandBaseForOS(name, true); got != "grep" {
			t.Errorf("%s -> %s", name, got)
		}
	}
	if got := commandBaseForOS(`/usr/bin/grep.exe`, false); got != "grep.exe" {
		t.Fatal(got)
	}
	if got := commandBaseForOS(`dir\grep.exe`, false); got != `dir\grep.exe` {
		t.Fatal(got)
	}
}

func TestWindowsStageWordsPortable(t *testing.T) {
	got := windowsLexicalWords(`PATH="C:\Program Files\bin" "C:\Program Files\grep.exe" pattern`, " \t\r\n")
	want := []string{`PATH=C:\Program Files\bin`, `C:\Program Files\grep.exe`, "pattern"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	if commandBaseForOS(got[1], true) != "grep" {
		t.Fatal(got[1])
	}
}
