package tools

import (
	"reflect"
	"testing"
)

func TestEnvironmentFlavor(t *testing.T) {
	if normalizeEnvKey("Path", true) != normalizeEnvKey("PATH", true) {
		t.Fatal("Windows keys must fold case")
	}
	if normalizeEnvKey("Path", false) == normalizeEnvKey("PATH", false) {
		t.Fatal("POSIX keys must retain case")
	}
	base := []string{"Path=first", "PATH=second", "HOME=home"}
	over := map[string]string{"path": "override"}
	extra := []string{"pAtH=last"}
	if got := mergeEnvForOS(base, over, extra, true); !reflect.DeepEqual(got, []string{"pAtH=last", "HOME=home"}) {
		t.Fatal(got)
	}
	if got := mergeEnvForOS(base, over, extra, false); !reflect.DeepEqual(got, []string{"Path=first", "PATH=second", "HOME=home", "path=override", "pAtH=last"}) {
		t.Fatal(got)
	}
}

func TestWindowsHiddenDriveEnvironmentPortable(t *testing.T) {
	base := []string{`=C:=C:\one`, `=D:=D:\two`, `Path=old`}
	want := []string{`=C:=C:\last`, `=D:=D:\two`, `PATH=new`}
	got := mergeEnvForOS(base, map[string]string{"PATH": "new"}, []string{`=c:=C:\last`}, true)
	want[0] = `=c:=C:\last`
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}
