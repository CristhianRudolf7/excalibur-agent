package tools

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	platformfake "github.com/CristhianRudolf7/excalibur-agent/internal/platform/fake"
)

func TestSignalSelectors(t *testing.T) {
	protected := []platform.SignalTarget{{PID: 1234, Group: 1200, Name: "excalibur", Command: "excalibur exec ssh -p 2222 root@localhost"}, {PID: 1233, Group: 1100, Name: "parent", Command: "parent launcher"}, {PID: 1235, Group: 1200, Name: "excalibur-agent", Command: "excalibur-agent"}}
	for _, tc := range []struct {
		name   string
		args   []string
		refuse bool
	}{
		{"pkill", []string{"-f", "ssh -p 2222 root@localhost"}, true},
		{"pkill", []string{"-fx", "excalibur exec ssh -p 2222 root@localhost"}, true},
		{"pkill", []string{"-x", "excalibur"}, true},
		{"pkill", []string{"-f", "parent launcher"}, true},
		{"pkill", []string{"-f", "unrelated-worker"}, false},
		{"killall", []string{"excalibur"}, true},
		{"killall", []string{"unrelated-worker"}, false},
		{"kill", []string{"-9", "1234"}, true},
		{"kill", []string{"--", "-1200"}, true},
		{"kill", []string{"-TERM", "--", "-1100"}, true},
		{"kill", []string{"0"}, true},
		{"kill", []string{"--", "-1"}, true},
		{"kill", []string{"-s", "TERM", "1233"}, true},
		{"kill", []string{"-9", "9999"}, false},
	} {
		if err := checkSignalArgs(tc.name, tc.args, protected); (err != nil) != tc.refuse {
			t.Errorf("%s %v: %v", tc.name, tc.args, err)
		}
	}
}

func TestGuardSignals(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux process inspection")
	}
	for _, cmd := range []string{fmt.Sprintf("kill -9 %d", os.Getpid()), fmt.Sprintf("/bin/kill -TERM %d", os.Getppid()), fmt.Sprintf(`ki\ll -9 %d`, os.Getpid()), "pkill -f '.*'", "killall -9 excalibur", "kill -9 $PPID", `sh -c 'kill -9 $PPID'`, `"kill" -9 $PPID`, "printf ok; command kill -9 $PPID"} {
		// The test binary is not named excalibur.
		if cmd == "killall -9 excalibur" {
			continue
		}
		if err := guardSignals(cmd); err == nil {
			t.Errorf("allowed %s", cmd)
		}
	}
	for _, cmd := range []string{"printf 'kill -9 123'", "echo pkill", "true", "pkill -x w17-unrelated-worker"} {
		if err := guardSignals(cmd); err != nil {
			t.Errorf("%s: %v", cmd, err)
		}
	}
	t.Setenv("EXCALIBUR_SELF_KILL_GUARD", "0")
	if err := guardSignals("kill -9 $PPID"); err != nil {
		t.Fatal(err)
	}
}

func TestSessionRefusesSelfKill(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux process inspection")
	}
	for _, mode := range []string{"pipe", "pty", "detached"} {
		m := NewSessionManager(TerminalDeps{PTY: &platformfake.PTY{}})
		command := fmt.Sprintf("kill -9 %d", os.Getpid())
		var s *Session
		var output string
		var alive bool
		if mode == "detached" {
			s, output, alive = m.StartDetached("test", command, "", nil)
		} else {
			s, output, alive = m.Start("test", command, "", mode == "pty", 250, nil)
		}
		if alive || !strings.Contains(output, "self-kill guard refused") {
			t.Fatalf("session %v: %s", s, output)
		}
	}
}

func TestKillallLongAndPathNames(t *testing.T) {
	targets := []platform.SignalTarget{{PID: 1234, Group: 1200, Name: "excalibur-agent", Command: "/tmp/excalibur-agent-final"}}
	for _, name := range []string{"excalibur-agent-final", "/tmp/excalibur-agent-final"} {
		if err := checkSignalArgs("killall", []string{name}, targets); err == nil {
			t.Fatalf("allowed %s", name)
		}
	}
}
