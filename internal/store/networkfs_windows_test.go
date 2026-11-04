package store

import "testing"

func TestWindowsNetworkFS(t *testing.T) {
	for _, dir := range []string{`\\server.invalid\share\data`, `\\?\UNC\server.invalid\share\data`} {
		if !detectNetworkFS(dir) {
			t.Fatalf("UNC store was not detected: %q", dir)
		}
	}
	// The runner's temporary directory is expected to be on its local disk.
	if dir := t.TempDir(); detectNetworkFS(dir) {
		t.Fatalf("local temporary directory reported as remote: %q", dir)
	}
}
