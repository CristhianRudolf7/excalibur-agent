package safego

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

// FailFunc marks the owner of a crashed goroutine as failed. Owners
// typically transition a turn, run or adapter to failed and record the
// cause; writer-goroutine owners exit the process instead.
type FailFunc func(owner string, recovered any, stack []byte)

// Go launches fn as a long-lived goroutine owned by owner. If fn
// panics, Go logs the panic with its stack, calls onFail exactly once
// and returns normally: the daemon survives. A nil onFail only logs.
func Go(owner string, onFail FailFunc, fn func()) {
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				stack := debug.Stack()
				slog.Error("goroutine panicked",
					"owner", owner,
					"panic", fmt.Sprintf("%v", recovered),
					"stack", string(stack),
				)
				if onFail != nil {
					func() {
						defer func() {
							// The failure callback must never crash the
							// guard itself.
							_ = recover()
						}()
						onFail(owner, recovered, stack)
					}()
				}
			}
		}()
		fn()
	}()
}
