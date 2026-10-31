// Package cpupool is the Section 3.2 worker pool for CPU-intensive
// background work (local embeddings, tree-sitter, compression,
// fingerprinting). Each worker pins its OS thread and lowers that
// thread's priority so bulk CPU work does not stall the interactive
// path. Go has no priorities between goroutines, so admission plus
// thread-level nice is the mechanism.
//
// Workers are long-lived goroutines launched via safego.Go: a panicking
// task marks only that task failed (the worker survives); a panicking
// worker itself is contained by the safego guard.
package cpupool

import (
	"errors"
	"runtime"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// ErrClosed is returned when submitting to a closed Pool.
// ErrFull is returned by TrySubmit when no worker is immediately free.
var (
	ErrClosed = errors.New("cpupool: pool closed")
	ErrFull   = errors.New("cpupool: no worker available")
)

// DefaultSize returns the default worker count: one per logical CPU.
// Callers that share the machine with the admission-capped engines
// pass a smaller size explicitly.
func DefaultSize() int {
	if n := runtime.NumCPU(); n > 0 {
		return n
	}
	return 1
}

// Pool runs submitted tasks on low-priority pinned threads.
type Pool struct {
	tasks  chan func()
	wg     sync.WaitGroup
	once   sync.Once
	closed chan struct{}
	size   int
}

// New returns a Pool with size workers and a 4*size queue using the
// platform-default thread lowering. Non-positive size means
// DefaultSize.
func New(size int) *Pool {
	return NewWithLower(size, 0, lowerCurrentThread)
}

// NewWithLower returns a Pool with size workers, an explicit queue
// length (non-positive means 4*size) and a custom thread-lowering
// hook (nil means none). The hook runs once per worker thread after
// it is pinned.
func NewWithLower(size, queueLen int, lower func() error) *Pool {
	if size <= 0 {
		size = DefaultSize()
	}
	if queueLen <= 0 {
		queueLen = 4 * size
	}
	p := &Pool{
		tasks:  make(chan func(), queueLen),
		closed: make(chan struct{}),
		size:   size,
	}
	for i := 0; i < size; i++ {
		p.wg.Add(1)
		worker := &worker{tasks: p.tasks, done: p.wg.Done, lower: lower}
		safego.Go("cpupool-worker", nil, worker.run)
	}
	return p
}

// Submit queues task, blocking until a worker takes it. It returns
// ErrClosed after Close.
func (p *Pool) Submit(task func()) error {
	select {
	case <-p.closed:
		return ErrClosed
	case p.tasks <- task:
		return nil
	}
}

// TrySubmit queues task without blocking. It returns ErrFull when the
// queue is full and ErrClosed after Close.
func (p *Pool) TrySubmit(task func()) error {
	select {
	case <-p.closed:
		return ErrClosed
	default:
	}
	select {
	case <-p.closed:
		return ErrClosed
	case p.tasks <- task:
		return nil
	default:
		return ErrFull
	}
}

// Close drains queued tasks, stops the workers and waits for them. It
// is idempotent. Tasks submitted after Close fail with ErrClosed.
func (p *Pool) Close() {
	p.once.Do(func() {
		close(p.closed)
		close(p.tasks)
	})
	p.wg.Wait()
}

// Size reports the worker count.
func (p *Pool) Size() int {
	if p == nil {
		return 0
	}
	return p.size
}

// worker pins one thread, lowers it once, then serves tasks until the
// queue closes. A panicking task fails alone: the deferred recover
// keeps the worker alive for the next task.
type worker struct {
	tasks chan func()
	done  func()
	lower func() error
}

func (w *worker) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer w.done()
	if w.lower != nil {
		_ = w.lower()
	}
	for task := range w.tasks {
		func() {
			defer func() {
				_ = recover()
			}()
			if task != nil {
				task()
			}
		}()
	}
}
