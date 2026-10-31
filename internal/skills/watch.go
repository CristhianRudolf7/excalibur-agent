package skills

import (
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// RescanInterval is the fallback re-exploration period (Section
// 12.2: the OpenClaw scheme of a filesystem watcher plus a
// re-scan every 30 minutes).
const RescanInterval = 30 * time.Minute

// debounceDelay coalesces bursts of filesystem events into one
// reindex.
const debounceDelay = 500 * time.Millisecond

// Watcher keeps the index fresh: an fsnotify watch over every
// configured root (plus their skill directories, one level deep)
// and a fallback full rescan every interval (Section 12.2). A
// changed content hash invalidates the entry, revalidates it and
// refreshes the lexical state inside Scan. The watcher's goroutine
// runs under safego.Go so a panic marks the owner instead of
// killing the daemon.
type Watcher struct {
	ix       *Index
	w        *fsnotify.Watcher
	interval time.Duration
	onScan   func(err error)
	stop     chan struct{}
	done     chan struct{}
}

// Watch starts a Watcher over the index roots and performs the
// initial scan. onScan, when non-nil, runs after every scan,
// including the initial one, with the scan error (nil on
// success).
func (ix *Index) Watch(interval time.Duration, onScan func(error)) (*Watcher, error) {
	if interval <= 0 {
		interval = RescanInterval
	}
	if err := ix.Scan(); err != nil {
		return nil, err
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	wch := &Watcher{ix: ix, w: w, interval: interval, onScan: onScan, stop: make(chan struct{}), done: make(chan struct{})}
	for _, r := range ix.roots.OrderedRoots() {
		wch.addTree(r.Dir)
	}
	if onScan != nil {
		onScan(nil)
	}
	safego.Go("skills/watch", nil, wch.loop)
	return wch, nil
}

// addTree watches dir and its immediate child directories: skill
// content (SKILL.md, scripts, references) lives one level below
// the root.
func (w *Watcher) addTree(dir string) {
	if dir == "" {
		return
	}
	_ = w.w.Add(dir)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.IsDir() {
			_ = w.w.Add(filepath.Join(dir, e.Name()))
		}
	}
}

// Roots returns the watched filesystem roots.
func (w *Watcher) Roots() []string {
	var out []string
	for _, r := range w.ix.roots.OrderedRoots() {
		if r.Dir != "" {
			out = append(out, r.Dir)
		}
	}
	return out
}

func (w *Watcher) loop() {
	defer close(w.done)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	var timer *time.Timer
	var timerC <-chan time.Time
	arm := func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.NewTimer(debounceDelay)
		timerC = timer.C
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-w.stop:
			return
		case ev, ok := <-w.w.Events:
			if !ok {
				return
			}
			// A new skill directory needs its own watch.
			if ev.Op&(fsnotify.Create) != 0 {
				if st, err := os.Stat(ev.Name); err == nil && st.IsDir() {
					_ = w.w.Add(ev.Name)
				}
			}
			arm()
		case err, ok := <-w.w.Errors:
			if !ok {
				return
			}
			_ = err
			arm()
		case <-timerC:
			timerC = nil
			w.rescan()
		case <-ticker.C:
			w.rescan()
		}
	}
}

func (w *Watcher) rescan() {
	err := w.ix.Scan()
	if w.onScan != nil {
		func() {
			defer func() { _ = recover() }()
			w.onScan(err)
		}()
	}
}

// Close stops the watcher and waits for its loop to finish.
func (w *Watcher) Close() error {
	close(w.stop)
	<-w.done
	return w.w.Close()
}
