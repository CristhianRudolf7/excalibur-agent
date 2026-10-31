//go:build linux

package envsnap

import (
	"os"
	"sort"
	"strconv"
	"strings"
)

// listeningPorts lists local TCP ports in LISTEN state (0A) from
// /proc/net/tcp and tcp6. Best effort: unreadable tables yield nil.
func listeningPorts() []string {
	seen := map[uint64]bool{}
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n")[1:] {
			f := strings.Fields(line)
			if len(f) < 4 || f[3] != "0A" {
				continue
			}
			addr := f[1]
			i := strings.LastIndex(addr, ":")
			if i < 0 {
				continue
			}
			p, err := strconv.ParseUint(addr[i+1:], 16, 16)
			if err != nil {
				continue
			}
			seen[p] = true
		}
	}
	var out []string
	for p := range seen {
		out = append(out, "tcp:"+strconv.FormatUint(p, 10))
	}
	sort.Strings(out)
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}
