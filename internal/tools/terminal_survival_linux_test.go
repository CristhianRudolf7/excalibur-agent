//go:build linux

package tools

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

func TestOpenSessionsPreservedAndLegacySwitch(t *testing.T) {
	for _, enabled := range []string{"1", "0"} {
		t.Run(enabled, func(t *testing.T) {
			t.Setenv("EXCALIBUR_SURVIVE_SESSIONS", enabled)
			restore := SetExecScope(ExecScope{Active: true})
			defer restore()
			m := NewSessionManager(TerminalDeps{})
			s, _, _ := m.Start("test", "sleep 20", "", false, 250, nil)
			defer m.Close(s.ID)
			if s.LogPath != "" {
				defer os.Remove(s.LogPath)
			}
			stopped := m.StopHelperSessions()
			if enabled == "1" && (len(stopped) != 0 || !groupAlive(s.rootPID()) || s.LogPath == "") {
				t.Fatalf("open session lost: %v log=%q", stopped, s.LogPath)
			}
			if enabled == "0" && len(stopped) != 1 {
				t.Fatalf("legacy helper not stopped: %v", stopped)
			}
		})
	}
}

// The same test binary acts as an HTTP service and as the harness launcher.
// This verifies both parent-death handling and output written after that death.
func TestForegroundServiceSurvivesHarnessExit(t *testing.T) {
	switch os.Getenv("EXCALIBUR_SURVIVAL_TEST_ROLE") {
	case "server":
		listener, err := net.Listen("tcp", os.Getenv("EXCALIBUR_SURVIVAL_TEST_ADDR"))
		if err != nil {
			os.Exit(2)
		}
		safego.Go("survival-test-timeout", nil, func() { time.Sleep(15 * time.Second); os.Exit(0) })
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("served after harness exit")
			fmt.Fprint(w, "ready")
		})
		fmt.Println("listening")
		_ = http.Serve(listener, handler)
		os.Exit(0)
	case "launcher":
		restore := SetExecScope(ExecScope{Active: true})
		defer restore()
		m := NewSessionManager(TerminalDeps{Group: platform.LinuxProcessGroup{}})
		command := strconv.Quote(os.Args[0]) + " -test.run=^TestForegroundServiceSurvivesHarnessExit$"
		// Keep a package/build wrapper alive around the service: the listener
		// belongs to a descendant and must override finite-helper cleanup.
		dir := os.Getenv("EXCALIBUR_SURVIVAL_TEST_DIR")
		command = strconv.Quote(filepath.Join(dir, "make")) + " " + command
		s, _, alive := m.Start("launcher", command, "", false, 250, []string{"EXCALIBUR_SURVIVAL_TEST_ROLE=server"})
		if !alive {
			os.Exit(3)
		}
		until := time.Now().Add(3 * time.Second)
		for !processTreeListeningTCP(s.rootPID()) && time.Now().Before(until) {
			time.Sleep(10 * time.Millisecond)
		}
		if !processTreeListeningTCP(s.rootPID()) || len(m.StopHelperSessions()) != 0 {
			os.Exit(4)
		}
		raw, _ := json.Marshal(m.Surviving())
		if err := os.WriteFile(filepath.Join(dir, "sessions.json"), raw, 0600); err != nil {
			os.Exit(5)
		}
		os.Exit(0)
	}
	t.Setenv("EXCALIBUR_SURVIVE_SESSIONS", "1")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	dir := t.TempDir()
	writeScript(t, dir, "make", `"$@" & wait`)
	cmd := exec.Command(os.Args[0], "-test.run=^TestForegroundServiceSurvivesHarnessExit$")
	cmd.Env = append(os.Environ(), "EXCALIBUR_SURVIVAL_TEST_ROLE=launcher", "EXCALIBUR_SURVIVAL_TEST_ADDR="+addr, "EXCALIBUR_SURVIVAL_TEST_DIR="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("launcher: %v %s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sessions []SurvivingProcess
	if err := json.Unmarshal(raw, &sessions); err != nil || len(sessions) != 1 {
		t.Fatalf("sessions: %s %v", raw, err)
	}
	defer syscall.Kill(-sessions[0].Pid, syscall.SIGKILL)
	defer os.Remove(sessions[0].LogPath)
	client := &http.Client{Timeout: time.Second}
	for i := 0; i < 2; i++ {
		response, err := client.Get("http://" + addr)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
	}
	log, err := os.ReadFile(sessions[0].LogPath)
	if err != nil || strings.Count(string(log), "served after harness exit") != 2 {
		t.Fatalf("output after harness exit: %s %v", log, err)
	}
}
