package platform

import (
	"os/exec"
	"reflect"
	"testing"
)

func TestWindowsIsolatorPassThrough(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "echo hi")
	before := append([]string(nil), cmd.Args...)
	if err := (WindowsIsolator{}).Isolate(cmd); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cmd.Args, before) {
		t.Fatal("command changed")
	}
	if err := (WindowsIsolator{}).Isolate(nil); err != nil {
		t.Fatal(err)
	}
}
