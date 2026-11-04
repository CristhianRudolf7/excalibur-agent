package tools

import (
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Handles are never inherited. Closing the daemon or releasing a finished
// session kills every remaining member, including reparented grandchildren.
var sessionJobs = struct {
	sync.Mutex
	handles map[int]windows.Handle
}{handles: make(map[int]windows.Handle)}

func attachSessionProcess(proc *os.Process) (func(), error) {
	if proc == nil {
		// Simulated PTYs have no native process to attach.
		return func() {}, nil
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(proc.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	err = windows.AssignProcessToJobObject(job, process)
	// A very short command may already have exited before assignment.
	if err != nil {
		var code uint32
		if windows.GetExitCodeProcess(process, &code) == nil && code != 259 {
			err = nil
		}
	}
	windows.CloseHandle(process)
	if err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	if err = resumeSessionProcess(proc.Pid); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	sessionJobs.Lock()
	sessionJobs.handles[proc.Pid] = job
	sessionJobs.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			sessionJobs.Lock()
			defer sessionJobs.Unlock()
			delete(sessionJobs.handles, proc.Pid)
			windows.CloseHandle(job)
		})
	}, nil
}

func signalSessionProcess(proc *os.Process, sig os.Signal) (bool, error) {
	if sig != sigINT() {
		return true, signalGroup(proc.Pid, sig == sigKILL())
	}
	// Keep the original job valid while waiting without holding the registry lock.
	var job windows.Handle
	sessionJobs.Lock()
	if original, ok := sessionJobs.handles[proc.Pid]; ok {
		self := windows.CurrentProcess()
		err := windows.DuplicateHandle(self, original, self, &job, 0, false, windows.DUPLICATE_SAME_ACCESS)
		if err != nil {
			sessionJobs.Unlock()
			return true, err
		}
	}
	sessionJobs.Unlock()
	if job != 0 {
		defer windows.CloseHandle(job)
	}

	// CreateProcess publishes a PID before the initial thread connects to the
	// console. Sending CTRL_BREAK then can exit the parent before its child
	// joins the console group; retrying the old group after that is ineffective.
	// Wait for existing job members to connect before the first event. Detached
	// children may never connect, so bound readiness by the existing close margin.
	deadline := time.Now().Add(termKillMargin)
	for {
		pids := []int{proc.Pid}
		if job != 0 {
			pids = jobProcessIDs(job)
		}
		if len(pids) == 0 || job == 0 && !processAlive(proc.Pid) {
			return true, nil
		}
		attached, err := consoleProcessIDs()
		if err != nil {
			return true, err
		}
		ready := true
		for _, pid := range pids {
			if !attached[pid] {
				ready = false
				break
			}
		}
		if ready || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// An interrupt stays an interrupt, even when ignored or delivery fails.
	return true, windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(proc.Pid))
}

var getConsoleProcessList = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList")

func consoleProcessIDs() (map[int]bool, error) {
	for slots := 64; slots <= 65536; slots *= 2 {
		ids := make([]uint32, slots)
		count, _, err := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&ids[0])), uintptr(len(ids)))
		if count == 0 {
			return nil, err
		}
		if count > uintptr(len(ids)) {
			continue
		}
		attached := make(map[int]bool, count)
		for _, pid := range ids[:count] {
			attached[int(pid)] = true
		}
		return attached, nil
	}
	return nil, windows.ERROR_MORE_DATA
}

func readProcessTable() processTable {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snapshot)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	var table processTable
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		table = append(table, procInfo{PID: int(entry.ProcessID), PPID: int(entry.ParentProcessID), Comm: commandBaseForOS(windows.UTF16ToString(entry.ExeFile[:]), true), Argv: windowsProcessCommandLine(entry.ProcessID)})
	}
	// Job membership keeps descendants visible after their parent exits.
	sessionJobs.Lock()
	defer sessionJobs.Unlock()
	for root, job := range sessionJobs.handles {
		for _, pid := range jobProcessIDs(job) {
			for i := range table {
				if table[i].PID == pid {
					table[i].PGID = root
				}
			}
		}
	}
	return table
}

func jobProcessIDs(job windows.Handle) []int {
	for slots := 64; slots <= 65536; slots *= 2 {
		buf := make([]uintptr, slots+2)
		err := windows.QueryInformationJobObject(job, windows.JobObjectBasicProcessIdList, uintptr(unsafe.Pointer(&buf[0])), uint32(len(buf)*int(unsafe.Sizeof(uintptr(0)))), nil)
		if errors.Is(err, windows.ERROR_MORE_DATA) {
			continue
		}
		if err != nil {
			return nil
		}
		header := (*[2]uint32)(unsafe.Pointer(&buf[0]))
		count := int(header[1])
		offset := 8 / int(unsafe.Sizeof(uintptr(0)))
		if count > len(buf)-offset {
			return nil
		}
		ids := make([]int, count)
		for i := range ids {
			ids[i] = int(buf[offset+i])
		}
		return ids
	}
	return nil
}

