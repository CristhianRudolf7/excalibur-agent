//go:build darwin

package envsnap

import (
	"context"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// listeningPorts lists visible local TCP listeners. Like the Linux reader,
// discovery is best effort and reports at most twenty sorted tcp:port entries.
func listeningPorts() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, _ := exec.CommandContext(ctx, "/usr/sbin/lsof", "-nP", "-a", "-iTCP", "-sTCP:LISTEN", "-Fn").Output()
	return parseLsofListeningPorts(string(raw))
}

func parseLsofListeningPorts(raw string) []string {
	seen := map[uint64]bool{}
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(line, "n") {
			continue
		}
		addr := line[1:]
		// A listener has one endpoint. Ignore connected sockets if presented.
		if strings.Contains(addr, "->") {
			continue
		}
		i := strings.LastIndexByte(addr, ':')
		if i <= 0 {
			continue
		}
		port, err := strconv.ParseUint(addr[i+1:], 10, 16)
		if err != nil {
			continue
		}
		seen[port] = true
	}
	var out []string
	for port := range seen {
		out = append(out, "tcp:"+strconv.FormatUint(port, 10))
	}
	sort.Strings(out)
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}
