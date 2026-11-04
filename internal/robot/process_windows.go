package robot

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var robotJobs sync.Map // *exec.Cmd -> *windowsJob
func supported() error { return nil }
func configureProcess(cmd *exec.Cmd) {
	job := &windowsJob{}
	job.configure(cmd)
	robotJobs.Store(cmd, job)
}
func startRobotProcess(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		cleanupRobotProcess(cmd)
		return err
	}
	job, ok := robotJobs.Load(cmd)
	if !ok {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("robot process has no job")
	}
	if err := job.(*windowsJob).started(cmd); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		cleanupRobotProcess(cmd)
		return err
	}
	return nil
}
func stopProcess(cmd *exec.Cmd) error {
	if job, ok := robotJobs.Load(cmd); ok {
		return job.(*windowsJob).kill()
	}
	return nil
}
func cleanupRobotProcess(cmd *exec.Cmd) {
	if job, ok := robotJobs.LoadAndDelete(cmd); ok {
		_ = job.(*windowsJob).close()
	}
}
func robotDataDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(dir, "excalibur")
}
func defaultRobotPython() string {
	return filepath.Join(robotDataDir(), "robot-venv", "Scripts", "python.exe")
}
func defaultRobotScript() string { return filepath.Join(robotDataDir(), "robot-bridge", "server.py") }

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
