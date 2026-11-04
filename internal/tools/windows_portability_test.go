package tools

import (
	"runtime"
	"testing"
)

// requirePOSIXFeature retains Linux assertions for features outside Windows.
func requirePOSIXFeature(t *testing.T, feature string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip(feature + " requires POSIX facilities")
	}
}
