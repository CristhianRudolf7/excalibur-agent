package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsProcessHelper(t *testing.T) {
	role := os.Getenv("EXCALIBUR_PROCESS_TEST_ROLE")
	if role == "" {
		return
	}
	dir := os.Getenv("EXCALIBUR_PROCESS_TEST_DIR")
	if role == "root" {
		for {
			if _, err := os.Stat(filepath.Join(dir, "start")); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if role != "leaf" {
		next := "branch"
		if role == "branch" {
			next = "leaf"
		}
		cmd := processTestCommand(dir, next)
		if err := cmd.Start(); err != nil {
			os.Exit(2)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, role), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		os.Exit(3)
	}
	time.Sleep(time.Minute)
	os.Exit(0)
}

func processTestCommand(dir, role string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$")
	cmd.Env = append(os.Environ(), "EXCALIBUR_PROCESS_TEST_ROLE="+role, "EXCALIBUR_PROCESS_TEST_DIR="+dir)
	return cmd
}

func TestWindowsProcessGroupConfigure(t *testing.T) {
	cmd := exec.Command("cmd.exe")
	attr := &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_UNICODE_ENVIRONMENT}
	cmd.SysProcAttr = attr
	WindowsProcessGroup{}.Configure(cmd)
	if cmd.SysProcAttr != attr || !attr.HideWindow || attr.CreationFlags != windows.CREATE_UNICODE_ENVIRONMENT|windows.CREATE_NEW_PROCESS_GROUP {
		t.Fatalf("attributes not preserved: %+v", attr)
	}
	WindowsProcessGroup{}.Configure(nil)
}

func TestWindowsProcessGroupKillTree(t *testing.T) {
	for _, job := range []bool{false, true} {
		t.Run(fmt.Sprint("job=", job), func(t *testing.T) {
			dir := t.TempDir()
			cmd := processTestCommand(dir, "root")
			WindowsProcessGroup{}.Configure(cmd)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer cmd.Wait()
			defer (WindowsProcessGroup{}).Kill(cmd.Process.Pid)
			if job {
				if err := AttachJob(cmd.Process.Pid); err != nil {
					t.Fatal(err)
				}
				if err := AttachJob(cmd.Process.Pid); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "start"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			for _, role := range []string{"root", "branch", "leaf"} {
				deadline := time.Now().Add(10 * time.Second)
				var raw []byte
				for time.Now().Before(deadline) {
					raw, _ = os.ReadFile(filepath.Join(dir, role))
					if len(raw) > 0 {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				pid, err := strconv.Atoi(string(raw))
				if err != nil {
					t.Fatalf("helper %s not ready: %v", role, err)
				}
				h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
				if err != nil {
					t.Fatal(err)
				}
				defer windows.CloseHandle(h)
				// Check every generation after Kill without relying on PID reuse.
				defer func() {
					status, err := windows.WaitForSingleObject(h, 5000)
					if err != nil || status != windows.WAIT_OBJECT_0 {
						t.Errorf("%s survived: status %d, %v", role, status, err)
					}
				}()
			}
			if err := (WindowsProcessGroup{}).Kill(cmd.Process.Pid); err != nil {
				t.Fatal(err)
			}
			processJobs.Lock()
			_, held := processJobs.handles[cmd.Process.Pid]
			processJobs.Unlock()
			if held {
				t.Fatal("job handle leaked")
			}
		})
	}
}

func TestWindowsProcessGroupKillDead(t *testing.T) {
	if err := (WindowsProcessGroup{}).Kill(1 << 30); err != nil {
		t.Fatal(err)
	}
	for _, pid := range []int{0, -1} {
		if err := (WindowsProcessGroup{}).Kill(pid); err == nil {
			t.Fatal("invalid Kill succeeded")
		}
		if err := AttachJob(pid); err == nil {
			t.Fatal("invalid AttachJob succeeded")
		}
	}
}
