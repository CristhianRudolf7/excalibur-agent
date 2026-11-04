package tools

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"golang.org/x/sys/windows"
)

func TestDiagramJobLimits(t *testing.T) {
	job, err := newDiagramJob(1<<30, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(job)
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	err = windows.QueryInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)), nil)
	if err != nil {
		t.Fatal(err)
	}
	flags := uint32(windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_JOB_MEMORY | windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS)
	if limits.BasicLimitInformation.LimitFlags&flags != flags || limits.JobMemoryLimit != 1<<30 || limits.BasicLimitInformation.ActiveProcessLimit != 3 {
		t.Fatalf("limits: %+v", limits)
	}
	// With one active process allowed, the helper must fail to start a child.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDiagramProcessHelper$")
	cmd.Env = append(os.Environ(), "EXCALIBUR_DIAGRAM_HELPER=limit")
	if err := runLimitedDiagramCommand(cmd, 1<<30, 1); err != nil {
		t.Fatal(err)
	}
}

func TestDiagramJobCancellationKillsTree(t *testing.T) {
	testDiagramTreeCleanup(t, true)
}

func TestDiagramJobCloseKillsTree(t *testing.T) {
	testDiagramTreeCleanup(t, false)
}

func testDiagramTreeCleanup(t *testing.T, cancelParent bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDiagramProcessHelper$")
	mode := "exit"
	if cancelParent {
		mode = "wait"
	}
	cmd.Env = append(os.Environ(), "EXCALIBUR_DIAGRAM_HELPER="+mode, "EXCALIBUR_DIAGRAM_PID="+pidFile)
	done := make(chan error, 1)
	safego.Go("test/diagram-job", nil, func() { done <- runDiagramCommand(cmd) })
	var data []byte
	for ctx.Err() == nil {
		data, _ = os.ReadFile(pidFile)
		if len(data) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(data) == 0 {
		cancel()
		<-done
		t.Fatal("helper never wrote child PID")
	}
	if cancelParent {
		cancel()
	}
	err := <-done
	if cancelParent && err == nil {
		t.Fatal("cancelled renderer succeeded")
	}
	if !cancelParent && err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return
	} // Already reaped.
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	state, err := windows.WaitForSingleObject(handle, 5000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("descendant survived: state %d, %v", state, err)
	}
}

func TestDiagramProcessHelper(t *testing.T) {
	mode := os.Getenv("EXCALIBUR_DIAGRAM_HELPER")
	if mode == "" {
		return
	}
	if mode == "child" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestDiagramProcessHelper$")
	child.Env = append(os.Environ(), "EXCALIBUR_DIAGRAM_HELPER=child")
	err := child.Start()
	if mode == "limit" {
		if err == nil {
			_ = child.Process.Kill()
			_ = child.Wait()
			os.Exit(2)
		}
		os.Exit(0)
	}
	if err != nil {
		os.Exit(3)
	}
	if err := os.WriteFile(os.Getenv("EXCALIBUR_DIAGRAM_PID"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
		os.Exit(4)
	}
	if mode == "wait" {
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}

func TestWindowsPlantUMLCandidates(t *testing.T) {
	t.Setenv("EXCALIBUR_PLANTUML_JAR", "")
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("ProgramData", root)
	t.Setenv("SCOOP", filepath.Join(root, "user-scoop"))
	jar := filepath.Join(root, "chocolatey", "lib", "plantuml", "tools", "plantuml.jar")
	if err := os.MkdirAll(filepath.Dir(jar), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jar, nil, 0600); err != nil {
		t.Fatal(err)
	}
	backend := SVGDiagramBackend{LookPath: func(name string) (string, error) {
		if name == "java.exe" {
			return "java.exe", nil
		}
		return "", exec.ErrNotFound
	}}
	bin, args, pipe := backend.command("plantuml", "svg", "in", "out")
	if bin != "java.exe" || !pipe || !strings.Contains(strings.Join(args, "|"), jar) {
		t.Fatalf("%q %v %v", bin, args, pipe)
	}
	scoop := filepath.Join(os.Getenv("SCOOP"), "apps", "plantuml", "current", "plantuml.jar")
	found := false
	for _, path := range plantUMLJars() {
		found = found || path == scoop
	}
	if !found {
		t.Fatal("missing scoop path")
	}
}
