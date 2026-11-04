package store

import (
	"bufio"
	"strings"
)

// networkFSTypes degrades to journal_mode=DELETE on these filesystem
// types (Section 4.3; Codex forces DELETE on NFS for the same reason).
var networkFSTypes = map[string]bool{
	"nfs":        true,
	"nfs4":       true,
	"cifs":       true,
	"smb":        true,
	"smbfs":      true,
	"sshfs":      true,
	"fuse.sshfs": true,
	"9p":         true,
	"vboxsf":     true,
}

// mountEntry is one parsed /proc/mounts line.
type mountEntry struct {
	mountPoint string
	fsType     string
}

// parseMounts parses /proc/mounts-style content.
func parseMounts(content string) []mountEntry {
	var out []mountEntry
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		out = append(out, mountEntry{mountPoint: fields[1], fsType: fields[2]})
	}
	return out
}

// mountForDir returns the entry with the longest mount-point prefix of
// dir, or nil when mounts is empty.
func mountForDir(mounts []mountEntry, dir string) *mountEntry {
	var best *mountEntry
	bestLen := -1
	for i, m := range mounts {
		mp := m.mountPoint
		if dir == mp || strings.HasPrefix(dir, mp+"/") || mp == "/" {
			if len(mp) > bestLen {
				bestLen = len(mp)
				best = &mounts[i]
			}
		}
	}
	return best
}

// detectNetworkFS selects the native filesystem probe. Failure to determine the
// filesystem type never prevents opening a local store (Section 4.3).
func detectNetworkFS(dir string) bool { return detectPlatformNetworkFS(dir) }
