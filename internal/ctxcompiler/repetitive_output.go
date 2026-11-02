package ctxcompiler

import (
	"fmt"
	"regexp"
	"strings"
)

const repetitiveScanBytes = 2 << 20
const repetitiveScanLines = 20000

var repetitiveNumbers = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)
var repetitiveBars = regexp.MustCompile(`[=■█▓░#>.-]{3,}`)
var repetitiveANSI = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")
var diagnosticLine = regexp.MustCompile(`(?i)\b(error|warning|warn|fatal|failed|failure|exception|traceback)\b`)

// CollapseRepetitiveOutput changes only model observations. Durable history and
// blobs retain their original bytes. Work and allocations are bounded per call.
func CollapseRepetitiveOutput(output string) string {
	if !envFlagDefaultOn(nil, "EXCALIBUR_COLLAPSE_PROGRESS") || len(output) < 512 {
		return output
	}
	limit := min(len(output), repetitiveScanBytes)
	if limit < len(output) {
		if n := strings.LastIndexByte(output[:limit], '\n'); n >= 0 {
			limit = n + 1
		} else {
			return output
		}
	}
	prefix := output[:limit]
	lines := strings.SplitAfter(prefix, "\n")
	if len(lines) > repetitiveScanLines {
		lines = lines[:repetitiveScanLines]
		limit = 0
		for _, line := range lines {
			limit += len(line)
		}
	}
	// Carriage-return progress updates are separate observations within a line.
	// Keep their exact bytes when retained.
	var pieces []string
	for _, line := range lines {
		for len(line) > 0 {
			i := strings.IndexByte(line, '\r')
			if i < 0 {
				pieces = append(pieces, line)
				break
			}
			pieces = append(pieces, line[:i+1])
			line = line[i+1:]
		}
	}
	if len(pieces) > repetitiveScanLines {
		return output
	}
	signature := func(line string) string {
		s := repetitiveANSI.ReplaceAllString(line, "")
		s = repetitiveNumbers.ReplaceAllString(s, "#")
		s = repetitiveBars.ReplaceAllString(s, "<bar>")
		return strings.Join(strings.Fields(s), " ")
	}
	var b strings.Builder
	collapsed := false
	for i := 0; i < len(pieces); {
		key := signature(pieces[i])
		j := i + 1
		for j < len(pieces) && signature(pieces[j]) == key {
			j++
		}
		if j-i < 8 || key == "" {
			for _, line := range pieces[i:j] {
				b.WriteString(line)
			}
			i = j
			continue
		}
		omitted := 0
		flush := func() {
			if omitted > 0 {
				fmt.Fprintf(&b, "[collapsed %d repetitive lines; full output retained in tool log/blob]\n", omitted)
				omitted = 0
				collapsed = true
			}
		}
		for k := i; k < j; k++ {
			if k < i+2 || k >= j-2 || diagnosticLine.MatchString(pieces[k]) {
				flush()
				b.WriteString(pieces[k])
			} else {
				omitted++
			}
		}
		flush()
		i = j
	}
	if !collapsed {
		return output
	}
	b.WriteString(output[limit:])
	return b.String()
}
