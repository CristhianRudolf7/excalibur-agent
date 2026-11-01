package tools

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Edit diagnostics for edit_file (EXCALIBUR_EDIT_HINTS). A miss names
// the most similar lines so the next call can copy the real text; an
// ambiguous match lists every line it occurs on and accepts an
// occurrence index.

// exactOccurrences returns the byte offsets of the non-overlapping exact
// occurrences of sub in content: the places strings.Count counts.
func exactOccurrences(content, sub string) []int {
	if sub == "" {
		return nil
	}
	var offs []int
	for i := 0; i <= len(content); {
		j := strings.Index(content[i:], sub)
		if j < 0 {
			break
		}
		offs = append(offs, i+j)
		i += j + len(sub)
	}
	return offs
}

// lineNumbersAt maps byte offsets to 1-based line numbers.
func lineNumbersAt(content string, offs []int) []int {
	out := make([]int, 0, len(offs))
	for _, off := range offs {
		out = append(out, strings.Count(content[:off], "\n")+1)
	}
	return out
}

// joinLineNumbers renders line numbers as "4, 19, 40".
func joinLineNumbers(nums []int) string {
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

// locateOldText finds the one place old_text replaces in content. It
// returns the byte offset and length of that place, or a refusal text
// that tells the caller what to change. Exact matches are counted
// first; normalized and trimmed matches (FindMatch) apply only when no
// exact match exists, as before. With EXCALIBUR_EDIT_HINTS off the
// earlier wording and behaviour are kept and occurrence is ignored.
func locateOldText(content, path, oldText string, occurrence int) (int, int, string) {
	if !EditHintsEnabled() {
		off, _, amb := FindMatch(content, oldText)
		if off < 0 {
			if amb != nil {
				return -1, 0, fmt.Sprintf("multiple matches at lines %v; refine old_text", amb)
			}
			return -1, 0, fmt.Sprintf("old_text not found in %s (%d lines)", path, len(strings.Split(content, "\n")))
		}
		return off, len(matchedSlice(content, off, oldText)), ""
	}
	exact := exactOccurrences(content, oldText)
	if len(exact) > 0 {
		if occurrence > 0 {
			if occurrence > len(exact) {
				return -1, 0, fmt.Sprintf("occurrence %d is out of range: old_text matches %d place(s) in %s (lines %s)",
					occurrence, len(exact), path, joinLineNumbers(lineNumbersAt(content, exact)))
			}
			return exact[occurrence-1], len(oldText), ""
		}
		if len(exact) == 1 {
			return exact[0], len(oldText), ""
		}
		return -1, 0, multipleMatchMessage(path, len(exact), joinLineNumbers(lineNumbersAt(content, exact)))
	}
	off, _, amb := FindMatch(content, oldText)
	if off >= 0 {
		return off, len(matchedSlice(content, off, oldText)), ""
	}
	if len(amb) > 0 {
		return -1, 0, multipleMatchMessage(path, len(amb), joinLineNumbers(amb))
	}
	return -1, 0, zeroMatchMessage(path, content, oldText)
}

func multipleMatchMessage(path string, n int, lines string) string {
	return fmt.Sprintf("old_text matches %d places in %s (lines %s); add more surrounding context to old_text so it matches once, or pass occurrence (1-based) to edit one of them",
		n, path, lines)
}

// zeroMatchMessage reports a miss with the three lines most similar to
// old_text and their line numbers.
func zeroMatchMessage(path, content, oldText string) string {
	lines := strings.Split(content, "\n")
	near := nearestLines(lines, oldText, 3)
	if len(near) == 0 {
		return fmt.Sprintf("old_text not found in %s (%d lines); no line resembles it. Re-read the file and copy old_text exactly", path, len(lines))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "old_text not found in %s (%d lines); the most similar lines are:", path, len(lines))
	for _, i := range near {
		fmt.Fprintf(&b, "\n  line %d: %s", i+1, clipText(lines[i], 160))
	}
	b.WriteString("\nre-read those lines and copy old_text exactly")
	return b.String()
}

// nearestLines ranks lines by bigram similarity to the most distinctive
// (longest) non-blank line of target and returns the 0-based indices of
// the best k lines that share any bigram, best first, ties by position.
func nearestLines(lines []string, target string, k int) []int {
	probe := ""
	for _, l := range strings.Split(target, "\n") {
		if t := strings.TrimSpace(l); len(t) > len(probe) {
			probe = t
		}
	}
	if probe == "" || k <= 0 {
		return nil
	}
	want := bigramCounts(strings.ToLower(probe))
	wantTotal := bigramTotal(want)
	type scored struct {
		idx   int
		score float64
	}
	var hits []scored
	for i, l := range lines {
		t := strings.ToLower(strings.TrimSpace(l))
		if t == "" {
			continue
		}
		got := bigramCounts(t)
		overlap := bigramOverlap(want, got)
		if overlap == 0 {
			continue
		}
		hits = append(hits, scored{idx: i, score: 2 * float64(overlap) / float64(wantTotal+bigramTotal(got))})
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].score != hits[b].score {
			return hits[a].score > hits[b].score
		}
		return hits[a].idx < hits[b].idx
	})
	if len(hits) > k {
		hits = hits[:k]
	}
	out := make([]int, len(hits))
	for i, h := range hits {
		out[i] = h.idx
	}
	return out
}

func bigramCounts(s string) map[string]int {
	m := map[string]int{}
	for i := 0; i+1 < len(s); i++ {
		m[s[i:i+2]]++
	}
	return m
}

func bigramTotal(m map[string]int) int {
	n := 0
	for _, c := range m {
		n += c
	}
	return n
}

func bigramOverlap(a, b map[string]int) int {
	n := 0
	for k, c := range a {
		if d, ok := b[k]; ok {
			if d < c {
				n += d
			} else {
				n += c
			}
		}
	}
	return n
}

// clipText shortens s to n runes for one-line diagnostics.
func clipText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
