package envsnap

import (
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
)

func TestWindowsSnapshotProbes(t *testing.T) {
	if got := detectDistro(); !strings.HasPrefix(got, "Windows ") || !strings.Contains(got, "build ") {
		t.Fatal(got)
	}
	t.Setenv("Path", os.Getenv("PATH"))
	t.Setenv("ComSpec", os.Getenv("ComSpec"))
	t.Setenv("ENVSNAP_SENTINEL_SECRET", "private")
	env := strings.ToLower(strings.Join(probeEnvironment(), "\n"))
	for _, name := range []string{"path=", "systemroot=", "comspec=", "pathext="} {
		if !strings.Contains(env, name) {
			t.Fatal("missing " + name)
		}
	}
	if strings.Contains(env, "envsnap_sentinel_secret=") {
		t.Fatal("secret inherited")
	}
	if shell := pickDefaultShell([]string{"cmd", "powershell", "pwsh"}); !strings.Contains(strings.ToLower(shell), "pwsh") {
		t.Fatal(shell)
	}
	if cgroupsSupported() {
		t.Fatal("cgroups enabled on Windows")
	}
}
func TestWindowsListeningPorts(t *testing.T) {
	// The ephemeral port may fall beyond the public 20-entry limit, so inspect
	// complete native tables here as well as checking the bounded snapshot.
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	for _, tt := range []struct {
		tcp      bool
		port     int
		protocol string
	}{
		{true, listener.Addr().(*net.TCPAddr).Port, "tcp"},
		{false, udp.LocalAddr().(*net.UDPAddr).Port, "udp"},
	} {
		want := fmt.Sprintf("%s:%d", tt.protocol, tt.port)
		ports := parseWindowsPortTable(windowsPortTable(false, tt.tcp), false, tt.tcp)
		found := false
		for _, port := range ports {
			if port == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s from %v", want, ports)
		}
	}
	if got := listeningPorts(); len(got) > 20 {
		t.Fatal(got)
	}
}