// Windows does not expose other processes' console handles through Toolhelp.
func processHoldsTerminal(pid int) bool { return false }

func signalGroup(pgid int, kill bool) error {
	if pgid <= 0 {
		return nil
	}
	sessionJobs.Lock()
	if job, ok := sessionJobs.handles[pgid]; ok {
		err := windows.TerminateJobObject(job, 1)
		sessionJobs.Unlock()
		return err
	}
	sessionJobs.Unlock()
	// Detached services intentionally have no kill-on-close job. Snapshot their
	// descendants before terminating the root, then terminate leaves first.
	tree := readProcessTable().tree(pgid)
	var result error
	for i := len(tree) - 1; i >= 0; i-- {
		pid := tree[i].PID
		h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			continue
		}
		if err == nil {
			err = windows.TerminateProcess(h, 1)
			windows.CloseHandle(h)
		}
		if err != nil && processAlive(pid) {
			result = errors.Join(result, err)
		}
	}
	return result
}

func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h)
	state, err := windows.WaitForSingleObject(h, 0)
	return err == nil && state == uint32(windows.WAIT_TIMEOUT)
}

func groupAlive(pgid int) bool {
	if pgid <= 0 {
		return false
	}
	sessionJobs.Lock()
	job, ok := sessionJobs.handles[pgid]
	if ok {
		alive := len(jobProcessIDs(job)) > 0
		sessionJobs.Unlock()
		return alive
	}
	sessionJobs.Unlock()
	return len(readProcessTable().tree(pgid)) > 0
}

var getExtendedTCPTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

func processTreeListeningTCP(root int) bool {
	pids := map[uint32]bool{}
	for _, p := range readProcessTable().tree(root) {
		pids[uint32(p.PID)] = true
	}
	for _, family := range []uint32{windows.AF_INET, windows.AF_INET6} {
		size := uint32(0)
		getExtendedTCPTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, uintptr(family), 3, 0)
		for attempts := 0; attempts < 3 && size >= 4; attempts++ {
			buf := make([]byte, size)
			code, _, _ := getExtendedTCPTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, uintptr(family), 3, 0)
			if code == uintptr(windows.ERROR_INSUFFICIENT_BUFFER) {
				continue
			}
			if code != 0 {
				break
			}
			rowSize, pidOffset := 24, 20
			if family == windows.AF_INET6 {
				rowSize, pidOffset = 56, 52
			}
			count := int(binary.LittleEndian.Uint32(buf[:4]))
			if count > (len(buf)-4)/rowSize {
				break
			}
			for i := 0; i < count; i++ {
				offset := 4 + i*rowSize + pidOffset
				if pids[binary.LittleEndian.Uint32(buf[offset:offset+4])] {
					return true
				}
			}
			break
		}
	}
	return false
}

// Suspend the initial thread until the job owns the process. This closes the
// startup race in which a fast shell could create an unowned grandchild.
func prepareSessionProcess(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED | windows.CREATE_NEW_PROCESS_GROUP
	c.SysProcAttr.HideWindow = true
}

func resumeSessionProcess(pid int) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	var entry windows.ThreadEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != uint32(pid) {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		_, err = windows.ResumeThread(thread)
		windows.CloseHandle(thread)
		if err != nil {
			return err
		}
	}
	if err != nil && !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return err
	}
	return nil
}

func ignoreCompletedSessionSignal(exited bool) bool { return exited }

// Toolhelp supplies parent IDs and image names. Query the command line too so
// the existing finish-gate classifiers can distinguish go build and python -m pip.
func windowsProcessCommandLine(pid uint32) string {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(process)
	var size uint32
	_ = windows.NtQueryInformationProcess(process, windows.ProcessCommandLineInformation, nil, 0, &size)
	if size < uint32(unsafe.Sizeof(windows.NTUnicodeString{})) || size > 1024*1024 {
		return ""
	}
	buf := make([]byte, size)
	if windows.NtQueryInformationProcess(process, windows.ProcessCommandLineInformation, unsafe.Pointer(&buf[0]), size, &size) != nil {
		return ""
	}
	value := (*windows.NTUnicodeString)(unsafe.Pointer(&buf[0]))
	start := uintptr(unsafe.Pointer(&buf[0]))
	textStart := uintptr(unsafe.Pointer(value.Buffer))
	if textStart < start || textStart+uintptr(value.Length) > start+uintptr(len(buf)) || value.Length%2 != 0 {
		return ""
	}
	return windows.UTF16ToString(unsafe.Slice(value.Buffer, int(value.Length)/2))
}
