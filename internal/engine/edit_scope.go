package engine

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

const editScopeSwitch = "EXCALIBUR_EDIT_SCOPE_AUDIT"

const (
	editScopeFileBytes = 256 << 10
	editScopeReadBytes = 4 << 20
	editScopeMaxFiles  = 32
	editScopeMaxLines  = 20
	editScopeFileLines = 8192
)

var substitutionRequest = regexp.MustCompile(`(?i)\b(?:replac(?:e|ing)|renam(?:e|ing)|substitut(?:e|ing)|redact(?:ing)?|sanitiz(?:e|ing))\b`)

// Literal pairs improve precision for values that intentionally contain spaces,
// quotes or escapes. Unspecified replacements use structural comparison only;
// this advisory does not try to infer the semantic meaning of arbitrary prose.
var substitutionPair = regexp.MustCompile("(?i)\\b(?:replace|rename|substitute)\\s+(?:all occurrences of\\s+)?(`[^`\\n]+`|\"[^\"\\n]+\"|'[^'\\n]+'|[^\\s]+)\\s+(?:with|to|by)\\s+(`[^`\\n]+`|\"[^\"\\n]+\"|'[^'\\n]+'|[^\\s.,;]+)")

type editScopeEvidence struct {
	diff, note string
}

// Only autonomous exec/eval runs get this additional evidence. Assistant
// workers retain their existing edit and completion policies. Reuse the input
// manifest rather than taking another copy or reading repository history.
func (e *Engine) editScopeAudit(run *Run, request string) []editScopeEvidence {
	if !switchOn(editScopeSwitch) || !e.explicitFinishRequired(run) || !substitutionRequest.MatchString(request) {
		return nil
	}
	snap, _, _ := run.finishRunView()
	if snap == nil {
		return nil
	}
	remaining := int64(editScopeReadBytes)
	var out []editScopeEvidence
	for _, path := range sortedKeys(snap) {
		entry := snap.Entries[path]
		if entry.OriginalCopy == "" || entry.Size > editScopeFileBytes || entry.Size < 0 {
			continue
		}
		before, ok := readEditScopeFile(entry.OriginalCopy, &remaining)
		if !ok {
			continue
		}
		after, ok := readEditScopeFile(path, &remaining)
		if !ok || before == after {
			continue
		}
		out = append(out, summarizeEditScope(path, before, after, request))
		if len(out) >= editScopeMaxFiles || remaining <= 0 {
			break
		}
	}
	return out
}

func readEditScopeFile(path string, remaining *int64) (string, bool) {
	// Do not follow a file replaced by a symlink or open pipes/devices.
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > editScopeFileBytes || info.Size()+1 > *remaining {
		return "", false
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, editScopeFileBytes+1))
	*remaining -= int64(len(data)) + 1
	if err != nil || len(data) > editScopeFileBytes || bytes.IndexByte(data, 0) >= 0 {
		return "", false
	}
	if bytes.Count(data, []byte{'\n'}) >= editScopeFileLines {
		return "", false
	}
	return string(data), true
}

func editScopeNormalizer(request string) *strings.Replacer {
	var pairs []string
	for _, match := range substitutionPair.FindAllStringSubmatch(request, 32) {
		for _, value := range match[1:] {
			if len(value) >= 2 && strings.ContainsRune("`\"'", rune(value[0])) && value[len(value)-1] == value[0] {
				value = value[1 : len(value)-1]
			}
			if value != "" {
				pairs = append(pairs, value, "\x00")
			}
		}
	}
	return strings.NewReplacer(pairs...)
}

// No source values are emitted: an audit of sanitization must not copy the
// values being removed into evidence. Summaries show changed hunk locations and
// structural differences, with no unchanged context and at most 20 lines/file.
func summarizeEditScope(path, before, after, request string) editScopeEvidence {
	oldLines, newLines := strings.SplitAfter(before, "\n"), strings.SplitAfter(after, "\n")
	positions := make(map[string][]int)
	for i, line := range newLines {
		positions[line] = append(positions[line], i)
	}
	normalize := editScopeNormalizer(request)
	var b strings.Builder
	fmt.Fprintf(&b, "edit scope %s (values omitted):\n", path)
	lines, firstExample := 1, ""
	for i, j := 0, 0; i < len(oldLines) || j < len(newLines); {
		if i < len(oldLines) && j < len(newLines) && oldLines[i] == newLines[j] {
			i++
			j++
			continue
		}
		startI, startJ := i, j
		// Align on the nearest unchanged line. The position index avoids a
		// quadratic line-pair table, including for long inserted/deleted blocks.
		endI, endJ := len(oldLines), len(newLines)
		best := len(oldLines) + len(newLines)
		for a := i; a < len(oldLines) && a-i < best; a++ {
			matches := positions[oldLines[a]]
			at := sort.SearchInts(matches, j)
			if at < len(matches) && a-i+matches[at]-j < best {
				endI, endJ, best = a, matches[at], a-i+matches[at]-j
			}
		}
		old := normalize.Replace(strings.Join(oldLines[startI:endI], ""))
		new := normalize.Replace(strings.Join(newLines[startJ:endJ], ""))
		example := editScopeDifference(old, new)
		if firstExample == "" && example != "" {
			firstExample = fmt.Sprintf("line %d: %s", startJ+1, example)
		}
		if example == "" {
			example = "value spans changed"
		}
		if lines < editScopeMaxLines {
			fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@ %s\n", startI+1, endI-startI, startJ+1, endJ-startJ, example)
			lines++
		}
		i, j = endI, endJ
	}
	result := editScopeEvidence{diff: b.String()}
	if firstExample != "" {
		result.note = "edit changed text beyond the replaced values in " + path + ": " + firstExample
	}
	return result
}

func editScopeDifference(before, after string) string {
	if before == after {
		return ""
	}
	// Explicit pairs have been replaced with a sentinel. Any residual change
	// around those spans is outside the requested values, including key order.
	if strings.ContainsRune(before, 0) || strings.ContainsRune(after, 0) {
		return "text outside requested value spans changed (values omitted)"
	}
	structure := func(s string, chars string) string {
		var b strings.Builder
		for _, r := range s {
			if strings.ContainsRune(chars, r) {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	if structure(before, "'\"`") != structure(after, "'\"`") {
		return "quoting changed (values omitted)"
	}
	if structure(before, "\\") != structure(after, "\\") {
		return "escaping changed (values omitted)"
	}
	if structure(before, " \t\r\n") != structure(after, " \t\r\n") {
		return "whitespace or line layout changed (values omitted)"
	}
	// Preserve the order and spelling of line keys when values were not named
	// literally. This handles config rewrites without guessing value contents.
	keys := func(s string) string {
		var b strings.Builder
		for _, line := range strings.Split(s, "\n") {
			if at := strings.IndexAny(line, "=:"); at >= 0 {
				b.WriteString(line[:at+1])
				b.WriteByte('\n')
			}
		}
		return b.String()
	}
	if keys(before) != keys(after) {
		return "surrounding keys or their order changed (values omitted)"
	}
	return ""
}

func appendEditScopeEvidence(small *tasks.VerifierSmallContext, audit []editScopeEvidence) {
	for _, item := range audit {
		small.Diff += "\n" + item.diff
		if item.note != "" {
			small.Diff += item.note + "\n"
		}
	}
}
