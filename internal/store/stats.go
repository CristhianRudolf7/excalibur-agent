package store

import "time"

// Porter statistics helpers. All counters live behind statsMu; the
// hot path (per command) takes one short lock hold.
func (db *DB) addExecuted(p Priority) {
	db.statsMu.Lock()
	defer db.statsMu.Unlock()
	db.stats.Executed[p]++
}

func (db *DB) addFailed(p Priority) {
	db.statsMu.Lock()
	defer db.statsMu.Unlock()
	db.stats.Failed[p]++
}

func (db *DB) addCanceled(p Priority) {
	db.statsMu.Lock()
	defer db.statsMu.Unlock()
	db.stats.Canceled++
	_ = p
}

func (db *DB) addBatch() {
	db.statsMu.Lock()
	defer db.statsMu.Unlock()
	db.stats.Batches++
}

func (db *DB) addBusyRetry() {
	db.statsMu.Lock()
	defer db.statsMu.Unlock()
	db.stats.BusyRetries++
}

func (db *DB) addBusyError() {
	db.statsMu.Lock()
	defer db.statsMu.Unlock()
	db.stats.BusyErrors++
}

func (db *DB) addFullError() {
	db.statsMu.Lock()
	defer db.statsMu.Unlock()
	db.stats.FullErrors++
}

func (db *DB) addQueueWait(p Priority, d time.Duration) {
	db.statsMu.Lock()
	defer db.statsMu.Unlock()
	db.stats.QueueWait[p] += d
}

func (db *DB) addExecTime(p Priority, d time.Duration) {
	db.statsMu.Lock()
	defer db.statsMu.Unlock()
	db.stats.ExecTime[p] += d
}
