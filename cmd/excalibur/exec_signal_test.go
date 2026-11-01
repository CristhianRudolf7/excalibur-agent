package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/cli"
)

func TestExecCommandSignal(t *testing.T) {
	if os.Getenv("EXCALIBUR_COMMAND_SIGNAL_HELPER") == "1" {
		// TestMain deliberately pins stub wiring; this subprocess uses only its
		// parent's simulated HTTP server and a synthetic credential.
		t.Setenv("EXCALIBUR_PROVIDER", "openai")
		t.Setenv("EXCALIBUR_CREDENTIALS_FILE", os.Getenv("EXCALIBUR_COMMAND_CREDS"))
		err := runExecCmd([]string{"wait for the pending request", "--profile", "eval", "--project", os.Getenv("EXCALIBUR_COMMAND_PROJECT"), "--logs-dir", os.Getenv("EXCALIBUR_COMMAND_LOGS")})
		os.Exit(cli.ExitCode(err))
	}
	for _, sig := range []os.Signal{syscall.SIGTERM, os.Interrupt} {
		t.Run(sig.String(), func(t *testing.T) {
			requested := make(chan struct{})
			var once sync.Once
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				once.Do(func() { close(requested) })
				<-r.Context().Done()
			}))
			defer srv.Close()
			logs, project := t.TempDir(), t.TempDir()
			creds := filepath.Join(t.TempDir(), "empty.json")
			if err := os.WriteFile(creds, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
			output, err := os.CreateTemp(t.TempDir(), "command-output-*")
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			cmd := exec.Command(os.Args[0], "-test.run=^TestExecCommandSignal$")
			cmd.Stdout = output
			cmd.Stderr = output
			cmd.Env = append(os.Environ(), "EXCALIBUR_COMMAND_SIGNAL_HELPER=1", "EXCALIBUR_COMMAND_PROJECT="+project, "EXCALIBUR_COMMAND_LOGS="+logs,
				"EXCALIBUR_PROVIDER=openai", "EXCALIBUR_MODEL=signal-test", "EXCALIBUR_API_KEY=simulated-test-key", "EXCALIBUR_BASE_URL="+srv.URL+"/v1", "EXCALIBUR_COMMAND_CREDS="+creds)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill() }()
			select {
			case <-requested:
			case <-time.After(15 * time.Second):
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				raw, _ := os.ReadFile(output.Name())
				t.Fatalf("model request never reached simulated server: %s", raw)
			}
			start := time.Now()
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			err = cmd.Wait()
			if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != cli.ExitPaused {
				t.Fatalf("exit: %v", err)
			}
			if time.Since(start) > 2*time.Second {
				t.Fatal("command exceeded cleanup bound")
			}
			for _, name := range []string{"final.json", "metrics.json", "trajectory.json", "excalibur/final.json", "excalibur/metrics.json", "excalibur/trajectory.json"} {
				raw, err := os.ReadFile(filepath.Join(logs, name))
				if err != nil || !json.Valid(raw) {
					t.Fatalf("missing or invalid %s: %v", name, err)
				}
			}
			raw, _ := os.ReadFile(filepath.Join(logs, "excalibur/final.json"))
			var final struct {
				State string `json:"state"`
			}
			_ = json.Unmarshal(raw, &final)
			if final.State != "stopped_by_timeout" {
				t.Fatal(string(raw))
			}
		})
	}
}
