package cli

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func TestWindowsDaemonDiscovery(t *testing.T) {
	root, err := (platform.WindowsPaths{}).RuntimeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cliRuntimeDir(), daemon.RuntimeAddrDir(root); got != want {
		t.Fatalf("runtime directory = %q; want %q", got, want)
	}
	t.Setenv(daemon.AddrEnv, "127.0.0.1:12345")
	got, ok := daemonAddr()
	if !ok || got != "127.0.0.1:12345" {
		t.Fatalf("address override = %q, %v", got, ok)
	}
}
