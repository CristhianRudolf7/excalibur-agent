package automations

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"golang.org/x/sys/windows"
)

func TestWindowsScriptInterpreters(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)", "LOCALAPPDATA", "PATH"} {
		t.Setenv(key, root)
	}
	if _, _, err := scriptInvocation("bash"); err == nil || !strings.Contains(err.Error(), "install Git for Windows") {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "Git", "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bash.exe", "python.exe", "py.exe"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("fake"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path, args, err := scriptInvocation("bash")
	if err != nil || path != filepath.Join(bin, "bash.exe") || len(args) != 0 {
		t.Fatalf("%q %v %v", path, args, err)
	}
	t.Setenv("PATH", bin)
	path, args, err = scriptInvocation("python3")
	if err != nil || path != filepath.Join(bin, "py.exe") || strings.Join(args, " ") != "-3" {
		t.Fatalf("%q %v %v", path, args, err)
	}
	if err := os.Remove(filepath.Join(bin, "py.exe")); err != nil {
		t.Fatal(err)
	}
	path, args, err = scriptInvocation("python3")
	if err != nil || path != filepath.Join(bin, "python.exe") || len(args) != 0 {
		t.Fatalf("%q %v %v", path, args, err)
	}
	stub := filepath.Join(root, "Microsoft", "WindowsApps")
	if err := os.MkdirAll(stub, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stub, "python.exe"), []byte("alias"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stub+string(os.PathListSeparator)+bin)
	path, _, err = scriptInvocation("python3")
	if err != nil || path != filepath.Join(bin, "python.exe") {
		t.Fatalf("%q %v", path, err)
	}
	t.Setenv("PATH", stub)
	if _, _, err := scriptInvocation("python3"); err == nil {
		t.Fatal("Store alias selected")
	}
}

func TestWindowsGitBashFromRelativePath(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		t.Setenv(key, "")
	}
	for _, sub := range []string{"bin", "cmd"} {
		if err := os.MkdirAll(filepath.Join(root, "Git", sub), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{"bin/bash.exe", "cmd/git.exe"} {
		if err := os.WriteFile(filepath.Join(root, "Git", file), []byte("fake"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	t.Setenv("PATH", filepath.Join("Git", "cmd"))
	path, err := windowsBash()
	if err != nil || path != filepath.Join(root, "Git", "bin", "bash.exe") {
		t.Fatalf("%q %v", path, err)
	}
}

func TestWindowsScriptHelper(t *testing.T) {
	switch os.Getenv("EXCALIBUR_AUTOMATION_HELPER") {
	case "child":
		time.Sleep(time.Minute)
		os.Exit(0)
	case "parent", "parent-exit":
		cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsScriptHelper$")
		cmd.Env = append(os.Environ(), "EXCALIBUR_AUTOMATION_HELPER=child")
		if err := cmd.Start(); err != nil {
			os.Exit(2)
		}
		fmt.Println(cmd.Process.Pid)
		if os.Getenv("EXCALIBUR_AUTOMATION_HELPER") == "parent-exit" {
			os.Exit(0)
		}
		time.Sleep(time.Minute)
		os.Exit(0)
	}
}

func TestWindowsScriptCancellationKillsDescendants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsScriptHelper$")
	cmd.Env = append(os.Environ(), "EXCALIBUR_AUTOMATION_HELPER=parent")
	if err := configureScriptProcess(cmd); err != nil {
		t.Fatal(err)
	}
	defer cleanupScriptProcess(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	safego.Go("automation-windows-test", nil, func() { done <- runScriptProcess(cmd) })
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(12 * time.Second):
			t.Error("script did not stop")
		}
	}()
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
	cancel()
	if state, err := windows.WaitForSingleObject(child, 5000); err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("child remains alive: %d %v", state, err)
	}
}

func TestWindowsScriptCleanupKillsBackgroundChildren(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsScriptHelper$")
	cmd.Env = append(os.Environ(), "EXCALIBUR_AUTOMATION_HELPER=parent-exit")
	var output cappedBuffer
	cmd.Stdout = &output
	if err := configureScriptProcess(cmd); err != nil {
		t.Fatal(err)
	}
	defer cleanupScriptProcess(cmd)
	if err := runScriptProcess(cmd); err != nil {
		t.Fatal(err)
	}
	var pid uint32
	if _, err := fmt.Sscan(string(output.data), &pid); err != nil {
		t.Fatal(err)
	}
	child, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(child)
	cleanupScriptProcess(cmd)
	if state, err := windows.WaitForSingleObject(child, 5000); err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("background child remains: %d %v", state, err)
	}
}
