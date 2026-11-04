package tools

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"golang.org/x/sys/windows"
)

// Isolate the console from go test and its caller. These cases exercise a
// successful GenerateConsoleCtrlEvent even when tests run through SSH or CI.
func TestWindowsInterrupt(t *testing.T) {
	for _, behavior := range []string{"graceful", "ignore", "orphan", "detached", "late-attach", "bash-sleep", "no-console"} {
		t.Run(behavior, func(t *testing.T) {
			c := exec.Command(os.Args[0], "-test.run=^TestWindowsInterruptHelper$", "-test.timeout=30s", "-test.v")
			c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE, HideWindow: true}
			if behavior == "no-console" {
				c.SysProcAttr.CreationFlags = windows.DETACHED_PROCESS
			}
			c.Env = mergeEnv(os.Environ(), map[string]string{
				"EXCALIBUR_WINDOWS_INTERRUPT_HELPER":   "host",
				"EXCALIBUR_WINDOWS_INTERRUPT_BEHAVIOR": behavior,
				"EXCALIBUR_WINDOWS_INTERRUPT_PATH":     filepath.Join(t.TempDir(), "tree"),
			}, nil)
			if out, err := c.CombinedOutput(); err != nil {
				t.Fatalf("console host: %v\n%s", err, out)
			}
		})
	}
}

func TestWindowsInterruptHelper(t *testing.T) {
	mode := os.Getenv("EXCALIBUR_WINDOWS_INTERRUPT_HELPER")
	if mode == "" {
		return
	}
	behavior := os.Getenv("EXCALIBUR_WINDOWS_INTERRUPT_BEHAVIOR")
	path := os.Getenv("EXCALIBUR_WINDOWS_INTERRUPT_PATH")
	if mode == "host" {
		if behavior == "bash-sleep" {
			testWindowsBashInterrupt(t, path)
		} else {
			testWindowsConsoleInterrupt(t, behavior, path)
		}
		return
	}
	interrupts := make(chan os.Signal, 4)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	if mode == "root" {
		c := exec.Command(os.Args[0], "-test.run=^TestWindowsInterruptHelper$")
		c.Env = mergeEnv(os.Environ(), map[string]string{"EXCALIBUR_WINDOWS_INTERRUPT_HELPER": "leaf"}, nil)
		if behavior == "late-attach" {
			c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
		}
		if behavior == "orphan" {
			c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
		}
		if behavior == "detached" || behavior == "no-console" {
			c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS}
		}
		if err := c.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := os.WriteFile(path+".pid", []byte(strconv.Itoa(c.Process.Pid)), 0600); err != nil {
			os.Exit(3)
		}
	} else if err := os.WriteFile(path+".ready", []byte("ready"), 0600); err != nil {
		os.Exit(4)
	}
	for range interrupts {
		if err := os.WriteFile(path+"."+mode+".break", []byte("received"), 0600); err != nil {
			os.Exit(5)
		}
		if behavior == "graceful" || behavior == "late-attach" || mode == "root" && behavior != "ignore" {
			os.Exit(0)
		}
	}
}

