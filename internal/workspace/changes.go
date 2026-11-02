package workspace

import (
	"bytes"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

// Changes collapses recorded writes for trusted run IDs, in recording order.
// Run identity, rather than a wall-clock interval, isolates concurrent turns.
// The first preimage and last postimage define the net change, without reading
// the working tree (which may already contain another turn's writes).
func (h *History) Changes(runIDs, roots []string) []api.FileChange {
	runs := make(map[string]bool, len(runIDs))
	for _, id := range runIDs {
		if id != "" {
			runs[id] = true
		}
	}
	type images struct {
		pre, post        []byte
		created, deleted bool
	}
	files := map[string]images{}
	h.mu.Lock()
	for _, v := range h.vers {
		if !runs[v.RunID] {
			continue
		}
		pair, exists := files[v.Path]
		if !exists {
			pair.pre = h.blobs[v.Preimage]
			pair.created = v.Created
		}
		pair.post = h.blobs[v.Postimage]
		pair.deleted = v.Deleted
		files[v.Path] = pair
	}
	h.mu.Unlock()
	var out []api.FileChange
	for path, pair := range files {
		if bytes.Equal(pair.pre, pair.post) && pair.created == pair.deleted {
			continue
		}
		if pair.created && pair.deleted {
			continue
		}
		c := api.FileChange{Path: changePath(path, roots), Status: "modified"}
		if pair.created {
			c.Status = "added"
		}
		if pair.deleted {
			c.Status = "deleted"
		}
		if !IsText(pair.pre) || !IsText(pair.post) {
			c.Binary = true
			c.Hunks = []api.FileHunk{}
			out = append(out, c)
			continue
		}
		c.Hunks, c.Added, c.Removed, c.Truncated = changeHunks(pair.pre, pair.post)
		if !pair.deleted {
			if len(pair.post) <= 200*1024 {
				content := string(pair.post)
				c.Content = &content
			} else {
				c.Truncated = true
			}
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	if len(out) > 50 {
		out = out[:50]
	}
	return out
}

func changePath(path string, roots []string) string {
	best := ""
	for _, root := range roots {
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && len(root) > len(best) {
			best = root
		}
	}
	if best != "" {
		path, _ = filepath.Rel(best, path)
	}
	return filepath.ToSlash(path)
}

func changeLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	lines := strings.SplitAfter(string(data), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// Reuse the bounded history LCS. For large inputs its prefix/suffix fallback
// describes a valid middle replacement without allocating a quadratic table.
func changeHunks(pre, post []byte) ([]api.FileHunk, int, int, bool) {
	base, next := changeLines(pre), changeLines(post)
	type line struct {
		text     string
		old, new int
		tag      byte
	}
	var lines []line
	old, newLine, added, removed := 1, 1, 0, 0
	for _, op := range diffToBase(base, next) {
		if op.tag == 'e' {
			for _, s := range op.lines {
				lines = append(lines, line{s, old, newLine, ' '})
				old++
				newLine++
			}
		} else {
			for _, s := range base[op.base0:op.base1] {
				lines = append(lines, line{s, old, newLine, '-'})
				old++
				removed++
			}
			for _, s := range op.lines {
				lines = append(lines, line{s, old, newLine, '+'})
				newLine++
				added++
			}
		}
	}
	hunks := []api.FileHunk{}
	used := 0
	truncated := false
	for i := 0; i < len(lines); {
		if lines[i].tag == ' ' {
			i++
			continue
		}
		start, end := max(0, i-3), i+1
		// Merge changes separated by at most six context lines.
		last := i
		for j := i + 1; j < len(lines) && j <= last+7; j++ {
			if lines[j].tag != ' ' {
				last = j
				end = j + 1
			}
		}
		end = min(len(lines), end+3)
		if len(hunks) == 40 {
			truncated = true
			break
		}
		hunk := api.FileHunk{OldStart: lines[start].old, NewStart: lines[start].new, Lines: []string{}}
		for _, l := range lines[start:end] {
			text := string(l.tag) + strings.TrimSuffix(l.text, "\n")
			if used+len(text) > 200*1024 {
				truncated = true
				break
			}
			used += len(text)
			hunk.Lines = append(hunk.Lines, text)
			if l.tag != '+' {
				hunk.OldLines++
			}
			if l.tag != '-' {
				hunk.NewLines++
			}
		}
		if hunk.OldLines == 0 {
			hunk.OldStart--
		}
		if hunk.NewLines == 0 {
			hunk.NewStart--
		}
		if len(hunk.Lines) > 0 {
			hunks = append(hunks, hunk)
		}
		if truncated {
			break
		}
		i = end
	}
	return hunks, added, removed, truncated
}

// IsText accepts UTF-8 without NUL bytes in the first 8 KiB.
func IsText(data []byte) bool {
	return utf8.Valid(data) && !bytes.ContainsRune(data[:min(len(data), 8192)], 0)
}
