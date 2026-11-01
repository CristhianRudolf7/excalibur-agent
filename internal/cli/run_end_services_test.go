package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	providerfake "github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// freePort returns an unused localhost TCP port for the survival test.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestRunEndBackgroundSurvives is the W10 end-to-end proof: a background
// HTTP server started during the run still answers after `excalibur
// exec` returns, and the trial records list the surviving process.
// Gates are disabled here to isolate the survival mechanism (the
// artifact/self-verify discipline is covered by its own suites); the
// prompt test below covers the keep_running teaching.
func TestRunEndBackgroundSurvives(t *testing.T) {
	t.Setenv(tools.DetachBackgroundSwitch, "on")
	t.Setenv("EXCALIBUR_ARTIFACT_GATE", "0")
	t.Setenv("EXCALIBUR_SELFVERIFY", "0")
	port := freePort(t)
	workdir := t.TempDir()
	serveArgs, _ := json.Marshal(map[string]any{
		"command":    fmt.Sprintf("python3 -m http.server %d --bind 127.0.0.1", port),
		"background": true, "workdir": workdir,
	})
	verifyArgs, _ := json.Marshal(map[string]string{
		"command": fmt.Sprintf("sleep 1; curl -fsS http://127.0.0.1:%d/ | head -20", port),
	})
	evidence := fmt.Sprintf("started detached server `python3 -m http.server %d` (background:true, keep_running on port %d); verified `curl -fsS http://127.0.0.1:%d/` shows directory listing; server left running", port, port, port)
	fin := finishScript("serving on port "+fmt.Sprint(port), evidence)
	fake := providerfake.New("run-end-services")
	fake.Responses = []providers.ChatResponse{
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c1", Name: "exec", Arguments: serveArgs}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c2", Name: "exec", Arguments: verifyArgs}}}},
	}
	fake.Responses = append(fake.Responses, fin...)
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request:    fmt.Sprintf("Serve %s on port %d with a python http server, keep it running", workdir, port),
		LogsDir:    logs,
		Profile:    ProfileEval,
		ProjectDir: workdir,
		Provider:   fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q (%s), want completed", out.State, out.Summary)
	}
	// Trial records must list the surviving detached process.
	svcPath := filepath.Join(out.Dir, "services.json")
	raw, err := os.ReadFile(svcPath)
	if err != nil {
		t.Fatalf("services.json missing: %v", err)
	}
	var rec struct {
		Count     int `json:"count"`
		Surviving []struct {
			Pid     int    `json:"pid"`
			Command string `json:"command"`
			LogPath string `json:"log_path"`
		} `json:"surviving"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Count == 0 || len(rec.Surviving) == 0 {
		t.Fatalf("services.json must record a surviving process, got %s", string(raw))
	}
	pid := rec.Surviving[0].Pid
	if pid <= 0 {
		t.Fatalf("surviving pid must be positive, got %+v", rec.Surviving[0])
	}
	// The server must still answer after the run returned.
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				lastErr = nil
				break
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(200 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("background server on %d must answer after exec returns: %v", port, lastErr)
	}
	// Cleanup the detached survivor (the point is it survived; the
	// test must not leak it).
	if proc, err := os.FindProcess(pid); err == nil {
		_ = proc.Signal(syscall.SIGTERM)
		time.Sleep(300 * time.Millisecond)
		_ = proc.Signal(syscall.SIGKILL)
	}
}

// TestRunEndFinishDescription is covered in tools, but the eval prompt
// must also teach the keep_running discipline and the no-kill rule.
func TestRunEndPromptTeachesSurvival(t *testing.T) {
	t.Setenv(tools.DetachBackgroundSwitch, "on")
	fake := providerfake.New("run-end-prompt")
	fake.Responses = finishScript("did it", "exec true ok")
	workdir := t.TempDir()
	logs := t.TempDir()
	_, err := RunExec(context.Background(), ExecOptions{
		Request: "noop", LogsDir: logs, Profile: ProfileEval,
		ProjectDir: workdir, Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.ChatRequests) == 0 {
		t.Fatal("expected model calls")
	}
	// The system prompt is the first request's messages; search all.
	foundKeep, foundKill := false, false
	for _, req := range fake.ChatRequests {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "keep_running") || strings.Contains(m.Content, "left running") {
				foundKeep = true
			}
			if strings.Contains(strings.ToLower(m.Content), "killing") {
				foundKill = true
			}
		}
	}
	if !foundKeep {
		t.Fatal("prompt must teach keep_running for required services")
	}
	if !foundKill {
		t.Fatal("prompt must teach that killing a required process is a failure")
	}
}
