package robotlab

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsVenvPython(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "Scripts", "python.exe")
	if got := venvPython(dir); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	t.Setenv("EXCALIBUR_ROBOTLAB_PYTHON", "")
	if got := NewSidecar(dir).python; got != venvPython(filepath.Join(dir, "robotlab", "venv")) {
		t.Fatal(got)
	}
}

func TestWindowsInterpreterSelection(t *testing.T) {
	root := t.TempDir()
	stub := filepath.Join(root, "Microsoft", "WindowsApps")
	real := filepath.Join(root, "Python")
	for _, dir := range []string{stub, real} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	touch := func(dir, name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	touch(stub, "python.exe")
	touch(real, "python.exe")
	t.Setenv("PATH", stub+string(os.PathListSeparator)+real)
	path, args, err := bootstrapPython()
	if err != nil || path != filepath.Join(real, "python.exe") || len(args) != 0 {
		t.Fatalf("%q %v %v", path, args, err)
	}
	touch(real, "py.exe")
	path, args, err = bootstrapPython()
	if err != nil || path != filepath.Join(real, "py.exe") || strings.Join(args, " ") != "-3" {
		t.Fatalf("%q %v %v", path, args, err)
	}
	t.Setenv("PATH", stub)
	if _, _, err := bootstrapPython(); err == nil {
		t.Fatal("Store alias selected")
	}
}

func TestWindowsRuntimeEnvironment(t *testing.T) {
	t.Setenv("Path", os.Getenv("PATH"))
	t.Setenv("SystemRoot", os.Getenv("SystemRoot"))
	t.Setenv("ComSpec", os.Getenv("ComSpec"))
	t.Setenv("MUJOCO_GL", "osmesa")
	t.Setenv("OPENAI_API_KEY", "private")
	env := strings.Join(childEnvironment(false), "\n")
	for _, key := range []string{"path=", "systemroot=", "comspec="} {
		if !strings.Contains(strings.ToLower(env), key) {
			t.Fatal("missing " + key)
		}
	}
	if strings.Contains(env, "MUJOCO_GL=") || strings.Contains(env, "OPENAI_API_KEY=") {
		t.Fatal("unexpected environment inheritance")
	}
	if !detectOSMesa() {
		t.Fatal("Windows must not require OSMesa")
	}
}

func TestWindowsPlatformEnvAllowed(t *testing.T) {
	for _, key := range []string{"Path", "SystemRoot", "SystemDrive", "WINDIR", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "TEMP", "TMP", "PATHEXT", "ComSpec", "NUMBER_OF_PROCESSORS", "PROCESSOR_ARCHITECTURE"} {
		for _, name := range []string{key, strings.ToLower(key), strings.ToUpper(key)} {
			if !platformEnvAllowed(name) {
				t.Errorf("rejected platform variable %q", name)
			}
		}
	}
	for _, key := range []string{"OPENAI_API_KEY", "PYTHONPATH", "PYTHONHOME", "PATH_EXTRA", "", "EXCALIBUR_SECRET"} {
		if platformEnvAllowed(key) {
			t.Errorf("allowed unrelated variable %q", key)
		}
	}
}

func TestWindowsJobHelper(t *testing.T) {
	switch os.Getenv("EXCALIBUR_ROBOTLAB_JOB_HELPER") {
	case "child":
		time.Sleep(time.Minute)
		os.Exit(0)
	case "parent":
		cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsJobHelper$")
		cmd.Env = append(os.Environ(), "EXCALIBUR_ROBOTLAB_JOB_HELPER=child")
		if err := cmd.Start(); err != nil {
			os.Exit(2)
		}
		fmt.Println(cmd.Process.Pid)
		time.Sleep(time.Minute)
		os.Exit(0)
	}
}

func TestWindowsSidecarJobKillsDescendants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsJobHelper$")
	cmd.Env = append(os.Environ(), "EXCALIBUR_ROBOTLAB_JOB_HELPER=parent")
	group := sidecarProcessGroup()
	group.Configure(cmd)
	cmd.Cancel = func() error { return group.Kill(cmd.Process.Pid) }
	defer cleanupSidecarGroup(group)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = startSidecarCommand(cmd, group); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = group.Kill(cmd.Process.Pid); _ = cmd.Wait() }()
	if err = lowerSidecarPriority(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(out)
	if !scanner.Scan() {
		t.Fatal("child PID missing")
	}
	var pid uint32
	if _, err = fmt.Sscan(scanner.Text(), &pid); err != nil {
		t.Fatal(err)
	}
	child, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(child)
	if err = group.Kill(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if result, err := windows.WaitForSingleObject(child, 5000); err != nil || result != windows.WAIT_OBJECT_0 {
		t.Fatalf("descendant remains: %d %v", result, err)
	}
}

func TestWindowsProtocolHelper(t *testing.T) {
	if os.Getenv("EXCALIBUR_ROBOTLAB_WINDOWS_PROTOCOL") != "1" {
		return
	}
	if os.Getenv("MUJOCO_GL") != "" || os.Getenv("SystemRoot") == "" {
		os.Exit(2)
	}
	output := json.NewEncoder(os.Stdout)
	_ = output.Encode(map[string]any{"event": "ready", "data": map[string]any{"version": "windows-test"}})
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID uint64 `json:"id"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(3)
		}
		_ = output.Encode(map[string]any{"id": request.ID, "result": map[string]any{"ok": true}})
	}
	os.Exit(0)
}

func TestWindowsSidecarProtocolAndClose(t *testing.T) {
	t.Setenv("EXCALIBUR_ROBOTLAB", "1")
	t.Setenv("EXCALIBUR_ROBOTLAB_WINDOWS_PROTOCOL", "1")
	sidecar := NewSidecar(t.TempDir())
	sidecar.Command = func(_, _, _ string) *exec.Cmd {
		return exec.Command(os.Args[0], "-test.run=^TestWindowsProtocolHelper$")
	}
	defer sidecar.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reply, err := sidecar.Call(ctx, "ping", nil)
	if err != nil || !strings.Contains(string(reply), `"ok":true`) {
		t.Fatalf("%s %v", reply, err)
	}
	status := sidecar.Status(ctx)
	if !status.Running || status.Version != "windows-test" {
		t.Fatalf("%+v", status)
	}
	if err := sidecar.Close(); err != nil {
		t.Fatal(err)
	}
	if sidecar.Status(ctx).Running {
		t.Fatal("sidecar still running after close")
	}
}
