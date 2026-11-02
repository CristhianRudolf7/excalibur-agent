package cli

import (
	"os"
	"testing"
)

// TestMain pins the package test environment to the explicit stub
// provider (Section 19 contract: the stub runs only when explicitly
// selected) and isolates resolution from the operator's real
// credentials file. Tests needing other wiring set their own
// EXCALIBUR_* variables with t.Setenv or override resolveSettings.
func TestMain(m *testing.M) {
	_ = os.Setenv("EXCALIBUR_PROVIDER", "stub")
	_ = os.Setenv("EXCALIBUR_CREDENTIALS_FILE", "/nonexistent-operator-credentials.json")
	// Embedded tests must not discover a live daemon or another test worker's
	// rendezvous file. Routing tests replace this address with their fake.
	_ = os.Setenv("EXCALIBUR_DAEMON_ADDR", "127.0.0.1:1")
	os.Exit(m.Run())
}
