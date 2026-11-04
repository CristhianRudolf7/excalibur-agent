//go:build darwin

package tools

import "testing"

func TestDarwinTerminalGates(t *testing.T) {
	if !survivableSessionsSupported() || !detachedBackgroundSupported() {
		t.Fatal("Unix session detachment disabled")
	}
	if namespaceIsolationSupported() {
		t.Fatal("Linux namespaces enabled on Darwin")
	}
}
