package store

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"time"
)

// Write implements Store: it enqueues fn on lane p and blocks the
// caller on backpressure until there is room or ctx expires
// (Section 4.3). Commands canceled before the writer takes them are
// discarded with ErrWriteCanceled. While a disk-full episode lasts,
// background writes fail fast so consolidation and indexing pause and
// the interactive path keeps going.
func (db *DB) Write(ctx context.Context, p Priority, fn WriteFunc) error {
	if p < PriorityInteractive || p > PriorityBackground {
		return fmt.Errorf("store: invalid priority %d", int(p))
	}
	if fn == nil {
		return fmt.Errorf("store: nil write func")
	}
	db.mu.Lock()
	closed := db.closed
	db.mu.Unlock()
	if closed {
		return ErrClosed
	}
	if p == PriorityBackground && db.diskFull.Load() {
		return ErrDiskFull
	}
	if err := ctx.Err(); err != nil {
		db.addCanceled(p)
		return ErrWriteCanceled
	}
	resp := make(chan error, 1)
	cmd := command{prio: p, fn: fn, resp: resp, ctx: ctx, enqueuedAt: time.Now()}
	select {
	case db.queues[p] <- cmd:
	case <-db.stop:
		return ErrClosed
	case <-ctx.Done():
		db.addCanceled(p)
		return ErrWriteCanceled
	}
	select {
	case db.notifyChan <- struct{}{}:
	default:
	}
	select {
	case err := <-resp:
		return err
	case <-ctx.Done():
		// The command stays queued and will run; its reply is
		// dropped on the buffered channel. The caller observes
		// its own cancellation, mirroring context semantics.
		return ctx.Err()
	case <-db.stop:
		// Draining: the writer finishes queued work before
		// exiting, so wait for the outcome instead of failing.
		select {
		case err := <-resp:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// run is the porter: one writer goroutine per file, short
// transactions, group commit (Section 4.3). It must only be launched
// via safego.Go (see Open).
func (db *DB) run() {
	defer close(db.done)
	for {
		first, ok := db.take()
		if !ok {
			return
		}
		db.executeBatch(first)
		db.afterBatch()
	}
}

// take blocks until the highest-priority queued command is available.
// It returns false once Close drained every lane.
func (db *DB) take() (command, bool) {
	for {
		for i := range db.queues {
			select {
			case cmd := <-db.queues[i]:
				return cmd, true
			default:
			}
		}
		select {
		case <-db.notifyChan:
		case <-db.stop:
			for i := range db.queues {
				select {
				case cmd := <-db.queues[i]:
					return cmd, true
				default:
				}
			}
			return command{}, false
		}
	}
}

// drainLane takes every queued command of one lane without blocking.
func (db *DB) drainLane(lane int, batch []command, max int) []command {
	for len(batch) < max {
		select {
		case cmd := <-db.queues[lane]:
			batch = append(batch, cmd)
		default:
			return batch
		}
	}
	return batch
}

// gather collects same-or-higher priority commands until the group
// commit window closes or the batch is full (Section 4.3).
func (db *DB) gather(first command) []command {
	max := db.opts.GroupCommitMaxOps
	if max < 1 {
		max = 1
	}
	batch := []command{first}
	for i := 0; i <= int(first.prio); i++ {
		batch = db.drainLane(i, batch, max)
	}
	if len(batch) >= max {
		return batch
	}
	timer := time.NewTimer(db.opts.GroupCommitWait)
	defer timer.Stop()
	for len(batch) < max {
		select {
		case <-db.notifyChan:
			for i := 0; i <= int(first.prio); i++ {
				batch = db.drainLane(i, batch, max)
			}
			if len(batch) >= max {
				return batch
			}
		case <-timer.C:
			return batch
		case <-db.stop:
			for i := 0; i <= int(first.prio); i++ {
				batch = db.drainLane(i, batch, max)
			}
			return batch
		}
	}
	return batch
}

// executeBatch runs one group commit: BEGIN IMMEDIATE (never a
// deferred transaction, whose promotion fails with SQLITE_BUSY), one
// savepoint per command so a constraint failure rolls back only that
// command, a single COMMIT, and one reply per command (Section 4.3).
// Replies go out only after the commit decision, so no success is
// reported for rolled-back work.
func (db *DB) executeBatch(first command) {
	batch := db.gather(first)
	results := make([]error, len(batch))
	decided := make([]bool, len(batch))

	frame := context.Background()
	if err := db.beginImmediate(frame); err != nil {
		for i := range batch {
			results[i] = err
			decided[i] = true
		}
		db.replyBatch(batch, results)
		switch err {
		case ErrBusy:
			db.addBusyError()
		case ErrDiskFull:
			db.addFullError()
		}
		return
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = db.writerConn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	ex := connExecer{conn: db.writerConn}
	aborted := false
	for i, cmd := range batch {
		if cmd.ctx.Err() != nil {
			results[i] = ErrWriteCanceled
			decided[i] = true
			db.addCanceled(cmd.prio)
			continue
		}
		wait := time.Since(cmd.enqueuedAt)
		sp := fmt.Sprintf("sp_%d", i)
		if _, err := db.writerConn.ExecContext(frame, "SAVEPOINT "+sp); err != nil {
			aborted = true
			abortErr := fmt.Errorf("store: savepoint: %w", err)
			for j := i; j < len(batch); j++ {
				if !decided[j] {
					results[j] = abortErr
					decided[j] = true
				}
			}
			break
		}
		start := time.Now()
		err := cmd.fn(cmd.ctx, ex)
		db.addQueueWait(cmd.prio, wait)
		db.addExecTime(cmd.prio, time.Since(start))
		if err == nil {
			if _, rerr := db.writerConn.ExecContext(frame, "RELEASE "+sp); rerr != nil {
				aborted = true
				abortErr := fmt.Errorf("store: release savepoint: %w", rerr)
				for j := i; j < len(batch); j++ {
					if !decided[j] {
						results[j] = abortErr
						decided[j] = true
					}
				}
				break
			}
			results[i] = nil
			decided[i] = true
			continue
		}
		_, _ = db.writerConn.ExecContext(frame, "ROLLBACK TO "+sp)
		_, _ = db.writerConn.ExecContext(frame, "RELEASE "+sp)
		switch classifyError(err) {
		case classConstraint, classOther:
			results[i] = fmt.Errorf("store: write command: %w", err)
			decided[i] = true
			db.addFailed(cmd.prio)
		case classBusy:
			aborted = true
			for j := i; j < len(batch); j++ {
				if !decided[j] {
					results[j] = ErrBusy
					decided[j] = true
				}
			}
			db.addBusyError()
		case classFull:
			aborted = true
			db.noteDiskFull()
			for j := i; j < len(batch); j++ {
				if !decided[j] {
					results[j] = ErrDiskFull
					decided[j] = true
				}
			}
			db.addFullError()
		}
		if aborted {
			break
		}
	}

	if !aborted {
		if _, err := db.writerConn.ExecContext(frame, "COMMIT"); err != nil {
			switch classifyError(err) {
			case classBusy:
				for i := range batch {
					results[i] = ErrBusy
					decided[i] = true
				}
				db.addBusyError()
			case classFull:
				db.noteDiskFull()
				for i := range batch {
					results[i] = ErrDiskFull
					decided[i] = true
				}
				db.addFullError()
			default:
				cerr := fmt.Errorf("store: commit: %w", err)
				for i := range batch {
					results[i] = cerr
					decided[i] = true
				}
			}
		} else {
			committed = true
			db.clearDiskFull()
			db.addBatch()
			for i, cmd := range batch {
				if results[i] == nil {
					db.addExecuted(cmd.prio)
				}
			}
		}
	}
	_ = aborted
	db.replyBatch(batch, results)
}

// replyBatch answers every waiter; results must be decided for all.
func (db *DB) replyBatch(batch []command, results []error) {
	for i, cmd := range batch {
		err := results[i]
		if err == nil && cmd.ctx.Err() != nil {
			err = ErrWriteCanceled
		}
		select {
		case cmd.resp <- err:
		default:
			// The waiter gave up; the outcome stands.
		}
	}
}

// beginImmediate acquires the write lock with bounded retries. The
// busy_timeout pragma already waited inside SQLite; retries cover an
// external holder past that ceiling, then a typed error goes back to
// every waiter with nothing applied (Section 4.3).
func (db *DB) beginImmediate(ctx context.Context) error {
	var err error
	for attempt := 0; ; attempt++ {
		_, err = db.writerConn.ExecContext(ctx, "BEGIN IMMEDIATE")
		if err == nil {
			return nil
		}
		switch classifyError(err) {
		case classBusy:
			if attempt >= db.opts.BusyRetries {
				return ErrBusy
			}
			db.addBusyRetry()
			if sleepErr := sleepJitter(ctx, attempt); sleepErr != nil {
				return ErrWriteCanceled
			}
		case classFull:
			db.noteDiskFull()
			return ErrDiskFull
		default:
			return fmt.Errorf("store: begin immediate: %w", err)
		}
	}
}

// sleepJitter waits with exponential backoff and jitter.
func sleepJitter(ctx context.Context, attempt int) error {
	base := 25 * time.Millisecond << attempt
	if base > 500*time.Millisecond {
		base = 500 * time.Millisecond
	}
	wait := base/2 + time.Duration(rand.Int64N(int64(base)))
	select {
	case <-time.After(wait):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// afterBatch checkpoints passively when idle and watches the WAL.
func (db *DB) afterBatch() {
	idle := true
	for i := range db.queues {
		if len(db.queues[i]) > 0 {
			idle = false
			break
		}
	}
	if idle {
		_ = db.checkpoint("PASSIVE")
	}
	db.watchWAL()
}

// checkpoint runs a WAL checkpoint outside any transaction.
func (db *DB) checkpoint(mode string) error {
	switch mode {
	case "PASSIVE", "FULL", "RESTART", "TRUNCATE":
	default:
		return fmt.Errorf("store: invalid checkpoint mode %q", mode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := db.writerConn.ExecContext(ctx, "PRAGMA wal_checkpoint("+mode+")")
	return err
}

// Checkpoint runs a WAL checkpoint (PASSIVE when idle, TRUNCATE in
// the daily maintenance window, Section 4.3).
func (db *DB) Checkpoint(ctx context.Context, mode string) error {
	switch mode {
	case "PASSIVE", "FULL", "RESTART", "TRUNCATE":
	default:
		return fmt.Errorf("store: invalid checkpoint mode %q", mode)
	}
	_, err := db.writerConn.ExecContext(ctx, "PRAGMA wal_checkpoint("+mode+")")
	return err
}

// WALBytes returns the current -wal file size, 0 when there is none.
func (db *DB) WALBytes() int64 {
	st, err := os.Stat(db.path + "-wal")
	if err != nil {
		return 0
	}
	return st.Size()
}

// CheckWAL returns the WAL size and whether it exceeds the alert
// threshold, firing OnWALAlert on the false->true transition.
func (db *DB) CheckWAL() (int64, bool) {
	size := db.WALBytes()
	over := size > db.opts.WALAlertBytes
	if over {
		if db.walAlerted.CompareAndSwap(false, true) {
			if fn := db.opts.OnWALAlert; fn != nil {
				fn(size)
			}
		}
	} else {
		db.walAlerted.Store(false)
	}
	return size, over
}

// watchWAL is the per-batch WAL watch.
func (db *DB) watchWAL() {
	db.CheckWAL()
}

// noteDiskFull enters disk-full mode and fires the callback on the
// false->true transition (Section 4.3: pause background, tell the
// master, keep the interactive path while space lasts).
func (db *DB) noteDiskFull() {
	db.diskFull.Store(true)
	if db.diskFullTx.CompareAndSwap(false, true) {
		if fn := db.opts.OnDiskFull; fn != nil {
			fn()
		}
	}
}

// clearDiskFull leaves disk-full mode after a clean commit.
func (db *DB) clearDiskFull() {
	if db.diskFull.CompareAndSwap(true, false) {
		db.diskFullTx.Store(false)
	}
}

// defaultWriterPanic is the critical-writer policy of Sections 3.2
// and 4.3: log and exit so the supervisor restarts the daemon.
func defaultWriterPanic(owner string, recovered any, stack []byte) {
	slog.Error("store writer panicked; exiting for supervisor restart",
		"owner", owner, "panic", recovered)
	_, _ = os.Stderr.Write(stack)
	os.Exit(1)
}
