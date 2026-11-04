package platform

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SignalTarget describes a process terminal commands must not terminate.
// Windows has no queryable POSIX process-group ID, so Group is zero.
type SignalTarget struct {
	PID, Group    int
	Name, Command string
}

// ProtectedSignalTargets includes the harness and its surviving ancestors.
// Names come from Toolhelp; Command is the full image path when queryable.
// Windows does not expose other processes' command lines through Toolhelp.
func ProtectedSignalTargets() ([]SignalTarget, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	entries := make(map[uint32]windows.ProcessEntry32)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		entries[entry.ProcessID] = entry
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, err
	}
	var targets []SignalTarget
	seen := make(map[uint32]bool)
	for pid := uint32(os.Getpid()); pid != 0 && !seen[pid]; {
		seen[pid] = true
		e, ok := entries[pid]
		if !ok {
			break
		}
		name := windows.UTF16ToString(e.ExeFile[:])
		command := name
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
		if err == nil {
			path := make([]uint16, 32768)
			n := uint32(len(path))
			if windows.QueryFullProcessImageName(h, 0, &path[0], &n) == nil {
				command = windows.UTF16ToString(path[:n])
			}
			windows.CloseHandle(h)
		}
		targets = append(targets, SignalTarget{PID: int(pid), Name: name, Command: command})
		pid = e.ParentProcessID
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("platform: harness absent from process snapshot")
	}
	return targets, nil
}

// Reexec is unsupported: Windows cannot replace an image while preserving its
// PID and signal ownership. Spawning a replacement would break that contract.
func Reexec(argv, env []string) error { return errors.ErrUnsupported }
