package tools

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

// waitSessionExit polls until the session reports an exit status.
func waitSessionExit(t *testing.T, s *Session) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if code, exited := s.Exit(); exited {
			return code
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("session %s did not exit", s.ID)
	return 0
}

func TestSignalDeathReportsShellStatus(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux signals")
	}
	m := NewSessionManager(TerminalDeps{})
	s, _, _ := m.Start("test", "sleep 30", "", false, 250, nil)
	defer m.Close(s.ID)
	if err := m.Signal(s.ID, "kill"); err != nil {
		t.Fatal(err)
	}
	code := waitSessionExit(t, s)
	if code != 128+9 {
		t.Fatalf("SIGKILL death reported as %d, want 137", code)
	}
	sig, ok := s.ExitSignal()
	if !ok || sig != int(syscall.SIGKILL) || SignalName(sig) != "SIGKILL" {
		t.Fatalf("ExitSignal = %d, %v", sig, ok)
	}

	term, _, _ := m.Start("test", "sleep 30", "", false, 250, nil)
	defer m.Close(term.ID)
	if err := m.Signal(term.ID, "terminate"); err != nil {
		t.Fatal(err)
	}
	if code := waitSessionExit(t, term); code != 128+15 {
		t.Fatalf("SIGTERM death reported as %d, want 143", code)
	}
}

func TestNormalExitKeepsCodeAndHasNoSignal(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux signals")
	}
	m := NewSessionManager(TerminalDeps{})
	s, _, _ := m.Start("test", "exit 7", "", false, 250, nil)
	defer m.Close(s.ID)
	if code := waitSessionExit(t, s); code != 7 {
		t.Fatalf("exit code = %d, want 7", code)
	}
	if _, ok := s.ExitSignal(); ok {
		t.Fatal("normal exit reported a signal")
	}
}

func TestSignalNameTable(t *testing.T) {
	if SignalName(11) != "SIGSEGV" || SignalName(15) != "SIGTERM" || SignalName(99) != "SIG99" {
		t.Fatalf("names: %s %s %s", SignalName(11), SignalName(15), SignalName(99))
	}
}

func TestNonTTYStdinIsDevNull(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux fd inspection")
	}
	// A program that reads stdin must see end of file at once: stdin is the
	// null device for every non-tty session, so a prompt cannot hang it.
	m := NewSessionManager(TerminalDeps{})
	s, out, alive := m.Start("test", "readlink /proc/self/fd/0", "", false, 2000, nil)
	defer m.Close(s.ID)
	if alive || !strings.Contains(out, "/dev/null") {
		t.Fatalf("stdin target: alive=%v out=%q", alive, out)
	}
}

func TestSilentSessionOnTerminalGetsPromptNote(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux terminals")
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Skip("pty unavailable:", err)
	}
	defer master.Close()
	defer slave.Close()
	// The shell keeps the terminal device open on fd 3, the way a program
	// that prompts through /dev/tty does, and then stays silent.
	m := NewSessionManager(TerminalDeps{})
	s, out, alive := m.Start("test", fmt.Sprintf("exec 3<%s; sleep 20", slave.Name()), "", false, 250, nil)
	defer m.Close(s.ID)
	if !alive || !strings.Contains(out, "waiting for terminal input") {
		t.Fatalf("prompt note missing: alive=%v out=%q", alive, out)
	}
}

func TestSilentSessionWithoutTerminalHasNoPromptNote(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux terminals")
	}
	m := NewSessionManager(TerminalDeps{})
	s, out, _ := m.Start("test", "sleep 20", "", false, 250, nil)
	defer m.Close(s.ID)
	if strings.Contains(out, "waiting for terminal input") {
		t.Fatalf("unexpected prompt note: %q", out)
	}
}

func TestPromptNoteSwitchOff(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux terminals")
	}
	t.Setenv(PromptDetectSwitch, "off")
	master, slave, err := pty.Open()
	if err != nil {
		t.Skip("pty unavailable:", err)
	}
	defer master.Close()
	defer slave.Close()
	m := NewSessionManager(TerminalDeps{})
	s, out, _ := m.Start("test", fmt.Sprintf("exec 3<%s; sleep 20", slave.Name()), "", false, 250, nil)
	defer m.Close(s.ID)
	if strings.Contains(out, "waiting for terminal input") {
		t.Fatalf("note with switch off: %q", out)
	}
}

func TestStopHelperSessionsEndsHelpersKeepsServices(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux process groups")
	}
	m := NewSessionManager(TerminalDeps{})
	helperDir := t.TempDir()
	writeScript(t, helperDir, "make", "sleep 60")
	helper, _, _ := m.Start("test", filepath.Join(helperDir, "make"), "", false, 250, nil)
	service, _, _ := m.StartDetached("test", "sleep 60", "", nil)
	defer m.Close(helper.ID)
	defer m.Close(service.ID)
	defer os.Remove(service.LogPath)
	if !service.Detached || service.Pid <= 0 {
		t.Fatalf("service not detached: %+v", service)
	}
	stopped := m.StopHelperSessions()
	if len(stopped) != 1 || stopped[0] != helper.ID {
		t.Fatalf("stopped = %v, want only %s", stopped, helper.ID)
	}
	if groupAlive(helper.rootPID()) {
		t.Fatal("helper session still running after run end")
	}
	if !groupAlive(service.Pid) {
		t.Fatal("detached service stopped at run end")
	}
}

func TestStopHelperSwitchOffKeepsHelpers(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux process groups")
	}
	t.Setenv(StopHelperEnvSwitch, "0")
	m := NewSessionManager(TerminalDeps{})
	helper, _, _ := m.Start("test", "sleep 60", "", false, 250, nil)
	defer m.Close(helper.ID)
	if stopped := m.StopHelperSessions(); len(stopped) != 0 {
		t.Fatalf("switch off stopped %v", stopped)
	}
	if !groupAlive(helper.rootPID()) {
		t.Fatal("helper ended with the switch off")
	}
}

