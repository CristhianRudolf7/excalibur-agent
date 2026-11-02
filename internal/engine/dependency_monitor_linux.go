//go:build linux

package engine

import (
	"encoding/binary"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"golang.org/x/sys/unix"
)

// Observe actual workspace reads, including dynamically constructed paths.
// Monitors are call-scoped and stop before hashing, so receipt hashing cannot
// observe itself. Concurrent checks may conservatively share read dependencies.
type dependencyMonitor struct {
	fd       int
	dirs     map[int]string
	reads    map[string]bool
	stop     chan struct{}
	done     chan struct{}
	once     sync.Once
	complete bool
}

func startDependencyMonitor(dir string) *dependencyMonitor {
	if dir == "" || !switchOn("EXCALIBUR_EVIDENCE_DEPENDENCIES") {
		return nil
	}
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		return nil
	}
	m := &dependencyMonitor{fd: fd, dirs: map[int]string{}, reads: map[string]bool{}, stop: make(chan struct{}), done: make(chan struct{}), complete: true}
	m.watchTree(dir)
	if len(m.dirs) == 0 {
		_ = unix.Close(fd)
		return nil
	}
	safego.Go("engine/check-dependencies", nil, func() {
		defer close(m.done)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-m.stop:
				m.drain()
				return
			case <-ticker.C:
				m.drain()
			}
		}
	})
	return m
}

func (m *dependencyMonitor) watchTree(dir string) {
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if len(m.dirs) >= 8192 {
			return fs.SkipAll
		}
		wd, err := unix.InotifyAddWatch(m.fd, path, unix.IN_ACCESS|unix.IN_CREATE|unix.IN_MOVED_TO|unix.IN_ONLYDIR)
		if err != nil {
			return err
		}
		m.dirs[wd] = path
		return nil
	})
	if err != nil || len(m.dirs) >= 8192 {
		m.complete = false
	}
}

func (m *dependencyMonitor) drain() {
	var buffer [64 * 1024]byte
	for batches := 0; batches < 256; batches++ {
		n, err := unix.Read(m.fd, buffer[:])
		if err == unix.EAGAIN {
			return
		}
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			m.complete = false
			return
		}
		if n == 0 {
			return
		}
		for at := 0; at+16 <= n; {
			wd := int(int32(binary.NativeEndian.Uint32(buffer[at:])))
			mask := binary.NativeEndian.Uint32(buffer[at+4:])
			size := int(binary.NativeEndian.Uint32(buffer[at+12:]))
			end := at + 16 + size
			if end > n {
				m.complete = false
				return
			}
			name := strings.TrimRight(string(buffer[at+16:end]), "\x00")
			if mask&unix.IN_Q_OVERFLOW != 0 {
				m.complete = false
			}
			if dir, ok := m.dirs[wd]; ok && name != "" {
				path := filepath.Join(dir, name)
				if mask&unix.IN_ISDIR != 0 {
					if mask&(unix.IN_CREATE|unix.IN_MOVED_TO) != 0 {
						m.watchTree(path)
					}
				} else if mask&unix.IN_ACCESS != 0 {
					if len(m.reads) < 8192 {
						m.reads[path] = true
					} else {
						m.complete = false
					}
				}
			}
			at = end
		}
	}
	m.complete = false
}

func (m *dependencyMonitor) finish() (paths []string, complete bool) {
	if m == nil {
		return nil, false
	}
	m.once.Do(func() { close(m.stop); <-m.done; _ = unix.Close(m.fd) })
	for path := range m.reads {
		paths = append(paths, path)
	}
	return paths, m.complete
}
