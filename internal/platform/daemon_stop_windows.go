package platform

import (
	"context"
	"fmt"
	"os"
	"time"
	"unsafe"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"golang.org/x/sys/windows"
)

func daemonStopEventName(pid int) *uint16 {
	// A PID is unique within this session; the DACL prevents other users
	// from signaling the event. Local keeps it usable without admin rights.
	name, _ := windows.UTF16PtrFromString(fmt.Sprintf(`Local\ExcaliburDaemonStop-%d`, pid))
	return name
}

// ListenDaemonStop installs a user-private graceful stop event. There is no
// shutdown method in the daemon gate API; cancellation follows its normal
// controlled shutdown path without adding a public RPC contract.
func ListenDaemonStop(ctx context.Context, cancel context.CancelFunc) (func(), error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + user.User.Sid.String() + ")(A;;GA;;;SY)")
	if err != nil {
		return nil, err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	event, err := windows.CreateEvent(&sa, 1, 0, daemonStopEventName(os.Getpid()))
	if err != nil {
		return nil, err
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	safego.Go("daemon/windows-stop", nil, func() {
		defer close(done)
		defer windows.CloseHandle(event)
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			default:
			}
			result, err := windows.WaitForSingleObject(event, 100)
			if err != nil {
				return
			}
			if result == windows.WAIT_OBJECT_0 {
				cancel()
				return
			}
		}
	})
	return func() { close(stop); <-done }, nil
}

// RequestDaemonStop first requests normal lifecycle cancellation, waits for
// the process to exit, then terminates it if the grace period expires.
func RequestDaemonStop(pid int, grace time.Duration) error {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	event, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, daemonStopEventName(pid))
	if err == nil {
		_ = windows.SetEvent(event)
		_ = windows.CloseHandle(event)
	}
	result, err := windows.WaitForSingleObject(process, uint32(grace/time.Millisecond))
	if err != nil {
		return err
	}
	if result == windows.WAIT_OBJECT_0 {
		return nil
	}
	if err := windows.TerminateProcess(process, 0); err != nil {
		return err
	}
	// An explicit stop is successful, so Task Scheduler must not restart it.
	result, err = windows.WaitForSingleObject(process, 5000)
	if err != nil {
		return err
	}
	if result != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("platform: daemon did not exit after termination")
	}
	return nil
}
