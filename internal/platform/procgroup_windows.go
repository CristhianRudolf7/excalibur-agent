package platform

import (
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WindowsProcessGroup runs each tool in a new process group (Sections 3.2,
// 13.4). AttachJob adds kill-on-close ownership after the command starts.
type WindowsProcessGroup struct{}

var _ ProcessGroup = WindowsProcessGroup{}

// Configure preserves existing process attributes and creates a new group.
func (WindowsProcessGroup) Configure(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP
}

var processJobs = struct {
	sync.Mutex
	handles map[int]windows.Handle
}{handles: make(map[int]windows.Handle)}

// AttachJob assigns pid to a kill-on-close Job Object. Call immediately after
// starting the process, before it creates descendants: existing descendants
// are not retroactively assigned. Kill releases the registered job handle.
// The daemon exiting also closes the handle and terminates job members.
func AttachJob(pid int) error {
	if pid <= 0 {
		return errors.New("platform: AttachJob requires a positive pid")
	}
	processJobs.Lock()
	defer processJobs.Unlock()
	if _, ok := processJobs.handles[pid]; ok {
		return nil
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err == nil {
		err = windows.AssignProcessToJobObject(job, process)
	}
	if err != nil {
		windows.CloseHandle(job)
		return err
	}
	processJobs.handles[pid] = job
	return nil
}

// AttachJob is the post-start counterpart of Configure.
func (WindowsProcessGroup) AttachJob(pid int) error { return AttachJob(pid) }

// Kill terminates all job members, or walks a process snapshot children first.
// An already exited process is success, matching Linux ESRCH handling.
func (WindowsProcessGroup) Kill(pid int) error {
	if pid <= 0 {
		return errors.New("platform: Kill requires a positive pid")
	}
	if found, err := terminateRegisteredJob(pid); found {
		return err
	}
	entries, err := processSnapshot()
	if err != nil {
		return err
	}
	var result error
	for _, id := range processTreePostorder(uint32(pid), entries) {
		result = errors.Join(result, terminateProcess(id))
	}
	return result
}

// terminateRegisteredJob never falls back to a PID lookup. Session reapers
// use it after the process has exited, when its PID could already be reused.
func terminateRegisteredJob(pid int) (bool, error) {
	processJobs.Lock()
	defer processJobs.Unlock()
	job, ok := processJobs.handles[pid]
	if !ok {
		return false, nil
	}
	delete(processJobs.handles, pid)
	return true, errors.Join(windows.TerminateJobObject(job, 1), windows.CloseHandle(job))
}

func processSnapshot() ([]processParent, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	var entries []processParent
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		entries = append(entries, processParent{PID: entry.ProcessID, Parent: entry.ParentProcessID})
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, err
	}
	return entries, nil
}

func terminateProcess(pid uint32) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	if err = windows.TerminateProcess(h, 1); err != nil {
		var exit uint32
		if windows.GetExitCodeProcess(h, &exit) == nil && exit != 259 {
			return nil
		}
	}
	return err
}
