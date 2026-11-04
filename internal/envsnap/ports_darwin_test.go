//go:build darwin

package envsnap

import (
	"fmt"
	"net"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestDarwinLsofListeningPorts(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want []string
	}{
		{"", nil},
		{"p100\nf7\nn*:8080\nn127.0.0.1:80\nn[::1]:8080\nn[::]:443\nn*:80\n", []string{"tcp:443", "tcp:80", "tcp:8080"}},
		{"n*:65536\nn*:bad\nn*:https\nn\nn8080\nn:23\nn127.0.0.1:80->127.0.0.1:9000\n", nil},
	} {
		if got := parseLsofListeningPorts(tc.raw); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("parse %q: %v, want %v", tc.raw, got, tc.want)
		}
	}
	var raw strings.Builder
	for p := 1; p <= 30; p++ {
		fmt.Fprintf(&raw, "n*:%d\n", p)
	}
	got := parseLsofListeningPorts(raw.String())
	if len(got) != 20 || !slices.IsSorted(got) || got[0] != "tcp:1" || got[19] != "tcp:27" {
		t.Fatalf("bounded sorted output: %v", got)
	}
}

func TestDarwinListeningPortDiscovery(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	// Other services on the runner may crowd out our ephemeral port when
	// the twenty-entry cap is reached; fixed parser tests cover that cap.
	ports := listeningPorts()
	if len(ports) == 0 {
		t.Fatal("no TCP listeners discovered")
	}
	if len(ports) < 20 && !slices.Contains(ports, fmt.Sprintf("tcp:%d", listener.Addr().(*net.TCPAddr).Port)) {
		t.Fatalf("own listener missing: %v", ports)
	}
}
