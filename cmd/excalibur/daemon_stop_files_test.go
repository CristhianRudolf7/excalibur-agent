package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
)

func TestStoppedDaemonRendezvousCleanup(t *testing.T) {
	for _, tc := range []struct {
		name, pid string
		keep      bool
	}{
		{"stopped PID", "123\n", false},
		{"new daemon PID", "456\n", true},
		{"PID already removed", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := daemon.WriteAddrFile(dir, "127.0.0.1:12345"); err != nil {
				t.Fatal(err)
			}
			pidPath := filepath.Join(dir, daemonPidFile)
			if tc.pid != "" {
				if err := os.WriteFile(pidPath, []byte(tc.pid), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := clearStoppedDaemonFiles(dir, 123); err != nil {
				t.Fatal(err)
			}
			if got := daemon.ReadAddrFile(dir); (got != "") != tc.keep {
				t.Fatalf("address file = %q; keep = %v", got, tc.keep)
			}
			_, err := os.Stat(pidPath)
			if tc.keep && err != nil {
				t.Fatal("removed replacement daemon PID")
			}
			if !tc.keep && !os.IsNotExist(err) {
				t.Fatalf("stale PID file: %v", err)
			}
		})
	}
}
