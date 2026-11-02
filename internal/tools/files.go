package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Window defaults from Section 13.4: 200-line windows with numbers.
const defaultReadWindow = 200

// HistoryStore keeps preimages and postimages of agent edits
// (Section 13.7, minimal H3 subset). The full version history with
// three-way merge lands in H7; here every write records its preimage
// blob hash in the intent path so a crash between intent and effect
// stays reversible.
type HistoryStore interface {
	Append(v FileVersion)
	Versions(path string) []FileVersion
}

// FileVersion is one recorded agent write.
type FileVersion struct {
	ID        string    `json:"id"`
	Created   bool      `json:"created,omitempty"`
	Deleted   bool      `json:"deleted,omitempty"`
	Path      string    `json:"path"`
	Preimage  string    `json:"preimage_ref"`
	Postimage string    `json:"postimage_ref"`
	RunID     string    `json:"run_id,omitempty"`
	ToolCall  string    `json:"tool_call,omitempty"`
	At        time.Time `json:"at"`
}

// MemoryHistory is an in-memory HistoryStore.
type MemoryHistory struct {
	mu    sync.Mutex
	vers  []FileVersion
	blobs map[string][]byte
	now   func() time.Time
}

// NewMemoryHistory returns an empty history.
func NewMemoryHistory() *MemoryHistory {
	return &MemoryHistory{blobs: map[string][]byte{}, now: time.Now}
}

func blobRefOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "blob-sha256:" + hex.EncodeToString(sum[:])
}

// PutBlob stores blob bytes and returns the reference.
func (h *MemoryHistory) PutBlob(data []byte) string {
	ref := blobRefOf(data)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.blobs == nil {
		h.blobs = map[string][]byte{}
	}
	cp := append([]byte(nil), data...)
	h.blobs[ref] = cp
	return ref
}

// Blob returns stored blob bytes.
func (h *MemoryHistory) Blob(ref string) ([]byte, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, ok := h.blobs[ref]
	return append([]byte(nil), b...), ok
}

// Append implements HistoryStore.
func (h *MemoryHistory) Append(v FileVersion) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if v.At.IsZero() && h.now != nil {
		v.At = h.now()
	}
	h.vers = append(h.vers, v)
}

// Versions implements HistoryStore.
func (h *MemoryHistory) Versions(path string) []FileVersion {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []FileVersion
	for _, v := range h.vers {
		if v.Path == path {
			out = append(out, v)
		}
	}
	return out
}

// DiagnosticProvider returns LSP diagnostics for touched files; the
// result is appended to the edit outcome (Section 13.4 step 6).
type DiagnosticProvider interface {
	Diagnostics(paths []string) string
}

// FileHistoryStore is the history collaborator of the file tools:
// preimage/postimage blobs plus the version list (Sections 13.4,
// 13.7). *MemoryHistory is the in-process implementation; the
// workspace-backed history in the wiring layer implements the same
// interface so edits land in one shared history (Section 13.7).
type FileHistoryStore interface {
	PutBlob(data []byte) string
	Blob(ref string) ([]byte, bool)
	Append(v FileVersion)
	Versions(path string) []FileVersion
}

// FileDeps bundles the collaborators of the file tools.
type FileDeps struct {
	Fingerprints *FingerprintStore
	Leases       LeaseManager
	History      FileHistoryStore
	Diagnostics  DiagnosticProvider
	RunID        string
	// Workdir is the run's project directory. Relative paths resolve
	// against it when set; empty keeps the process working directory.
	Workdir string
	Now     func() time.Time
}

// runWorkdir returns the run's project directory, or "" without deps.
func (d *FileDeps) runWorkdir() string {
	if d == nil {
		return ""
	}
	return d.Workdir
}

// resolvePath maps a tool path to the file it names. Relative paths
// resolve against the run workdir when the run declares one
// (EXCALIBUR_RUN_WORKDIR, default on), so every file tool in a run
// agrees on where a relative path lives. Otherwise they resolve
// against the process working directory, as before.
func (d *FileDeps) resolvePath(p string) string {
	if d != nil && d.Workdir != "" && !filepath.IsAbs(p) && RunWorkdirEnabled() {
		return filepath.Join(d.Workdir, p)
	}
	return filepath.Clean(p)
}

