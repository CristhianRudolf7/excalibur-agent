// smoke runs the actual installer against locally built release assets.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: smoke ARTIFACT_DIRECTORY")
	}
	root, err := os.MkdirTemp("", "excalibur-release-smoke-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	server := httptest.NewServer(http.FileServer(http.Dir(os.Args[1])))
	defer server.Close()
	env := append(os.Environ(), "EXCALIBUR_BASE_URL="+server.URL, "EXCALIBUR_SKIP_BROWSER_INSTALL=1", "EXCALIBUR_PROVIDER=stub", "EXCALIBUR_SERVICE_NAME="+filepath.Base(root))
	binary := filepath.Join(root, ".local", "bin", "excalibur")
	if runtime.GOOS == "windows" {
		systemRoot := os.Getenv("SystemRoot")
		if systemRoot == "" {
			return fmt.Errorf("the Windows SystemRoot is required for the release smoke")
		}
		// Test the CLI without MinGW, Go or Node DLL directories on PATH.
		env = append(env, "PATH="+strings.Join([]string{filepath.Join(systemRoot, "System32"), systemRoot, filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0")}, string(os.PathListSeparator)))
		binary = filepath.Join(os.Getenv("LOCALAPPDATA"), "Excalibur", "bin", "excalibur.exe")
	} else {
		for _, key := range []string{"HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR"} {
			value := root
			if key != "HOME" {
				value = filepath.Join(root, key)
			}
			env = append(env, key+"="+value)
		}
	}
	command := exec.Command("sh", "scripts/install/install.sh")
	if runtime.GOOS == "windows" {
		command = exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "scripts/install/install.ps1")
	}
	command.Env = env
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, "uninstall")
		cmd.Env = env
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	}()
	if err := command.Run(); err != nil {
		return err
	}
	expectedVersion, err := os.ReadFile(filepath.Join(os.Args[1], "version.txt"))
	if err != nil {
		return err
	}
	versionCommand := exec.Command(binary, "--version")
	versionCommand.Env = env
	actualVersion, err := versionCommand.Output()
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(actualVersion)) != "excalibur "+strings.TrimSpace(string(expectedVersion)) {
		return fmt.Errorf("installed binary version does not match release version.txt")
	}
	for _, path := range []string{"/", "/settings?section=connections", "/healthz"} {
		response, err := http.Get("http://127.0.0.1:7787" + path)
		if err != nil {
			return err
		}
		raw, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			return err
		}
		if response.StatusCode != 200 {
			return fmt.Errorf("%s: HTTP %d", path, response.StatusCode)
		}
		if path != "/healthz" && (!strings.Contains(string(raw), "<html") || strings.Contains(string(raw), "development binary")) {
			return fmt.Errorf("UI is not embedded")
		}
	}
	for _, method := range []string{"principal.get", "codex.status", "conversation.send"} {
		params := map[string]any{}
		if method == "conversation.send" {
			params = map[string]any{"conversation_id": "release-smoke", "client_msg_id": "release-smoke-1", "text": "hello"}
		}
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "smoke", "method": method, "params": params})
		req, _ := http.NewRequest("POST", "http://127.0.0.1:7787/rpc", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://127.0.0.1:7787")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		raw, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			return err
		}
		var reply struct {
			Error json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(raw, &reply); err != nil {
			return err
		}
		if response.StatusCode != 200 || len(reply.Error) > 0 {
			return fmt.Errorf("%s failed: %s", method, raw)
		}
	}
	if os.Getenv("EXCALIBUR_SMOKE_BROWSER") == "1" {
		cmd := exec.Command("node", "scripts/release/browser-smoke.mjs", "http://127.0.0.1:7787")
		cmd.Env = env
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	fmt.Println("Release smoke passed: installer, embedded UI, health, same-origin RPC and chat intake.")
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
