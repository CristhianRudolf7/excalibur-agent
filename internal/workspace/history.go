package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// FileVersion is one recorded agent write: path, preimage and
// postimage content refs, the producing run and tool call, and the
// time (Appendix A file_versions, Section 13.7).
type FileVersion struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Preimage  string    `json:"preimage_ref"`
	Postimage string    `json:"postimage_ref"`
	RunID     string    `json:"run_id,omitempty"`
	ToolCall  string    `json:"tool_call,omitempty"`
	At        time.Time `json:"at"`
}

// History keeps preimages and postimages of agent writes
// (Section 13.7, Appendix A): blobs deduplicated by content, so
// killing the daemon between the intent and the effect still leaves
// the preimage in the store and the edit reversible. Safe for
// concurrent use.
type History struct {
	mu      sync.Mutex
	vers    []FileVersion
	byID    map[string]int
	blobs   map[string][]byte
	counter uint64
	now     func() time.Time
}

// NewHistory returns an empty history.
func NewHistory() *History {
	return &History{byID: map[string]int{}, blobs: map[string][]byte{}, now: time.Now}
}

// blobRefOf digests content like the tools history ("blob-sha256:").
func blobRefOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "blob-sha256:" + hex.EncodeToString(sum[:])
}

// PutBlob stores blob bytes and returns the content ref.
func (h *History) PutBlob(data []byte) string {
	ref := blobRefOf(data)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.blobs == nil {
		h.blobs = map[string][]byte{}
	}
	h.blobs[ref] = append([]byte(nil), data...)
	return ref
}

// Blob returns stored blob bytes.
func (h *History) Blob(ref string) ([]byte, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, ok := h.blobs[ref]
	return append([]byte(nil), b...), ok
}

// Record appends a version for path with its pre and post content,
// returning the version id.
func (h *History) Record(path string, pre, post []byte, runID, toolCall string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.byID == nil {
		h.byID = map[string]int{}
	}
	if h.blobs == nil {
		h.blobs = map[string][]byte{}
	}
	h.counter++
	at := time.Now()
	if h.now != nil {
		at = h.now()
	}
	id := fmt.Sprintf("ed_%d_%d", at.UnixNano(), h.counter)
	preRef := blobRefOf(pre)
	postRef := blobRefOf(post)
	h.blobs[preRef] = append([]byte(nil), pre...)
	h.blobs[postRef] = append([]byte(nil), post...)
	v := FileVersion{ID: id, Path: filepath.Clean(path), Preimage: preRef, Postimage: postRef, RunID: runID, ToolCall: toolCall, At: at}
	h.byID[id] = len(h.vers)
	h.vers = append(h.vers, v)
	return id
}

// Versions lists the versions of path oldest first.
func (h *History) Versions(path string) []FileVersion {
	h.mu.Lock()
	defer h.mu.Unlock()
	clean := filepath.Clean(path)
	var out []FileVersion
	for _, v := range h.vers {
		if v.Path == clean {
			out = append(out, v)
		}
	}
	return out
}

// Get returns one version by edit id.
func (h *History) Get(id string) (FileVersion, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	i, ok := h.byID[id]
	if !ok || i >= len(h.vers) {
		return FileVersion{}, false
	}
	return h.vers[i], true
}

// ConflictError reports a three-way revert conflict: the merged view
// is returned for the agent, but nothing was overwritten
// (Section 13.7).
type ConflictError struct {
	EditID   string
	Path     string
	Merged   string
	Conflict string
}

// Error implements error.
func (e *ConflictError) Error() string {
	return fmt.Sprintf("workspace: revert %s conflicts in %s; nothing overwritten:\n%s", e.EditID, e.Path, e.Conflict)
}