// fingerprintRefusal applies the fingerprint guard to an existing file.
// It returns the refusal text, or "" when the write may proceed. The
// second result reports the legacy explicit "*" wildcard, which skips
// the check only when EXCALIBUR_FP_AUTO is off.
func fingerprintRefusal(fps *FingerprintStore, path, expectedFP string) (string, bool) {
	if FingerprintAutoEnabled() {
		mi := fps.Guard(path, CleanFingerprint(expectedFP))
		if mi == nil {
			return "", false
		}
		return mismatchText(mi), false
	}
	if expectedFP == "*" {
		return "", true
	}
	expected := Fingerprint(expectedFP)
	if expected == "" {
		if fp, ok := fps.LastRead(path); ok {
			expected = fp
		}
	}
	if mi := fps.Check(path, expected); mi != nil {
		return mismatchText(mi), false
	}
	return "", false
}

func mismatchText(mi *MismatchInfo) string {
	out := (&FingerprintError{Info: *mi}).Error()
	if mi.ShouldScale {
		out += "\nscale: 3 consecutive mismatches; escalate to parent"
	}
	return out
}

// expectedFPFor returns the caller's expected fingerprint for a patch
// op, looked up by the path as written and then by the resolved path.
func expectedFPFor(m map[string]string, raw, resolved string) string {
	if v, ok := m[raw]; ok {
		return v
	}
	return m[resolved]
}

// readFileArgs is the read_file input shape.
type readFileArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

func schemaFor(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

var readFileSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"path":   map[string]any{"type": "string", "minLength": 1},
		"offset": map[string]any{"type": "integer", "minimum": 1},
		"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 2000},
	},
	"required":             []string{"path"},
	"additionalProperties": false,
})

var writeFileSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"path":        map[string]any{"type": "string", "minLength": 1},
		"content":     map[string]any{"type": "string"},
		"expected_fp": map[string]any{"type": "string", "description": "Optional. Existing files are checked against the version this run last read or wrote; pass a fingerprint only to name one explicitly"},
	},
	"required":             []string{"path", "content"},
	"additionalProperties": false,
})

var editFileSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"path":        map[string]any{"type": "string", "minLength": 1},
		"old_text":    map[string]any{"type": "string", "minLength": 1},
		"new_text":    map[string]any{"type": "string"},
		"expected_fp": map[string]any{"type": "string", "description": "Optional. Checked against the version this run last read or wrote"},
		"occurrence":  map[string]any{"type": "integer", "minimum": 1, "description": "1-based index of the exact match to edit when old_text matches several places"},
	},
	"required":             []string{"path", "old_text", "new_text"},
	"additionalProperties": false,
})

var applyPatchSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"ops": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":     map[string]any{"type": "string"},
					"op":       map[string]any{"type": "string", "enum": []string{"add", "delete", "update"}},
					"old_text": map[string]any{"type": "string"},
					"new_text": map[string]any{"type": "string"},
					"context":  map[string]any{"type": "string"},
				},
				"required": []string{"path", "op"},
			},
			"minItems": 1,
		},
		"expected_fps": map[string]any{"type": "object"},
	},
	"required":             []string{"ops"},
	"additionalProperties": false,
})

// ReadFileTool implements read_file.
type ReadFileTool struct{ Deps *FileDeps }

func (t *ReadFileTool) Definition() Definition {
	return Definition{
		Name: "read_file", Description: "Read a file window with line numbers and fingerprint",
		InputSchema: readFileSchema, Effect: "read_only", Idempotency: "idempotent",
		Cancel: "immediate", Timeout: 60_000_000_000, OutputBudget: DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "files", Tags: []string{"file", "read"}, Keywords: []string{"read", "file", "view", "lines"},
	}
}