func testWindowsConsoleInterrupt(t *testing.T, behavior, path string) {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^TestWindowsInterruptHelper$")
	c.Env = mergeEnv(os.Environ(), map[string]string{"EXCALIBUR_WINDOWS_INTERRUPT_HELPER": "root"}, nil)
	prepareSessionProcess(c)
	if behavior == "no-console" {
		c.SysProcAttr.CreationFlags |= windows.DETACHED_PROCESS
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	cleanup, err := attachSessionProcess(c.Process)
	if err != nil {
		c.Process.Kill()
		c.Wait()
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup(); c.Wait() })
	child := 0
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(path + ".pid")
		child, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
		if ready, _ := os.ReadFile(path + ".ready"); child > 0 && (behavior == "late-attach" || string(ready) == "ready") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ready, _ := os.ReadFile(path + ".ready"); child <= 0 || behavior != "late-attach" && string(ready) != "ready" {
		t.Fatal("grandchild did not publish readiness and PID")
	}
	if behavior == "no-console" {
		if handled, err := signalSessionProcess(c.Process, sigINT()); !handled || err == nil {
			t.Fatalf("unavailable console: handled=%v err=%v", handled, err)
		}
		time.Sleep(termKillMargin + 100*time.Millisecond)
		if !processAlive(c.Process.Pid) || !processAlive(child) {
			t.Fatal("failed interrupt delivery terminated the process tree")
		}
		return
	}
	if behavior == "late-attach" {
		attached, err := consoleProcessIDs()
		if err != nil {
			t.Fatal(err)
		}
		if attached[child] {
			t.Fatal("suspended child already attached to console")
		}
		resumed := make(chan error, 1)
		// Hold the child before console initialization until delivery has begun.
		safego.Go("test/windows-late-console-attach", nil, func() {
			time.Sleep(100 * time.Millisecond)
			resumed <- resumeSessionProcess(child)
		})
		t.Cleanup(func() {
			if err := <-resumed; err != nil {
				t.Error(err)
			}
		})
	}
	if handled, err := signalSessionProcess(c.Process, sigINT()); !handled || err != nil {
		t.Fatalf("interrupt: handled=%v err=%v", handled, err)
	}

	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, rootErr := os.Stat(path + ".root.break")
		_, leafErr := os.Stat(path + ".leaf.break")
		if rootErr == nil && (behavior == "orphan" || behavior == "detached" || behavior == "late-attach" || leafErr == nil) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if behavior == "graceful" || behavior == "late-attach" {
		for time.Now().Before(deadline) && (processAlive(c.Process.Pid) || processAlive(child)) {
			time.Sleep(10 * time.Millisecond)
		}
		if processAlive(c.Process.Pid) || processAlive(child) {
			t.Fatal("cooperative interrupt left root or grandchild alive")
		}
	} else {
		// Remain alive beyond the close/cancellation margin: interrupt must never
		// escalate, including for a descendant outside the console process group.
		time.Sleep(termKillMargin + 100*time.Millisecond)
		if !processAlive(child) || behavior == "ignore" && !processAlive(c.Process.Pid) {
			t.Fatal("interrupt terminated a process that did not exit on CTRL_BREAK")
		}
	}
	if got, _ := os.ReadFile(path + ".root.break"); string(got) != "received" {
		t.Fatal("root did not receive CTRL_BREAK in the shared console")
	}
	if behavior == "graceful" || behavior == "ignore" {
		if got, _ := os.ReadFile(path + ".leaf.break"); string(got) != "received" {
			t.Fatal("grandchild did not receive CTRL_BREAK in the shared group")
		}
	} else if behavior == "orphan" || behavior == "detached" {
		if _, err := os.Stat(path + ".leaf.break"); !os.IsNotExist(err) {
			t.Fatalf("grandchild outside the console group received CTRL_BREAK: %v", err)
		}
	}
	if behavior == "graceful" || behavior == "late-attach" {
		if err := c.Wait(); err != nil {
			t.Fatalf("cooperative root was forcefully terminated: %v", err)
		}
	}
	if err := signalGroup(c.Process.Pid, true); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && groupAlive(c.Process.Pid) {
		time.Sleep(10 * time.Millisecond)
	}
	if handled, err := signalSessionProcess(c.Process, sigINT()); !handled || err != nil {
		t.Fatalf("already dead: handled=%v err=%v", handled, err)
	}
}

// Exercise MSYS signal translation as well as the native Go control handler.
func testWindowsBashInterrupt(t *testing.T, path string) {
	t.Helper()
	shell := filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe")
	if _, err := os.Stat(shell); err != nil {
		t.Skip("Git Bash is not installed")
	}
	c := exec.Command(shell, "-c", `sleep 60 & child=$!; ps -p "$child" > "$EXCALIBUR_WINDOWS_INTERRUPT_PATH.pid"; wait`)
	prepareSessionProcess(c)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	cleanup, err := attachSessionProcess(c.Process)
	if err != nil {
		c.Process.Kill()
		c.Wait()
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup(); c.Wait() })
	deadline := time.Now().Add(10 * time.Second)
	child := 0
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(path + ".pid")
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		if len(lines) > 1 {
			fields := strings.Fields(lines[1])
			for i, field := range strings.Fields(lines[0]) {
				if field == "WINPID" && i < len(fields) {
					child, _ = strconv.Atoi(fields[i])
				}
			}
		}
		if child > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if child <= 0 {
		t.Fatal("Git Bash sleep did not publish its Windows PID")
	}
	if handled, err := signalSessionProcess(c.Process, sigINT()); !handled || err != nil {
		t.Fatalf("interrupt: handled=%v err=%v", handled, err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && (processAlive(c.Process.Pid) || processAlive(child)) {
		time.Sleep(10 * time.Millisecond)
	}
	if processAlive(c.Process.Pid) || processAlive(child) {
		t.Fatalf("Git Bash interrupt survivors: root=%v sleep=%v", processAlive(c.Process.Pid), processAlive(child))
	}
}
