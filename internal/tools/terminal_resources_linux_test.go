//go:build linux

package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/envsnap"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	platformfake "github.com/CristhianRudolf7/excalibur-agent/internal/platform/fake"
)

func oomTestTool(t *testing.T, counter string) (*ExecTool, string) {
	t.Helper()
	tool := execTestTool(t)
	// These tests signal only the controlled shell/worker, never the harness.
	t.Setenv("EXCALIBUR_SELF_KILL_GUARD", "off")
	t.Setenv(envsnap.EffectiveLimitsSwitch, "1")
	root := t.TempDir()
	for path, value := range map[string]string{
		"proc/self/cgroup":                "0::/job\n",
		"proc/self/mountinfo":             "1 0 0:1 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n",
		"proc/meminfo":                    "MemTotal: 33554432 kB\n",
		"sys/devices/system/cpu/online":   "0-7\n",
		"sys/fs/cgroup/job/memory.max":    "4294967296\n",
		"sys/fs/cgroup/job/cpu.max":       "200000 100000\n",
		"sys/fs/cgroup/job/memory.events": counter,
	} {
		file := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	r := &envsnap.ResourceReader{ProcRoot: filepath.Join(root, "proc"), SysRoot: filepath.Join(root, "sys")}
	tool.Sessions = NewSessionManager(TerminalDeps{Resources: r, PTY: platform.LinuxPTY{}})
	t.Cleanup(func() { closeSessions(tool.Sessions) })
	return tool, filepath.Join(root, "sys/fs/cgroup/job/memory.events")
}

func TestExecOOMDiagnosis(t *testing.T) {
	for _, tc := range []struct {
		name, counter, after, finish, want string
		disabled                           bool
	}{
		{"killed shell", "oom_kill 2\n", "oom_kill 3\n", "kill -KILL $$", "killed by the out-of-memory killer (memory limit 4GiB); reduce parallelism or memory use; retry parallelism: 1", false},
		{"shell status", "oom_kill 2\n", "oom_kill 3\n", "exit 137", "killed by the out-of-memory killer", false},
		{"child kill driver failure", "oom_kill 2\n", "oom_kill 3\n", "sh -c 'kill -KILL $$'; exit 2", "exit: 2 (killed by the out-of-memory killer", false},
		{"child kill driver success", "oom_kill 2\n", "oom_kill 3\n", "sh -c 'kill -KILL $$'; true", "exit: 0 (killed by the out-of-memory killer", false},
		{"unavailable", "", "", "exit 137", "exit: 137 (SIGKILL; possibly out of memory)", false},
		{"counter unchanged", "oom_kill 2\n", "oom_kill 2\n", "kill -KILL $$", "exit: 137 (SIGKILL)", false},
		{"counter reset", "oom_kill 2\n", "oom_kill 0\n", "exit 137", "exit: 137 (SIGKILL)", false},
		{"switch off", "oom_kill 2\n", "oom_kill 3\n", "exit 137", "exit: 137 (SIGKILL)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool, counter := oomTestTool(t, tc.counter)
			if tc.disabled {
				t.Setenv(envsnap.EffectiveLimitsSwitch, "off")
			}
			command := fmt.Sprintf("printf '%%s' %s > %s; %s", shellQuote(tc.after), shellQuote(counter), tc.finish)
			res := callTool(t, tool, map[string]any{"command": command, "workdir": t.TempDir()})
			header := strings.SplitN(res.Output, "\n", 2)[0]
			if !strings.Contains(header, tc.want) {
				t.Fatalf("header %q, want %q", header, tc.want)
			}
			if !strings.Contains(tc.want, "killed by") && strings.Contains(header, "killed by") {
				t.Fatalf("false OOM claim: %q", header)
			}
		})
	}
}

func TestBackgroundOOMResultIsFrozen(t *testing.T) {
	for _, detached := range []bool{false, true} {
		for _, evidence := range []bool{false, true} {
			t.Run(fmt.Sprintf("detached=%v/evidence=%v", detached, evidence), func(t *testing.T) {
				tool, counter := oomTestTool(t, "oom_kill 2\n")
				t.Setenv("EXCALIBUR_BUILD_EVIDENCE", fmt.Sprint(evidence))
				release := filepath.Join(t.TempDir(), "release")
				cmd := fmt.Sprintf("while [ ! -f %s ]; do sleep .01; done; printf 'oom_kill 3\\n' > %s; kill -KILL $$", shellQuote(release), shellQuote(counter))
				var s *Session
				var alive bool
				if detached {
					s, _, alive = tool.Sessions.StartDetached("test", cmd, t.TempDir(), nil, 250)
				} else {
					s, _, alive = tool.Sessions.Start("test", cmd, t.TempDir(), false, 250, nil)
				}
				if !alive {
					t.Fatal("background session must start alive")
				}
				if err := os.WriteFile(release, nil, 0644); err != nil {
					t.Fatal(err)
				}
				select {
				case <-s.done:
				case <-time.After(3 * time.Second):
					t.Fatal("background session did not finish")
				}
				// A later reset must not erase evidence captured on completion.
				if err := os.WriteFile(counter, []byte("oom_kill 0\n"), 0644); err != nil {
					t.Fatal(err)
				}
				io := &ExecIOTool{Sessions: tool.Sessions}
				for _, action := range []string{"read", "wait_for"} {
					res := callTool(t, io, map[string]any{"action": action, "session": string(s.ID), "timeout_s": .05})
					if !strings.Contains(strings.SplitN(res.Output, "\n", 2)[0], "killed by the out-of-memory killer (memory limit 4GiB)") {
						t.Fatalf("%s: %s", action, res.Output)
					}
				}
				listed := callTool(t, io, map[string]any{"action": "list"})
				if !strings.Contains(listed.Output, "  exit: 137 (killed by the out-of-memory killer") {
					t.Fatalf("listing: %s", listed.Output)
				}
			})
		}
	}
}

func TestPTYOOMResult(t *testing.T) {
	for _, autonomous := range []bool{false, true} {
		t.Run(fmt.Sprint(autonomous), func(t *testing.T) {
			tool, counter := oomTestTool(t, "oom_kill 2\n")
			if !autonomous {
				tool.Sessions.deps.PTY = resourceTestPTY{}
			}
			restore := SetExecScope(ExecScope{Active: autonomous})
			defer restore()
			command := fmt.Sprintf("printf 'oom_kill 3\\n' > %s; exit 137", shellQuote(counter))
			res := callTool(t, tool, map[string]any{"command": command, "workdir": t.TempDir(), "tty": true})
			if !strings.Contains(strings.SplitN(res.Output, "\n", 2)[0], "killed by the out-of-memory killer (memory limit 4GiB)") {
				t.Fatalf("PTY result: %s", res.Output)
			}
		})
	}
}

// The scripted PTY runs the small controlled command and returns its real exit
// through the shared platform fake, without depending on interactive devices.
type resourceTestPTY struct{}

func (resourceTestPTY) Start(cmd *exec.Cmd, _ platform.Winsize) (platform.PTYSession, error) {
	cmd.SysProcAttr = nil
	err := cmd.Run()
	return &platformfake.PTYSession{Code: err}, nil
}