func (t *ReadFileTool) Invoke(_ context.Context, inv Invocation) (Result, error) {
	var a readFileArgs
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	path := t.Deps.resolvePath(a.Path)
	// Task boundary (exec scope only): the harness's own logs and other
	// processes' roots are not readable from a run (item 93).
	if err := CheckPathBoundary(path); err != nil {
		return Result{Status: StatusDenied, Output: err.Error()}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{Status: StatusToolError, Output: fmt.Sprintf("read %s: %v. hint: to create it use write_file {\"path\": %q, \"content\": \"...\"}; to locate files use glob or grep", path, err, path)}, nil
	}
	fp := FingerprintOf(data)
	if t.Deps != nil && t.Deps.Fingerprints != nil {
		t.Deps.Fingerprints.NoteRead(path, fp)
	}
	text := string(data)
	lines := strings.Split(text, "\n")
	total := len(lines)
	// Trailing newline produces a phantom empty line; keep numbering
	// stable by trimming it from the count only.
	if total > 0 && lines[total-1] == "" {
		lines = lines[:total-1]
		total = len(lines)
	}
	offset := a.Offset
	if offset <= 0 {
		offset = 1
	}
	limit := a.Limit
	if limit <= 0 {
		limit = defaultReadWindow
	}
	if offset > total && total > 0 {
		return Result{Status: StatusInputError, Output: fmt.Sprintf("offset %d beyond %d lines", offset, total)}, nil
	}
	end := offset - 1 + limit
	if end > total {
		end = total
	}
	var b strings.Builder
	fmt.Fprintf(&b, "fp: %s  lines: %d  window: %d..%d\n", fp, total, offset, end)
	for i := offset; i <= end; i++ {
		fmt.Fprintf(&b, "%6d  %s\n", i, lines[i-1])
	}
	return Result{Status: StatusOK, Output: b.String()}, nil
}

func (t *ReadFileTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a readFileArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	return []string{"file:" + t.Deps.resolvePath(a.Path)}, nil
}

// WriteFileTool implements write_file with atomic replace.
type WriteFileTool struct{ Deps *FileDeps }

func (t *WriteFileTool) Definition() Definition {
	return Definition{
		Name: "write_file", Description: "Create or rewrite a whole file, checking the fingerprint when it exists",
		InputSchema: writeFileSchema, Effect: "write_local", Idempotency: "idempotent",
		Cancel: "after_completion", Timeout: 60_000_000_000, OutputBudget: DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Inverse: "revert_edit", Probe: "file fingerprint equals postimage",
		Group: "files", Tags: []string{"file", "write"}, Keywords: []string{"write", "file", "create", "overwrite"},
	}
}