// Revert restores one edit without needing git (Section 13.7): when
// the file still holds the postimage it is replaced by the preimage;
// when the file changed afterwards a three-way merge runs, and a
// conflict is reported without overwriting. A successful revert is
// recorded as a new version and its id returned.
func (h *History) Revert(editID, runID, toolCall string) (string, error) {
	h.mu.Lock()
	i, ok := h.byID[editID]
	if !ok || i >= len(h.vers) {
		h.mu.Unlock()
		return "", fmt.Errorf("workspace: unknown edit %q", editID)
	}
	v := h.vers[i]
	pre, okPre := h.blobs[v.Preimage]
	post, okPost := h.blobs[v.Postimage]
	h.mu.Unlock()
	if !okPre || !okPost {
		return "", fmt.Errorf("workspace: missing blobs for edit %q", editID)
	}
	cur, err := os.ReadFile(v.Path)
	if err != nil {
		return "", fmt.Errorf("workspace: read %s: %w", v.Path, err)
	}
	if string(cur) == string(post) {
		if err := atomicWrite(v.Path, pre); err != nil {
			return "", err
		}
		return h.Record(v.Path, cur, pre, runID, toolCall), nil
	}
	// Otherwise undo the recorded change on top of the current
	// content: a three-way merge of base=postimage (the common
	// ancestor), ours=preimage (the change to undo) and
	// theirs=current. Disjoint user changes survive; an overlap
	// conflicts and the file is left untouched.
	merged, conflicted, detail := Merge3(string(post), string(pre), string(cur))
	if conflicted {
		return "", &ConflictError{EditID: editID, Path: v.Path, Merged: merged, Conflict: detail}
	}
	if err := atomicWrite(v.Path, []byte(merged)); err != nil {
		return "", err
	}
	return h.Record(v.Path, cur, []byte(merged), runID, toolCall), nil
}

// Merge3 is a line-based three-way merge of base, mine (postimage)
// and theirs (current content). It returns the merged text, whether
// it conflicted, and a conflict description with both diffs. Edits on
// disjoint line ranges merge cleanly; overlapping edits with the same
// result are fine; anything else conflicts without touching the file.
func Merge3(base, mine, theirs string) (merged string, conflicted bool, detail string) {
	if mine == theirs {
		return mine, false, ""
	}
	if base == mine {
		return theirs, false, ""
	}
	if base == theirs {
		return mine, false, ""
	}
	bl := strings.Split(base, "\n")
	ml := strings.Split(mine, "\n")
	tl := strings.Split(theirs, "\n")
	mOps := diffToBase(bl, ml)
	tOps := diffToBase(bl, tl)
	out, conflicts := mergeOpcodeStreams(bl, mOps, tOps)
	if len(conflicts) > 0 {
		var b strings.Builder
		for _, c := range conflicts {
			fmt.Fprintf(&b, "conflict in base lines %d..%d:\n", c.base0+1, c.base1)
			fmt.Fprintf(&b, "--- agent change ---\n%s\n", strings.Join(c.mine, "\n"))
			fmt.Fprintf(&b, "--- later change ---\n%s\n", strings.Join(c.theirs, "\n"))
		}
		return "", true, b.String()
	}
	return strings.Join(out, "\n"), false, ""
}

// opcode is one base-aligned edit script entry: keep base[i:j], or
// replace base[i:j] with lines.
type opcode struct {
	tag   byte // 'e' equal, 'r' replace/insert/delete
	base0 int
	base1 int
	lines []string
}

