package tools

import (
	"reflect"
	"testing"
)

func TestWindowsEnvironmentMerge(t *testing.T) {
	base := []string{"Path=old", "TEMP=temp"}
	if !envKeySet(base, "PATH") {
		t.Fatal("Path was not found as PATH")
	}
	if got := mergeEnv(base, map[string]string{"PATH": "new"}, []string{"path=last"}); !reflect.DeepEqual(got, []string{"path=last", "TEMP=temp"}) {
		t.Fatal(got)
	}
}
