package automations

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var scriptJobs sync.Map // *exec.Cmd -> *windowsJob
func configureScriptProcess(cmd *exec.Cmd) error {
	job := &windowsJob{}
	job.configure(cmd)
	cmd.Cancel = func() error { return job.kill() }
	scriptJobs.Store(cmd, job)
	return nil
}
func runScriptProcess(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	value, ok := scriptJobs.Load(cmd)
	if !ok {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("script process has no job")
	}
	if err := value.(*windowsJob).started(cmd); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	return cmd.Wait()
}
func cleanupScriptProcess(cmd *exec.Cmd) {
	if job, ok := scriptJobs.LoadAndDelete(cmd); ok {
		_ = job.(*windowsJob).close()
	}
}
func scriptInvocation(binary string) (string, []string, error) {
	switch binary {
	case "python3":
		return findWindowsPython()
	case "bash":
		path, err := windowsBash()
		return path, nil, err
	default:
		return binary, nil, nil
	}
}

func windowsBash() (string, error) {
	var roots []string
	for _, key := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
		if dir := os.Getenv(key); dir != "" {
			roots = append(roots, filepath.Join(dir, "Git"))
		}
	}
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		roots = append(roots, filepath.Join(dir, "Programs", "Git"))
	}
	if git, err := exec.LookPath("git.exe"); err == nil {
		dir := filepath.Dir(git)
		roots = append(roots, filepath.Dir(dir), filepath.Dir(filepath.Dir(dir)))
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		root := filepath.Dir(dir)
		if strings.EqualFold(filepath.Base(root), "usr") {
			root = filepath.Dir(root)
		}
		if _, err := os.Stat(filepath.Join(root, "cmd", "git.exe")); err == nil {
			roots = append(roots, root)
		}
	}
	for _, root := range roots {
		for _, sub := range []string{"bin", "usr/bin"} {
			candidate, err := filepath.Abs(filepath.Join(root, sub, "bash.exe"))
			if err != nil {
				continue
			}
			if path, err := exec.LookPath(candidate); err == nil {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("bash not available on Windows; install Git for Windows")
}

// Resolve every PATH entry so a Store alias does not hide a real interpreter.
func findWindowsPython() (string, []string, error) {
	for _, name := range []string{"py.exe", "python.exe"} {
		for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
			if dir == "" {
				continue
			}
			candidate, err := filepath.Abs(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			if strings.Contains(strings.ToLower(filepath.ToSlash(candidate)), "/microsoft/windowsapps/") {
				continue
			}
			path, err := exec.LookPath(candidate)
			if err == nil {
				if name == "py.exe" {
					return path, []string{"-3"}, nil
				}
				return path, nil, nil
			}
		}
	}
	return "", nil, errors.New("python 3 not available on Windows; install Python with the py launcher or add python.exe to PATH")
}

// windowsJob owns a kill-on-close job. Children start suspended so descendants
// cannot escape before assignment. Handles are never inherited by the child.
type windowsJob struct {
	mu      sync.Mutex
	handle  windows.Handle
	stopped bool
}

func (j *windowsJob) configure(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED
}

func (j *windowsJob) started(cmd *exec.Cmd) (err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.stopped {
		return os.ErrProcessDone
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = windows.CloseHandle(job)
		}
	}()
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	if err = windows.AssignProcessToJobObject(job, process); err != nil {
		return err
	}
	if err = resumeWindowsProcess(uint32(cmd.Process.Pid)); err != nil {
		return err
	}
	j.handle = job
	return nil
}

func resumeWindowsProcess(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		_, err = windows.ResumeThread(thread)
		windows.CloseHandle(thread)
		return err
	}
	if err != windows.ERROR_NO_MORE_FILES {
		return err
	}
	return fmt.Errorf("suspended process %d has no primary thread", pid)
}

func (j *windowsJob) kill() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.stopped = true
	if j.handle == 0 {
		return nil
	}
	return windows.TerminateJobObject(j.handle, 1)
}
func (j *windowsJob) close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.stopped = true
	if j.handle == 0 {
		return nil
	}
	err := windows.CloseHandle(j.handle)
	j.handle = 0
	return err
}
