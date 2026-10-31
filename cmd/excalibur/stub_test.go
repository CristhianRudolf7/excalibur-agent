package main

import (
	"os"
	"testing"
)

// TestMain pins the command test environment to the explicit stub
// provider and isolates resolution from the operator's real
// credentials file: ordinary tests must never call paid APIs.
func TestMain(m *testing.M) {
	_ = os.Setenv("EXCALIBUR_PROVIDER", "stub")
	_ = os.Setenv("EXCALIBUR_CREDENTIALS_FILE", "/nonexistent-operator-credentials.json")
	os.Exit(m.Run())
}
