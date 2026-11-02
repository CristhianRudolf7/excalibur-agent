package tools

import (
	"net"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// procInfo is one process seen in the process table.
type procInfo struct {
	PID, PPID, PGID int
	Comm            string
	Argv            string
}

// processTable is a snapshot of the process table.
type processTable []procInfo

// tree returns the processes that belong to a session rooted at root:
// the root itself, every member of its process group and every
// descendant of those. Sessions start in their own group, so the group
// catches children reparented after their shell exits.
func (t processTable) tree(root int) []procInfo {
	if root <= 0 {
		return nil
	}
	member := map[int]bool{root: true}
	for _, p := range t {
		if p.PGID == root {
			member[p.PID] = true
		}
	}
	// Grow the set until no process with a member parent is left out.
	for changed := true; changed; {
		changed = false
		for _, p := range t {
			if !member[p.PID] && member[p.PPID] {
				member[p.PID] = true
				changed = true
			}
		}
	}
	var out []procInfo
	for _, p := range t {
		if member[p.PID] {
			out = append(out, p)
		}
	}
	return out
}

// ancestors returns the parent chain of pid, nearest first. It stops at
// init, at an unknown parent or at a cycle.
func (t processTable) ancestors(pid int) []procInfo {
	byPID := make(map[int]procInfo, len(t))
	for _, p := range t {
		byPID[p.PID] = p
	}
	var out []procInfo
	seen := map[int]bool{pid: true}
	cur, ok := byPID[pid]
	for ok && cur.PPID > 1 && !seen[cur.PPID] {
		parent, found := byPID[cur.PPID]
		if !found {
			break
		}
		seen[parent.PID] = true
		out = append(out, parent)
		cur = parent
	}
	return out
}

// Package managers and compilers that a finish gate must not outrun.
// Matching is on the process name and on the first word of the command
// line, so an interpreter started as `python3 -m pip` is covered too.
var (
	packageManagerNames = map[string]bool{"apt-get": true, "apt": true, "dpkg": true, "pip": true, "pip3": true, "npm": true, "cargo": true}
	buildNames          = map[string]bool{"make": true, "ninja": true, "cc1": true, "cc1plus": true, "gcc": true, "g++": true, "clang": true, "rustc": true}
)

// classifyProcess reports whether a process is a package manager or a
// build step (compiler, make, `cargo build`, `go build`).
func classifyProcess(p procInfo) (pkg, build bool) {
	fields := strings.Fields(p.Argv)
	base := ""
	if len(fields) > 0 {
		base = filepath.Base(fields[0])
	}
	for _, name := range []string{p.Comm, base} {
		if packageManagerNames[name] {
			pkg = true
		}
		if buildNames[name] {
			build = true
		}
	}
	switch {
	case (p.Comm == "go" || base == "go") && containsWord(fields, "build"):
		build = true
	case p.Comm == "cargo" && containsWord(fields, "build"):
		build = true
	case strings.HasPrefix(p.Comm, "python") && hasSequence(fields, "-m", "pip"):
		pkg = true
	}
	return pkg, build
}

func containsWord(fields []string, word string) bool {
	for _, f := range fields {
		if f == word {
			return true
		}
	}
	return false
}

func hasSequence(fields []string, a, b string) bool {
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == a && fields[i+1] == b {
			return true
		}
	}
	return false
}

// SessionInfo describes one terminal session for the finish gate and the
// trial records. The process flags come from the session's process tree
// and are only set while the session is alive.
type SessionInfo struct {
	ExitCode   *int      `json:"exit_code,omitempty"`
	ID         SessionID `json:"id"`
	Owner      string    `json:"owner"`
	Command    string    `json:"command"`
	PID        int       `json:"pid"`
	Alive      bool      `json:"alive"`
	Started    time.Time `json:"started"`
	LastOutput time.Time `json:"last_output"`
	Detached   bool      `json:"detached"`
	LogPath    string    `json:"log_path,omitempty"`
	// PackageManager is set when a package manager runs in the tree.
	PackageManager bool `json:"package_manager"`
	// Build is set when a compiler or build tool runs in the tree.
	Build bool `json:"build"`
	// TerminalWait is set when a live non-PTY session holds a terminal
	// device, the shape of a prompt waiting on /dev/tty.
	TerminalWait bool `json:"terminal_wait"`
}

// ActiveSessions lists every session this manager still tracks, in start
// order. Alive says whether its process is still running; the finish gate
// filters on it. Process flags are computed for live sessions only.
func (m *SessionManager) ActiveSessions() []SessionInfo {
	out := m.sessionInfos(func(*Session) bool { return true })
	if !switchDefaultOn("EXCALIBUR_SESSION_AUTHORITY") {
		return out
	}
	m.mu.Lock()
	for _, info := range m.tombstones {
		out = append(out, info)
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out
}

// ServiceSessions lists the detached background services (W10) this
// manager tracks, in start order, with the same fields as ActiveSessions.
func (m *SessionManager) ServiceSessions() []SessionInfo {
	return m.sessionInfos(func(s *Session) bool { return s.Detached })
}

func (m *SessionManager) sessionInfos(keep func(*Session) bool) []SessionInfo {
	var sessions []*Session
	m.mu.Lock()
	for _, sid := range m.order {
		if s, ok := m.sessions[sid]; ok && keep(s) {
			sessions = append(sessions, s)
		}
	}
	m.mu.Unlock()
	var table processTable
	for _, s := range sessions {
		if s.snapshot() {
			table = readProcessTable()
			break
		}
	}
	out := make([]SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		s.mu.Lock()
		info := SessionInfo{
			ID: s.ID, Owner: s.Owner, Command: s.Cmd, PID: s.pidLocked(),
			Alive: !s.exited, Started: s.Started, LastOutput: s.lastOutput,
			Detached: s.Detached, LogPath: s.LogPath,
		}
		s.mu.Unlock()
		if code, done := s.Exit(); done {
			info.ExitCode = &code
			info.Alive = false
		}
		if info.Alive && info.PID > 0 {
			for _, p := range table.tree(info.PID) {
				pkg, build := classifyProcess(p)
				info.PackageManager = info.PackageManager || pkg
				info.Build = info.Build || build
			}
			info.TerminalWait = !s.UsePTY && processHoldsTerminal(info.PID)
		}
		out = append(out, info)
	}
	return out
}

// ProbePort reports whether something accepts TCP connections on port at
// 127.0.0.1 or ::1, using a fresh connect each time. The finish gate uses
// it to check services the task declares.
func ProbePort(port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	for _, host := range []string{"127.0.0.1", "::1"} {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
}

// m.mu is held; only compact terminal facts survive eviction.
func (m *SessionManager) rememberSessionLocked(s *Session) {
	if !switchDefaultOn("EXCALIBUR_SESSION_AUTHORITY") {
		return
	}
	if m.tombstones == nil {
		m.tombstones = map[SessionID]SessionInfo{}
	}
	s.mu.Lock()
	info := SessionInfo{ID: s.ID, Owner: s.Owner, Command: s.Cmd, Started: s.Started, Alive: false}
	s.mu.Unlock()
	if code, done := s.Exit(); done {
		info.ExitCode = &code
	}
	m.tombstones[s.ID] = info
}