func TestActiveAndServiceSessionsReportProcesses(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux process table")
	}
	dir := t.TempDir()
	writeScript(t, dir, "apt-get", "sleep 20")
	writeScript(t, dir, "cc1", "sleep 20")
	m := NewSessionManager(TerminalDeps{})
	plain, _, _ := m.Start("test", "sleep 20", "", false, 250, nil)
	pkg, _, _ := m.Start("test", filepath.Join(dir, "apt-get"), dir, false, 250, nil)
	build, _, _ := m.Start("test", filepath.Join(dir, "cc1"), dir, false, 250, nil)
	service, _, _ := m.StartDetached("test", "sleep 20", "", nil)
	defer m.Close(plain.ID)
	defer m.Close(pkg.ID)
	defer m.Close(build.ID)
	defer m.Close(service.ID)
	defer os.Remove(service.LogPath)

	byID := map[SessionID]SessionInfo{}
	for _, info := range m.ActiveSessions() {
		byID[info.ID] = info
	}
	if len(byID) != 4 {
		t.Fatalf("ActiveSessions returned %d entries, want 4", len(byID))
	}
	if !byID[pkg.ID].PackageManager || byID[pkg.ID].Build {
		t.Fatalf("apt-get flags: %+v", byID[pkg.ID])
	}
	if !byID[build.ID].Build || byID[build.ID].PackageManager {
		t.Fatalf("cc1 flags: %+v", byID[build.ID])
	}
	if byID[plain.ID].PackageManager || byID[plain.ID].Build {
		t.Fatalf("plain sleep flagged: %+v", byID[plain.ID])
	}
	if !byID[plain.ID].Alive || byID[plain.ID].PID <= 0 || byID[plain.ID].Started.IsZero() || byID[plain.ID].Command != "sleep 20" {
		t.Fatalf("report fields: %+v", byID[plain.ID])
	}
	services := m.ServiceSessions()
	if len(services) != 1 || services[0].ID != service.ID || !services[0].Detached || services[0].LogPath == "" {
		t.Fatalf("ServiceSessions = %+v", services)
	}
	if err := m.Close(pkg.ID); err != nil {
		t.Fatal(err)
	}
	for _, info := range m.ActiveSessions() {
		if info.ID == pkg.ID && info.Alive {
			t.Fatalf("closed session still alive: %+v", info)
		}
	}
}

func writeScript(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestProbePortFindsListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("listen unavailable:", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if !ProbePort(port) {
		t.Fatalf("listening port %d not found", port)
	}
	_ = ln.Close()
	if ProbePort(port) {
		t.Fatalf("closed port %d still reported open", port)
	}
	if ProbePort(0) || ProbePort(70000) {
		t.Fatal("invalid port accepted")
	}
}

func TestClassifyProcessNames(t *testing.T) {
	cases := []struct {
		p          procInfo
		pkg, build bool
	}{
		{procInfo{Comm: "apt-get", Argv: "apt-get install -y x"}, true, false},
		{procInfo{Comm: "dpkg", Argv: "/usr/bin/dpkg --configure -a"}, true, false},
		{procInfo{Comm: "python3", Argv: "python3 -m pip install x"}, true, false},
		{procInfo{Comm: "python3", Argv: "python3 script.py"}, false, false},
		{procInfo{Comm: "make", Argv: "make -j4"}, false, true},
		{procInfo{Comm: "cc1", Argv: "/usr/lib/gcc/cc1 -quiet"}, false, true},
		{procInfo{Comm: "go", Argv: "/usr/local/go/bin/go build ./..."}, false, true},
		{procInfo{Comm: "go", Argv: "/usr/local/go/bin/go test ./..."}, false, false},
		{procInfo{Comm: "cargo", Argv: "cargo build --release"}, true, true},
		{procInfo{Comm: "sleep", Argv: "sleep 5"}, false, false},
	}
	for _, c := range cases {
		pkg, build := classifyProcess(c.p)
		if pkg != c.pkg || build != c.build {
			t.Errorf("%q: pkg=%v build=%v, want %v %v", c.p.Argv, pkg, build, c.pkg, c.build)
		}
	}
}

func TestProcessTreeFollowsGroupsAndParents(t *testing.T) {
	table := processTable{
		{PID: 10, PPID: 1, PGID: 10, Comm: "sh"},
		{PID: 11, PPID: 10, PGID: 10, Comm: "sleep"},
		{PID: 12, PPID: 1, PGID: 12, Comm: "orphan"}, // reparented, own group
		{PID: 13, PPID: 12, PGID: 12, Comm: "child"}, // descendant of an outsider
		{PID: 20, PPID: 1, PGID: 20, Comm: "other"},
		{PID: 21, PPID: 20, PGID: 20, Comm: "other-child"},
		{PID: 30, PPID: 11, PGID: 99, Comm: "escaped"}, // descendant of a member
	}
	got := map[int]bool{}
	for _, p := range table.tree(10) {
		got[p.PID] = true
	}
	for _, pid := range []int{10, 11, 30} {
		if !got[pid] {
			t.Errorf("tree(10) misses %d: %v", pid, got)
		}
	}
	for _, pid := range []int{12, 13, 20, 21} {
		if got[pid] {
			t.Errorf("tree(10) includes unrelated %d", pid)
		}
	}
	anc := table.ancestors(30)
	if len(anc) != 2 || anc[0].PID != 11 || anc[1].PID != 10 {
		t.Fatalf("ancestors(30) = %+v", anc)
	}
}
