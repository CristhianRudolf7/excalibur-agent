//go:build darwin

package tools

import (
	"net"
	"os"
	"reflect"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDarwinLsofPIDs(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want []int
	}{
		{"", nil},
		{"p42\nf7\np100\nf8\np42\np0\np-2\npbad\np\n", []int{42, 100}},
	} {
		if got := parseLsofPIDs(tc.raw); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("parse %q: %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestDarwinPSArgs(t *testing.T) {
	got := parseDarwinPSArgs("  12 /usr/local/bin/go build ./...\n 13\tpython3 -m pip install foo\n bad input\n0 ignored\n14\n")
	want := map[int]string{12: "/usr/local/bin/go build ./...", 13: "python3 -m pip install foo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args: %v", got)
	}
}

func TestDarwinProcessTable(t *testing.T) {
	pgid, err := syscall.Getpgid(0)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range readProcessTable() {
		if p.PID == os.Getpid() {
			if p.PPID != os.Getppid() || p.PGID != pgid || p.Comm == "" || p.Argv == "" {
				t.Fatalf("own process: %+v, want ppid=%d pgid=%d", p, os.Getppid(), pgid)
			}
			return
		}
	}
	t.Fatal("own process missing")
}

func TestDarwinControllingTerminal(t *testing.T) {
	var p unix.KinfoProc
	for _, tc := range []struct {
		flag, dev int32
		want      bool
	}{
		{0, 1, false}, {darwinPControlT, -1, false}, {darwinPControlT, 0, true}, {darwinPControlT, 123, true},
	} {
		p.Proc.P_flag, p.Eproc.Tdev = tc.flag, tc.dev
		if got := darwinHasControllingTerminal(p); got != tc.want {
			t.Fatalf("flag=%d dev=%d: %v", tc.flag, tc.dev, got)
		}
	}
	if processHoldsTerminal(-1) || groupAlive(-1) || groupAlive(0) || groupAlive(1) {
		t.Fatal("invalid process accepted")
	}
	for _, pgid := range []int{-1, 0, 1} {
		if err := signalGroup(pgid, true); err != syscall.EINVAL {
			t.Fatalf("invalid group %d: %v", pgid, err)
		}
	}
	if err := signalGroup(1<<30, false); err != nil || groupAlive(1<<30) {
		t.Fatalf("absent group: %v", err)
	}
}

func TestDarwinProcessTreeListener(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if !processTreeListeningTCP(os.Getpid()) {
		t.Fatal("own TCP listener missing")
	}
	if processTreeListeningTCP(-1) {
		t.Fatal("invalid tree reports listener")
	}
}
