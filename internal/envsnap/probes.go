package envsnap

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// detectDistro reads PRETTY_NAME from /etc/os-release (Linux). Empty
// elsewhere or when unreadable: absence, not an error.
func detectDistro() string {
	if distro, ok := platformDistro(); ok {
		return distro
	}
	raw, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

// probeVersion runs "<bin> --version" (falling back to "version" for
// Go-style tools) with a short timeout and returns the first line,
// truncated. Empty means present but version unknown.
func probeVersion(parent context.Context, timeout time.Duration, bin string) string {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	for _, args := range [][]string{{"--version"}, {"version"}, {"-version"}} {
		out := runCapture(ctx, bin, args...)
		if out == "" {
			continue
		}
		first := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
		if len(first) > 80 {
			first = first[:80]
		}
		if first != "" {
			return first
		}
	}
	return ""
}

// runCapture runs bin with args and returns combined output, or empty
// on any failure (absence, timeout, non-zero exit with no output).
func runCapture(ctx context.Context, bin string, args ...string) string {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = probeEnvironment()
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	return string(out)
}

// probeNetwork dials host (host:port) with a 1.5s timeout. Empty host
// skips the dial (unknown). Success is open; DNS or dial failure is
// restricted, with the reason as detail. No payload is ever sent and
// no paid API is called.
func probeNetwork(ctx context.Context, host string) (NetworkMode, string) {
	host = strings.TrimSpace(host)
	if host == "" {
		return NetworkUnknown, "no probe host configured"
	}
	if !strings.Contains(host, ":") {
		host += ":443"
	}
	d := net.Dialer{Timeout: 1500 * time.Millisecond}
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return NetworkRestricted, "dial " + host + ": " + shortErr(err)
	}
	_ = conn.Close()
	return NetworkOpen, "dial " + host + " ok"
}

func shortErr(err error) string {
	s := err.Error()
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// heavyDirs are collapsed to entry counts in the tree summary.
var heavyDirs = map[string]bool{
	"node_modules": true, ".git": true, "target": true,
	"__pycache__": true, ".venv": true, "venv": true,
	".hg": true, ".svn": true, "dist": true, "build": true,
	".cache": true, ".next": true, ".tox": true,
}

type treeResult struct {
	text      string
	truncated bool
}

// collectTree summarizes workdir: sorted top-level entries with kinds,
// one entry-count line per subdirectory at depth 2, heavy directories
// collapsed without descending. Bounded by maxEntries; errors yield a
// short note, never a failure.
func collectTree(ctx context.Context, workdir string, maxEntries, maxDepth int) treeResult {
	if strings.TrimSpace(workdir) == "" {
		return treeResult{text: "(no workdir)"}
	}
	entries, err := os.ReadDir(workdir)
	if err != nil {
		return treeResult{text: "(unreadable: " + shortErr(err) + ")"}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var b strings.Builder
	count := 0
	truncated := false
	write := func(s string) {
		if count >= maxEntries {
			truncated = true
			return
		}
		b.WriteString(s)
		count++
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			truncated = true
			break
		}
		name := e.Name()
		if e.IsDir() {
			if heavyDirs[name] {
				n := countDirEntries(filepath.Join(workdir, name))
				write("dir " + name + "/ (collapsed, " + itoa(n) + " entries)\n")
				continue
			}
			if maxDepth >= 2 {
				n := countDirEntries(filepath.Join(workdir, name))
				write("dir " + name + "/ (" + itoa(n) + " entries)\n")
			} else {
				write("dir " + name + "/\n")
			}
		} else {
			write("file " + name + "\n")
		}
	}
	if truncated {
		b.WriteString("... (truncated to " + itoa(maxEntries) + " entries)\n")
	}
	if count == 0 && !truncated {
		return treeResult{text: "(empty directory)"}
	}
	return treeResult{text: b.String(), truncated: truncated}
}

// countDirEntries counts direct children, capped for speed.
func countDirEntries(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return -1
	}
	if len(entries) > 999 {
		return 999
	}
	return len(entries)
}

func itoa(n int) string {
	if n < 0 {
		return "?"
	}
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