// diffToBase aligns base to next with an LCS on line hashes. Files in
// the revert path are source-sized; the DP table is bounded by
// capping exact comparison at 4000 lines per side, beyond which a
// prefix/suffix trim still yields useful opcodes.
func diffToBase(base, next []string) []opcode {
	const capLines = 4000
	if len(base) > capLines || len(next) > capLines {
		return diffTrim(base, next)
	}
	n, m := len(base), len(next)
	// LCS lengths from (i,j) to the end, computed backwards.
	table := make([][]int, n+1)
	for i := range table {
		table[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if base[i] == next[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}
	i, j := 0, 0
	var ops []opcode
	for i < n || j < m {
		if i < n && j < m && base[i] == next[j] {
			k0, l0 := i, j
			for i < n && j < m && base[i] == next[j] {
				i++
				j++
			}
			ops = append(ops, opcode{tag: 'e', base0: k0, base1: i, lines: next[l0:j]})
			continue
		}
		k0, l0 := i, j
		for (i < n || j < m) && !(i < n && j < m && base[i] == next[j]) {
			// Advance along the LCS path: prefer consuming the side
			// the table says is not on an optimal alignment.
			if j < m && (i >= n || table[i][j+1] >= table[i+1][j]) {
				j++
			} else {
				i++
			}
		}
		ops = append(ops, opcode{tag: 'r', base0: k0, base1: i, lines: append([]string(nil), next[l0:j]...)})
	}
	return ops
}

// diffTrim is the fallback for huge files: common prefix/suffix plus
// one middle replacement.
func diffTrim(base, next []string) []opcode {
	pre := 0
	for pre < len(base) && pre < len(next) && base[pre] == next[pre] {
		pre++
	}
	suf := 0
	for suf < len(base)-pre && suf < len(next)-pre && base[len(base)-1-suf] == next[len(next)-1-suf] {
		suf++
	}
	var ops []opcode
	if pre > 0 {
		ops = append(ops, opcode{tag: 'e', base0: 0, base1: pre, lines: next[:pre]})
	}
	ops = append(ops, opcode{tag: 'r', base0: pre, base1: len(base) - suf, lines: append([]string(nil), next[pre:len(next)-suf]...)})
	if suf > 0 {
		ops = append(ops, opcode{tag: 'e', base0: len(base) - suf, base1: len(base), lines: next[len(next)-suf:]})
	}
	return ops
}

type mergeConflict struct {
	base0  int
	base1  int
	mine   []string
	theirs []string
}

// mergeOpcodeStreams replays two base-aligned edit scripts. At every
// base span at most one side may change the region, and at every
// insertion point at most one side may insert different text;
// identical changes on both sides are taken once; anything else
// conflicts.
func mergeOpcodeStreams(base []string, mOps, tOps []opcode) ([]string, []mergeConflict) {
	mIns, mCover := splitOps(mOps)
	tIns, tCover := splitOps(tOps)
	points := map[int]bool{0: true, len(base): true}
	for _, o := range mCover {
		points[o.base0] = true
		points[o.base1] = true
	}
	for _, o := range tCover {
		points[o.base0] = true
		points[o.base1] = true
	}
	for p := range mIns {
		points[p] = true
	}
	for p := range tIns {
		points[p] = true
	}
	sorted := make([]int, 0, len(points))
	for p := range points {
		sorted = append(sorted, p)
	}
	sort.Ints(sorted)
	var out []string
	var conflicts []mergeConflict
	for s, p := range sorted {
		// Insertions at point p come before the span starting at p.
		if mi, ok := mIns[p]; ok {
			if ti, ok2 := tIns[p]; ok2 {
				if equalLines(mi, ti) {
					out = append(out, mi...)
				} else {
					conflicts = append(conflicts, mergeConflict{base0: p, base1: p, mine: mi, theirs: ti})
				}
			} else {
				out = append(out, mi...)
			}
		} else if ti, ok := tIns[p]; ok {
			out = append(out, ti...)
		}
		if s+1 >= len(sorted) {
			break
		}
		a, b := p, sorted[s+1]
		if a == b {
			continue
		}
		mOp := covering(mCover, a, b)
		tOp := covering(tCover, a, b)
		mChanged := mOp != nil && mOp.tag != 'e'
		tChanged := tOp != nil && tOp.tag != 'e'
		switch {
		case !mChanged && !tChanged:
			out = append(out, base[a:b]...)
		case mChanged && !tChanged:
			out = append(out, mOp.lines...)
		case !mChanged && tChanged:
			out = append(out, tOp.lines...)
		default:
			if equalLines(mOp.lines, tOp.lines) {
				out = append(out, mOp.lines...)
			} else {
				conflicts = append(conflicts, mergeConflict{base0: a, base1: b, mine: mOp.lines, theirs: tOp.lines})
			}
		}
	}
	return out, conflicts
}

// splitOps separates insertions (zero-width replacements) from the
// tiling ops that cover every base line exactly once.
func splitOps(ops []opcode) (map[int][]string, []opcode) {
	ins := map[int][]string{}
	var cover []opcode
	for _, o := range ops {
		if o.tag != 'e' && o.base0 == o.base1 {
			ins[o.base0] = append(ins[o.base0], o.lines...)
			continue
		}
		cover = append(cover, o)
	}
	return ins, cover
}

// covering returns the tiling op spanning [a,b].
func covering(ops []opcode, a, b int) *opcode {
	for i := range ops {
		if ops[i].base0 <= a && ops[i].base1 >= b {
			return &ops[i]
		}
	}
	return nil
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".excalibur-revert-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	if dh, err := os.Open(dir); err == nil {
		_ = dh.Sync()
		_ = dh.Close()
	}
	return nil
}
