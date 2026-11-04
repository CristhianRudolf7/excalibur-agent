package pdf

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"golang.org/x/sys/windows"
)

// Assign the kill-on-close job before the worker can execute any code. Pipes
// remain ordinary os/exec pipes; the worker never inherits the job handle.
func startWorkerProcess(cmd *exec.Cmd, group platform.ProcessGroup) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	if err := cmd.Start(); err != nil {
		return err
	}
	attach := platform.AttachJob
	if g, ok := group.(interface{ AttachJob(int) error }); ok {
		attach = g.AttachJob
	}
	err := attach(cmd.Process.Pid)
	if err == nil {
		err = resumeWorkerProcess(uint32(cmd.Process.Pid))
	}
	if err != nil {
		_ = group.Kill(cmd.Process.Pid)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("pdf: attach worker job: %w", err)
	}
	return nil
}

func resumeWorkerProcess(pid uint32) error {
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
	return fmt.Errorf("pdf: worker primary thread unavailable: %w", err)
}
