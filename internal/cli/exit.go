package cli

import (
	"errors"
	"fmt"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Stable process exit codes. The 0/2/3/4/5/130 mapping mirrors the
// exec final-state codes of Section 6.5; 69 (EX_UNAVAILABLE) marks
// commands whose backend has not landed yet; 1 is a harness error.
const (
	ExitOK             = 0
	ExitError          = 1
	ExitPending        = 2
	ExitBlocked        = 3
	ExitPaused         = 4
	ExitFailed         = 5
	ExitNotImplemented = 69
	ExitCanceled       = 130
)

// CodedError carries a stable process exit code with its cause, so
// every CLI and delegation order chains without parsing free text
// (Section 3.6: stable codes and --json everywhere).
type CodedError struct {
	Code int
	Err  error
}

// Error implements error.
func (e *CodedError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit %d", e.Code)
	}
	return e.Err.Error()
}

// Unwrap returns the cause.
func (e *CodedError) Unwrap() error { return e.Err }

// ExitCode unwraps err to its stable code: CodedError codes win, and
// anything else is a harness error.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var ee *CodedError
	if errors.As(err, &ee) {
		return ee.Code
	}
	return ExitError
}

// CodeForRunState maps the final run state of Section 7.2 to the
// Section 6.5 process exit code.
func CodeForRunState(s types.RunState) int {
	switch s {
	case types.RunCompleted:
		return ExitOK
	case types.RunCompletedPending:
		return ExitPending
	case types.RunBlocked:
		return ExitBlocked
	case types.RunPaused:
		return ExitPaused
	case types.RunFailed:
		return ExitFailed
	case types.RunCanceled:
		return ExitCanceled
	default:
		return ExitError
	}
}

// CodeForTaskState maps the derived task state (Section 8.2) to the
// same process exit code, so `task wait` and `exec` agree.
func CodeForTaskState(s types.TaskState) int {
	switch s {
	case types.TaskCompleted:
		return ExitOK
	case types.TaskCompletedPending:
		return ExitPending
	case types.TaskBlocked:
		return ExitBlocked
	case types.TaskPaused:
		return ExitPaused
	case types.TaskFailed:
		return ExitFailed
	case types.TaskCanceled:
		return ExitCanceled
	default:
		return ExitError
	}
}