func (t *WriteFileTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	var a struct {
		Path       string `json:"path"`
		Content    string `json:"content"`
		ExpectedFP string `json:"expected_fp"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	path := t.Deps.resolvePath(a.Path)
	if StructuredWriteCheckEnabled() {
		if refusal := literalEscapeRefusal(path, a.Content); refusal != "" {
			return Result{Status: StatusInputError, Output: refusal}, nil
		}
	}
	deps := t.Deps
	fps := NewFingerprintStore()
	var leases LeaseManager
	var hist FileHistoryStore
	var diags DiagnosticProvider
	var runID string
	if deps != nil {
		if deps.Fingerprints != nil {
			fps = deps.Fingerprints
		}
		leases = deps.Leases
		hist = deps.History
		diags = deps.Diagnostics
		runID = deps.RunID
	}
	var release func()
	runID = ExecutingRunID(ctx, runID)
	if leases != nil {
		var err error
		release, err = leases.Acquire(path, runID)
		if err != nil {
			return Result{Status: StatusToolError, Output: err.Error()}, nil
		}
		defer release()
	}
	// An existing file must still match what this run last saw. A new
	// file has nothing to check against.
	skipped := false
	if _, err := os.Stat(path); err == nil {
		refusal, wildcard := fingerprintRefusal(fps, path, a.ExpectedFP)
		if refusal != "" {
			return Result{Status: StatusToolError, Output: refusal}, nil
		}
		skipped = wildcard
	}
	// Record the preimage before any effect so a crash between intent
	// and effect stays reversible (Section 13.4 step 3).
	_, preErr := os.Stat(path)
	var preRef string
	if hist != nil {
		var pre []byte
		if data, err := os.ReadFile(path); err == nil {
			pre = data
		}
		preRef = hist.PutBlob(pre)
	}
	if err := atomicWriteFile(path, []byte(a.Content), 0o644); err != nil {
		return Result{Status: StatusToolError, Output: err.Error()}, nil
	}
	fp := FingerprintOf([]byte(a.Content))
	fps.NoteRead(path, fp)
	fps.ResetMismatches(path)
	if hist != nil {
		postRef := hist.PutBlob([]byte(a.Content))
		hist.Append(FileVersion{Created: os.IsNotExist(preErr), ID: newVersionID(), Path: path, Preimage: preRef, Postimage: postRef, RunID: runID, At: time.Now()})
	}
	out := fmt.Sprintf("wrote %s (%d bytes) fp: %s", path, len(a.Content), fp)
	if skipped {
		out += " (fingerprint check skipped by explicit \"*\")"
	}
	if StructuredWriteCheckEnabled() {
		if warning := parseWarning(path, a.Content); warning != "" {
			out += "\n" + warning
		}
	}
	if diags != nil {
		if d := diags.Diagnostics([]string{path}); d != "" {
			out += "\ndiagnostics:\n" + d
		}
	}
	return Result{Status: StatusOK, Output: out}, nil
}

func (t *WriteFileTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	return []string{"file:" + t.Deps.resolvePath(a.Path)}, nil
}

// EditFileTool implements edit_file with three-level matching.
type EditFileTool struct{ Deps *FileDeps }

func (t *EditFileTool) Definition() Definition {
	return Definition{
		Name: "edit_file", Description: "Replace a text fragment with three-level matching and fingerprint check",
		InputSchema: editFileSchema, Effect: "write_local", Idempotency: "idempotent",
		Cancel: "after_completion", Timeout: 60_000_000_000, OutputBudget: DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Inverse: "revert_edit", Probe: "file contains new_text at the matched location",
		Group: "files", Tags: []string{"file", "edit"}, Keywords: []string{"edit", "file", "replace", "patch"},
	}
}

func (t *EditFileTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	var a struct {
		Path       string `json:"path"`
		OldText    string `json:"old_text"`
		NewText    string `json:"new_text"`
		ExpectedFP string `json:"expected_fp"`
		Occurrence int    `json:"occurrence"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	path := t.Deps.resolvePath(a.Path)
	deps := t.Deps
	fps := NewFingerprintStore()
	var leases LeaseManager
	var hist FileHistoryStore
	var diags DiagnosticProvider
	var runID string
	if deps != nil {
		if deps.Fingerprints != nil {
			fps = deps.Fingerprints
		}
		leases = deps.Leases
		hist = deps.History
		diags = deps.Diagnostics
		runID = deps.RunID
	}
	var release func()
	runID = ExecutingRunID(ctx, runID)
	if leases != nil {
		var err error
		release, err = leases.Acquire(path, runID)
		if err != nil {
			return Result{Status: StatusToolError, Output: err.Error()}, nil
		}
		defer release()
	}
	refusal, skipped := fingerprintRefusal(fps, path, a.ExpectedFP)
	if refusal != "" {
		return Result{Status: StatusToolError, Output: refusal}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{Status: StatusToolError, Output: fmt.Sprintf("read %s: %v. hint: to create it use write_file {\"path\": %q, \"content\": \"...\"}", path, err, path)}, nil
	}
	content := string(data)
	off, length, refusal := locateOldText(content, path, a.OldText, a.Occurrence)
	if refusal != "" {
		return Result{Status: StatusToolError, Output: refusal}, nil
	}
	if off+length > len(content) {
		length = len(content) - off
	}
	var preRef string
	if hist != nil {
		preRef = hist.PutBlob(data)
	}
	updated := content[:off] + a.NewText + content[off+length:]
	if err := atomicWriteFile(path, []byte(updated), 0o644); err != nil {
		return Result{Status: StatusToolError, Output: err.Error()}, nil
	}
	fp := FingerprintOf([]byte(updated))
	fps.NoteRead(path, fp)
	fps.ResetMismatches(path)
	if hist != nil {
		postRef := hist.PutBlob([]byte(updated))
		hist.Append(FileVersion{ID: newVersionID(), Path: path, Preimage: preRef, Postimage: postRef, RunID: runID, At: time.Now()})
	}
	out := fmt.Sprintf("edited %s fp: %s", path, fp)
	if skipped {
		out += " (fingerprint check skipped by explicit \"*\")"
	}
	if diags != nil {
		if d := diags.Diagnostics([]string{path}); d != "" {
			out += "\ndiagnostics:\n" + d
		}
	}
	return Result{Status: StatusOK, Output: out}, nil
}

func (t *EditFileTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	return []string{"file:" + t.Deps.resolvePath(a.Path)}, nil
}

// matchedSlice returns the slice of content that FindMatch matched at
// off. For normalized levels the matched length can differ from
// len(oldText); approximate by oldText length on the original.
func matchedSlice(content string, off int, oldText string) string {
	if off < 0 || off > len(content) {
		return oldText
	}
	// Exact level first.
	if strings.HasPrefix(content[off:], oldText) {
		return oldText
	}
	// Normalized/trimmed: consume the same line count as oldText.
	nl := strings.Count(oldText, "\n") + 1
	rest := content[off:]
	lines := strings.Split(rest, "\n")
	if len(lines) >= nl {
		joined := strings.Join(lines[:nl], "\n")
		// Verify it matches under some level before using it.
		if _, _, amb := FindMatch(joined, oldText); amb == nil {
			_ = amb
		}
		return joined
	}
	return oldText
}

// PatchOp is one apply_patch operation.
type PatchOp struct {
	Path    string `json:"path"`
	Op      string `json:"op"`
	OldText string `json:"old_text,omitempty"`
	NewText string `json:"new_text,omitempty"`
	Context string `json:"context,omitempty"`
}

// ApplyPatchTool implements apply_patch over several files.
type ApplyPatchTool struct{ Deps *FileDeps }

func (t *ApplyPatchTool) Definition() Definition {
	return Definition{
		Name: "apply_patch", Description: "Apply multi-file add/delete/update operations with context",
		InputSchema: applyPatchSchema, Effect: "write_local", Idempotency: "idempotent",
		Cancel: "after_completion", Timeout: 60_000_000_000, OutputBudget: DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Inverse: "revert_edit", Probe: "files contain the patched postimages",
		Group: "files", Tags: []string{"file", "patch"}, Keywords: []string{"patch", "apply", "multi-file", "edit"},
	}
}

func (t *ApplyPatchTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	var a struct {
		Ops         []PatchOp         `json:"ops"`
		ExpectedFPs map[string]string `json:"expected_fps"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	if len(a.Ops) == 0 {
		return Result{Status: StatusInputError, Output: "ops must not be empty"}, nil
	}
	deps := t.Deps
	fps := NewFingerprintStore()
	var leases LeaseManager
	var hist FileHistoryStore
	var diags DiagnosticProvider
	var runID string
	if deps != nil {
		if deps.Fingerprints != nil {
			fps = deps.Fingerprints
		}
		leases = deps.Leases
		hist = deps.History
		diags = deps.Diagnostics
		runID = deps.RunID
	}
	// Validate all ops first so a bad patch applies nothing.
	type planned struct {
		path       string
		updated    string
		isNew      bool
		expectedFP string
	}
	plans := make([]planned, 0, len(a.Ops))
	touched := map[string]bool{}
	for i, op := range a.Ops {
		path := t.Deps.resolvePath(op.Path)
		expectedFP := expectedFPFor(a.ExpectedFPs, op.Path, path)
		switch op.Op {
		case "add":
			if _, err := os.Stat(path); err == nil {
				return Result{Status: StatusToolError, Output: fmt.Sprintf("op %d: %s exists; use update", i, path)}, nil
			}
			plans = append(plans, planned{path: path, updated: op.NewText, isNew: true})
		case "delete":
			if refusal, _ := fingerprintRefusal(fps, path, expectedFP); refusal != "" {
				return Result{Status: StatusToolError, Output: refusal}, nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return Result{Status: StatusToolError, Output: fmt.Sprintf("op %d: %v", i, err)}, nil
			}
			off, _, amb := FindMatch(string(data), op.OldText)
			if off < 0 {
				if amb != nil {
					return Result{Status: StatusToolError, Output: fmt.Sprintf("op %d: multiple matches at lines %v", i, amb)}, nil
				}
				return Result{Status: StatusToolError, Output: fmt.Sprintf("op %d: old_text not found in %s", i, path)}, nil
			}
			matched := matchedSlice(string(data), off, op.OldText)
			plans = append(plans, planned{path: path, updated: string(data)[:off] + string(data)[off+len(matched):], expectedFP: expectedFP})
		case "update":
			if refusal, _ := fingerprintRefusal(fps, path, expectedFP); refusal != "" {
				return Result{Status: StatusToolError, Output: refusal}, nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return Result{Status: StatusToolError, Output: fmt.Sprintf("op %d: %v", i, err)}, nil
			}
			content := string(data)
			if op.Context != "" && !strings.Contains(content, op.Context) {
				return Result{Status: StatusToolError, Output: fmt.Sprintf("op %d: context not found in %s", i, path)}, nil
			}
			off, _, amb := FindMatch(content, op.OldText)
			if off < 0 {
				if amb != nil {
					return Result{Status: StatusToolError, Output: fmt.Sprintf("op %d: multiple matches at lines %v", i, amb)}, nil
				}
				return Result{Status: StatusToolError, Output: fmt.Sprintf("op %d: old_text not found in %s", i, path)}, nil
			}
			matched := matchedSlice(content, off, op.OldText)
			plans = append(plans, planned{path: path, updated: content[:off] + op.NewText + content[off+len(matched):], expectedFP: expectedFP})
		default:
			return Result{Status: StatusInputError, Output: fmt.Sprintf("op %d: unknown op %q", i, op.Op)}, nil
		}
		touched[path] = true
	}
	// Apply with leases held per file.
	releases := map[string]func(){}
	runID = ExecutingRunID(ctx, runID)
	if leases != nil {
		for p := range touched {
			rel, err := leases.Acquire(p, runID)
			if err != nil {
				for _, r := range releases {
					r()
				}
				return Result{Status: StatusToolError, Output: err.Error()}, nil
			}
			releases[p] = rel
		}
		defer func() {
			for _, r := range releases {
				r()
			}
		}()
	}
	// Re-check fingerprints under the lease, then write.
	for _, p := range plans {
		if !p.isNew {
			if refusal, _ := fingerprintRefusal(fps, p.path, p.expectedFP); refusal != "" {
				return Result{Status: StatusToolError, Output: refusal}, nil
			}
		}
		var preRef string
		if hist != nil {
			var pre []byte
			if data, err := os.ReadFile(p.path); err == nil {
				pre = data
			}
			preRef = hist.PutBlob(pre)
		}
		if err := atomicWriteFile(p.path, []byte(p.updated), 0o644); err != nil {
			return Result{Status: StatusToolError, Output: err.Error()}, nil
		}
		if hist != nil {
			postRef := hist.PutBlob([]byte(p.updated))
			hist.Append(FileVersion{Created: p.isNew, ID: newVersionID(), Path: p.path, Preimage: preRef, Postimage: postRef, RunID: runID, At: time.Now()})
		}
		fps.NoteRead(p.path, FingerprintOf([]byte(p.updated)))
		fps.ResetMismatches(p.path)
	}
	paths := make([]string, 0, len(touched))
	for p := range touched {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	out := fmt.Sprintf("patched %d file(s): %s", len(paths), strings.Join(paths, ", "))
	if diags != nil {
		if d := diags.Diagnostics(paths); d != "" {
			out += "\ndiagnostics:\n" + d
		}
	}
	return Result{Status: StatusOK, Output: out}, nil
}

func (t *ApplyPatchTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a struct {
		Ops []PatchOp `json:"ops"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(a.Ops))
	for _, op := range a.Ops {
		keys = append(keys, "file:"+t.Deps.resolvePath(op.Path))
	}
	sort.Strings(keys)
	return keys, nil
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".excalibur-write-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
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
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	// fsync the directory so the rename survives a crash.
	if dh, err := os.Open(dir); err == nil {
		_ = dh.Sync()
		_ = dh.Close()
	}
	return nil
}

var versionCounter uint64
var versionMu sync.Mutex

func newVersionID() string {
	versionMu.Lock()
	defer versionMu.Unlock()
	versionCounter++
	return fmt.Sprintf("fv_%d_%d", time.Now().UnixNano(), versionCounter)
}
