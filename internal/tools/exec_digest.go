package tools

import (
	"fmt"
	"strings"
)

// execOutputDigest scans full output before clipping. Counts refer to full
// output, so a path found only in the middle remains discoverable.
func execOutputDigest(full string) string {
	if off("EXCALIBUR_EXEC_OUTPUT_DIGEST") {
		return ""
	}
	lines := strings.Split(strings.TrimSuffix(full, "\n"), "\n")
	counts := make(map[string]int)
	var paths []string
	for _, line := range lines {
		path := searchOutputPath(line)
		if path == "" {
			continue
		}
		if counts[path] == 0 {
			paths = append(paths, path)
		}
		counts[path]++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nFull output digest: %d lines", len(lines))
	if len(paths) > 0 {
		b.WriteString("; paths (hit counts):\n")
		for i, p := range paths {
			if i == 60 {
				fmt.Fprintf(&b, "+%d more\n", len(paths)-i)
				break
			}
			display := p
			if len(display) > 70 {
				display = display[:32] + "…" + display[len(display)-32:]
			}
			fmt.Fprintf(&b, "%s: %d\n", display, counts[p])
		}
	} else {
		b.WriteByte('\n')
	}
	return b.String()
}

func searchOutputPath(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	// rg and git grep use path:line:content or path:content. A path
	// prefix must be a filename rather than a diagnostic label.
	if i := strings.IndexByte(line, ':'); i > 0 {
		p := line[:i]
		if !strings.ContainsAny(p, " \t\r") {
			return p
		}
	}
	// find, rg --files and grep -l emit bare paths. Exclude prose and
	// shell headers, while accepting both relative and absolute paths.
	if !strings.ContainsAny(line, " \t\r:") && strings.ContainsAny(line, "/.") {
		return line
	}
	return ""
}
