package automations

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// cappedBuffer consumes all bytes while retaining bounded output.
type cappedBuffer struct{ data []byte }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := 64*1024 - len(b.data)
	if remaining > 0 {
		b.data = append(b.data, p[:min(remaining, n)]...)
	}
	return n, nil
}
func (m *Manager) runScript(ctx context.Context, id string, s Step, prior map[string]string) (string, error) {
	dir, err := filepath.Abs(filepath.Join(m.cfg.WorkDir, id))
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	binary, ext := "", ""
	switch s.Language {
	case "python":
		binary, ext = "python3", ".py"
	case "node":
		binary, ext = "node", ".js"
	case "bash":
		binary, ext = "bash", ".sh"
	}
	path := filepath.Join(dir, "step-"+uuid.NewString()+ext)
	if err = os.WriteFile(path, []byte(s.Code), 0600); err != nil {
		return "", err
	}
	defer os.Remove(path)
	input, err := json.Marshal(prior)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, binary, path)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "AUTOMATION_INPUT="+string(input))
	cmd.WaitDelay = 100 * time.Millisecond
	if err = configureScriptProcess(cmd); err != nil {
		return "", err
	}
	defer cleanupScriptProcess(cmd)
	var stdout, stderr cappedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	output := truncate(string(stdout.data), 2000)
	if ctx.Err() != nil {
		return output, ctx.Err()
	}
	if err != nil {
		return output, fmt.Errorf("script failed: %w: %s", err, truncate(string(stderr.data), 2000))
	}
	return output, nil
}
