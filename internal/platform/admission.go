package platform

import (
	"context"
	"errors"
	"runtime"
)

// ErrAdmissionClosed is returned when acquiring from a closed
// Admission.
var ErrAdmissionClosed = errors.New("platform: admission closed")

// DefaultReservedCPUs is the Appendix B initial value: two logical
// cores stay free for the interactive path (Section 3.2), so at most
// NumCPU-2 background engines occupy CPU at once.
const DefaultReservedCPUs = 2

// Admission is the Section 3.2 admission semaphore: background engines,
// the orchestrator and integrated tools pass through it, while the
// interactive path (gateway, intake, fast lane, master turns,
// read-only status) never blocks on it. Between steps an engine waits
// on channels and holds no slot.
//
// The zero value is unusable; build one with NewAdmission.
type Admission struct {
	sem    chan struct{}
	closed chan struct{}
}

// NewAdmission returns an Admission holding at most n concurrent
// slots. Non-positive n means one slot.
func NewAdmission(n int) *Admission {
	if n <= 0 {
		n = 1
	}
	return &Admission{sem: make(chan struct{}, n), closed: make(chan struct{})}
}

// DefaultAdmission returns an Admission sized to NumCPU-reserved,
// at least one slot. Pass DefaultReservedCPUs for the Appendix B
// behaviour.
func DefaultAdmission(reserved int) *Admission {
	n := runtime.NumCPU() - reserved
	if n < 1 {
		n = 1
	}
	return NewAdmission(n)
}

// Cap reports the maximum concurrent slots.
func (a *Admission) Cap() int {
	if a == nil || a.sem == nil {
		return 0
	}
	return cap(a.sem)
}

// InUse reports the slots currently held.
func (a *Admission) InUse() int {
	if a == nil || a.sem == nil {
		return 0
	}
	return len(a.sem)
}

// Acquire takes one slot, blocking until one is free, ctx expires, or
// the Admission is closed. The returned release function must be
// called exactly once to free the slot; it is idempotent.
func (a *Admission) Acquire(ctx context.Context) (release func(), err error) {
	if a == nil || a.sem == nil {
		return nil, errors.New("platform: nil admission")
	}
	select {
	case <-a.closed:
		return nil, ErrAdmissionClosed
	default:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-a.closed:
		return nil, ErrAdmissionClosed
	case a.sem <- struct{}{}:
	}
	released := false
	return func() {
		if released {
			return
		}
		released = true
		<-a.sem
	}, nil
}

// TryAcquire takes one slot without blocking, reporting false when
// all slots are held or the Admission is closed.
func (a *Admission) TryAcquire() (release func(), ok bool) {
	if a == nil || a.sem == nil {
		return nil, false
	}
	select {
	case <-a.closed:
		return nil, false
	default:
	}
	select {
	case <-a.closed:
		return nil, false
	case a.sem <- struct{}{}:
		released := false
		return func() {
			if released {
				return
			}
			released = true
			<-a.sem
		}, true
	default:
		return nil, false
	}
}

// Close unblocks pending and future Acquire calls with
// ErrAdmissionClosed. Slots held at Close stay valid until released.
func (a *Admission) Close() {
	if a == nil {
		return
	}
	select {
	case <-a.closed:
	default:
		close(a.closed)
	}
}
