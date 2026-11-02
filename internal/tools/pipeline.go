package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

// Pipeline budgets from Section 13.4 and Appendix B.
const (
	// DefaultOutputBudget caps normalized tool output kept inline.
	// Appendix B: head and tail of about 10K tokens (~40KB of text).
	DefaultOutputBudget int64 = 40 * 1024
	// MaxBufferedOutput caps the head+tail accumulation buffer (1 MiB).
	MaxBufferedOutput int64 = 1 << 20
	// DefaultToolTimeout applies when a definition leaves Timeout at 0.
	DefaultToolTimeout = 60 * time.Second
	// TruncationMarker separates kept head and tail.
	TruncationMarker = "\n…[truncated %d bytes; full output in blob %s; fetch with get_tool_output {\"ref\": \"%s\"}]…\n"
)

// OutputStore keeps full outputs behind a blob reference so
// get_tool_output can retrieve what truncation or masking hid
// (Section 13.4 step 5).
type OutputStore interface {
	// Put stores full output and returns its blob reference.
	Put(full string) string
	// Get returns the stored output for ref.
	Get(ref string) (string, bool)
}

// MemoryOutputStore is an in-memory OutputStore for tests and the
// embedded profile. Production wires the blob store behind it.
type MemoryOutputStore struct {
	calls   map[string]string
	mu      sync.Mutex
	data    map[string]string
	records []OutputRecord
	seq     int
}

// OutputTag names the call that produced a stored output. It is kept
// with the output so get_tool_output can list refs with their producer.
type OutputTag struct {
	Tool    string
	Summary string
}

// OutputRecord describes one stored output: its ref, the call id that
// produced it (call-N), the producing tool and call summary, and size.
type OutputRecord struct {
	Ref     string
	Call    string
	Tool    string
	Summary string
	Bytes   int
}

// TaggedOutputStore is implemented by stores that keep the producer of
// each output. The executor uses it when available.
type TaggedOutputStore interface {
	PutTagged(tag OutputTag, full string) string
}

// NewMemoryOutputStore returns an empty store.
func NewMemoryOutputStore() *MemoryOutputStore {
	return &MemoryOutputStore{data: map[string]string{}}
}

// Put implements OutputStore.
func (s *MemoryOutputStore) Put(full string) string {
	return s.PutTagged(OutputTag{}, full)
}

// PutTagged stores full output with its producer and returns its blob
// reference. Identical output keeps its first record and call id.
func (s *MemoryOutputStore) PutTagged(tag OutputTag, full string) string {
	sum := sha256.Sum256([]byte(full))
	ref := "blob-sha256:" + hex.EncodeToString(sum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = map[string]string{}
	}
	if _, seen := s.data[ref]; !seen {
		s.seq++
		s.records = append(s.records, OutputRecord{
			Ref: ref, Call: fmt.Sprintf("call-%d", s.seq),
			Tool: tag.Tool, Summary: tag.Summary, Bytes: len(full),
		})
	}
	s.data[ref] = full
	return ref
}

// Records lists stored outputs newest first, at most limit of them
// (limit <= 0 lists all).
func (s *MemoryOutputStore) Records(limit int) []OutputRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]OutputRecord, 0, len(s.records))
	for i := len(s.records) - 1; i >= 0; i-- {
		out = append(out, s.records[i])
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out
}

// ResolveCall maps a call id (call-N) to the blob reference it stored.
func (s *MemoryOutputStore) ResolveCall(call string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ref, ok := s.calls[call]; ok {
		return ref, true
	}
	for _, r := range s.records {
		if r.Call == call {
			return r.Ref, true
		}
	}
	return "", false
}

// BindCall associates the owning process/tool call with an existing immutable
// blob. Repeated binding cannot replace original output with a masking marker.
func (s *MemoryOutputStore) BindCall(call, ref string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if call == "" {
		return
	}
	if _, ok := s.data[ref]; !ok {
		return
	}
	if _, exists := s.calls[call]; exists {
		return
	}
	if s.calls == nil {
		s.calls = map[string]string{}
	}
	alreadyBound := false
	for _, existing := range s.calls {
		if existing == ref {
			alreadyBound = true
			break
		}
	}
	s.calls[call] = ref
	if !alreadyBound {
		for i := range s.records {
			if s.records[i].Ref == ref {
				s.records[i].Call = call
				break
			}
		}
	}
}

// Get implements OutputStore.
func (s *MemoryOutputStore) Get(ref string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[ref]
	return v, ok
}

// Redactor rewrites secret values before output normalization
// (Section 20.1). Nil means no redaction.
type Redactor func(s string) string

// IntentHook records tool intent before any effect and the result
// after (Section 13.4 steps 3 and 7). The journal owns durability;
// the executor only calls the hooks.
type IntentHook interface {
	RecordIntent(ctx context.Context, inv Invocation) error
	RecordResult(ctx context.Context, inv Invocation, res Result) error
}

// ExecuteOptions tunes one Executor.Execute call.
type ExecuteOptions struct {
	// CallID binds full output retrieval to the model-visible observation identity.
	CallID string
	// Timeout overrides the definition timeout when positive.
	Timeout time.Duration
	// IdempotencyKey is sent with the invocation; keyed tools reuse it
	// on retry instead of repeating effects (Section 17.2).
	IdempotencyKey string
}

// Executor runs tools through the Section 13.4 pipeline: repair and
// validate arguments, resolve resource keys, record intent, execute
// with timeout and cancel mode, redact, normalize and truncate output,
// store the full output and record the result.
type Executor struct {
	Registry Registry
	Outputs  OutputStore
	Redact   Redactor
	Journal  IntentHook
	Now      func() time.Time
	NewID    func() string
	// Tune overrides the surface ablation switches. Nil means the
	// all-ON default; production sets TuningFromEnv at the
	// composition root (wireengine.BuildEmbedded).
	Tune *SurfaceTuning
}

// NewExecutor returns an Executor with sane defaults.
func NewExecutor(reg Registry) *Executor {
	return &Executor{
		Registry: reg,
		Outputs:  NewMemoryOutputStore(),
		Now:      time.Now,
		NewID: func() string {
			var b [16]byte
			_, _ = rand.Read(b[:])
			return hex.EncodeToString(b[:])
		},
	}
}

// Execute runs the named tool through the full pipeline.
func (e *Executor) Execute(ctx context.Context, name string, rawArgs json.RawMessage, opts ExecuteOptions) (Result, error) {
	tune := DefaultSurfaceTuning()
	if e.Tune != nil {
		tune = *e.Tune
	}
	return e.ExecuteTuned(ctx, name, rawArgs, opts, tune)
}

// ExecuteTuned runs the named tool through the full pipeline under
// an explicit surface tuning (ablation switches for the agent-quality
// workstream; production passes TuningFromEnv(DefaultSurfaceTuning())).
func (e *Executor) ExecuteTuned(ctx context.Context, name string, rawArgs json.RawMessage, opts ExecuteOptions, tune SurfaceTuning) (Result, error) {
	if SkillToolDisabled(name) {
		return Result{Status: StatusInputError, Output: "skills library is disabled (EXCALIBUR_SKILLS=off)"}, nil
	}
	tool, err := e.Registry.Get(name)
	if err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	def := tool.Definition()

	repaired := RepairJSON(rawArgs)
	normalized, repairs := NormalizeSchemaArgs(repaired, def.InputSchema, tune.TolerantArgs)
	normalized = NormalizeArgs(name, normalized, tune)
	normalized, moreRepairs := NormalizeSchemaArgs(normalized, def.InputSchema, tune.TolerantArgs)
	repairs = append(repairs, moreRepairs...)
	if err := ValidateAgainstSchema(normalized, def.InputSchema); err != nil {
		out := "invalid arguments: " + err.Error() + schemaHint(def.InputSchema)
		if tune.ActionableErrors {
			out += "\n" + RepairHint(name, repaired)
		}
		return Result{Status: StatusInputError, Output: out}, nil
	}
	if refusal := redactionWriteRefusal(name, normalized); refusal != "" {
		return Result{Status: StatusInputError, Output: refusal}, nil
	}

	timeout := def.Timeout
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}
	if timeout <= 0 {
		timeout = DefaultToolTimeout
	}

	inv := Invocation{Tool: name, Arguments: normalized, IdempotencyKey: opts.IdempotencyKey, Timeout: timeout}
	if inv.IdempotencyKey == "" && def.Idempotency == types.IdemKeyed {
		inv.IdempotencyKey = e.newKey()
	}

	if e.Journal != nil {
		if err := e.Journal.RecordIntent(ctx, inv); err != nil {
			return Result{Status: StatusTransient, Transient: true, Output: "journal unavailable: " + err.Error()}, nil
		}
	}

	callCtx, cancel := e.contextForCancel(ctx, def.Cancel, timeout)
	defer cancel()

	resCh := make(chan toolOutcome, 1)
	safego.Go("tools/executor-invoke", nil, func() {
		res, err := tool.Invoke(callCtx, inv)
		resCh <- toolOutcome{res: res, err: err}
	})

	var res Result
	select {
	case <-callCtx.Done():
		err := callCtx.Err()
		if ctx.Err() != nil && def.Cancel == types.CancelAfterCompletion {
			// After-completion tools are left to finish up to their
			// timeout even when the caller went away.
			select {
			case out := <-resCh:
				var ferr error
				res, ferr = e.finishInvocation(out, def)
				if ferr != nil {
					return Result{Status: StatusToolError, Output: ferr.Error()}, nil
				}
			case <-time.After(timeout):
				return Result{Status: StatusTimeout, Output: fmt.Sprintf("tool %q exceeded timeout %s", name, timeout)}, nil
			}
			break
		}
		if err == context.DeadlineExceeded {
			return Result{Status: StatusTimeout, Output: fmt.Sprintf("tool %q exceeded timeout %s", name, timeout)}, nil
		}
		if out, ok := tryOutcome(resCh); ok {
			res, err = e.finishInvocation(out, def)
			if err != nil {
				return Result{Status: StatusToolError, Output: err.Error()}, nil
			}
			res.Status = StatusCanceled
			res.Transient = false
			break
		}
		_ = err
		return Result{Status: StatusCanceled, Output: fmt.Sprintf("tool %q canceled", name)}, nil
	case out := <-resCh:
		var ferr error
		res, ferr = e.finishInvocation(out, def)
		if ferr != nil {
			return Result{Status: StatusToolError, Output: ferr.Error()}, nil
		}
	}

	if len(repairs) > 0 {
		res.Output = "argument normalization: " + strings.Join(repairs, "; ") + "\n" + res.Output
	}
	res = e.normalize(res, def, OutputTag{Tool: name, Summary: callSummary(name, normalized)})
	if switchDefaultOn("EXCALIBUR_MASK_IDEMPOTENT") && opts.CallID != "" && e.Outputs != nil {
		if binder, ok := e.Outputs.(interface{ BindCall(string, string) }); ok {
			ref := res.BlobRef
			if _, exists := e.Outputs.Get(ref); !exists {
				if tagged, ok := e.Outputs.(TaggedOutputStore); ok {
					ref = tagged.PutTagged(OutputTag{Tool: name, Summary: callSummary(name, normalized)}, res.Output)
				} else {
					ref = e.Outputs.Put(res.Output)
				}
			}
			binder.BindCall(opts.CallID, ref)
		}
	}

	if e.Journal != nil {
		_ = e.Journal.RecordResult(ctx, inv, res)
	}
	return res, nil
}

type toolOutcome struct {
	res Result
	err error
}

func tryOutcome(ch <-chan toolOutcome) (toolOutcome, bool) {
	select {
	case out := <-ch:
		return out, true
	default:
		return toolOutcome{}, false
	}
}

func (e *Executor) finishInvocation(out toolOutcome, _ Definition) (Result, error) {
	if out.err != nil {
		return Result{}, out.err
	}
	return out.res, nil
}

func (e *Executor) normalize(res Result, def Definition, tag OutputTag) Result {
	if res.Status == "" {
		res.Status = StatusOK
	}
	if e.Redact != nil && res.Output != "" {
		res.Output = e.Redact(res.Output)
	}
	// Error text is always kept literal: truncation applies to the
	// successful payload, but even errors go through the blob store so
	// get_tool_output can page them.
	budget := def.OutputBudget
	if budget <= 0 {
		budget = DefaultOutputBudget
	}
	if int64(len(res.Output)) > budget {
		full := res.Output
		var ref string
		if tagged, ok := e.Outputs.(TaggedOutputStore); ok {
			ref = tagged.PutTagged(tag, full)
		} else if e.Outputs != nil {
			ref = e.Outputs.Put(full)
		} else {
			sum := sha256.Sum256([]byte(full))
			ref = "blob-sha256:" + hex.EncodeToString(sum[:])
		}
		res.Output = TruncateHeadTail(full, budget, ref)
		res.Truncated = true
		res.BlobRef = ref
	} else if res.Truncated && res.BlobRef == "" && e.Outputs != nil {
		res.BlobRef = e.Outputs.Put(res.Output)
	}
	return res
}

func (e *Executor) contextForCancel(parent context.Context, mode types.CancelMode, timeout time.Duration) (context.Context, context.CancelFunc) {
	switch mode {
	case types.CancelAfterCompletion:
		// Detach from parent cancellation; only the timeout applies so
		// atomic writes, git operations and transactions finish first.
		return context.WithTimeout(context.Background(), timeout)
	case types.CancelCooperative:
		// Cooperative tools are asked to stop; the executor still bounds
		// the wait by the timeout.
		return context.WithTimeout(parent, timeout)
	default:
		return context.WithTimeout(parent, timeout)
	}
}

func (e *Executor) newKey() string {
	if e.NewID != nil {
		return e.NewID()
	}
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// schemaHint appends the required-property summary of the schema so
// grave JSON errors still carry the failing schema part (13.4 step 1).
func schemaHint(schema json.RawMessage) string {
	var sch map[string]any
	if err := json.Unmarshal(schema, &sch); err != nil {
		return ""
	}
	req, _ := sch["required"].([]any)
	if len(req) == 0 {
		return ""
	}
	names := make([]string, 0, len(req))
	for _, r := range req {
		if s, ok := r.(string); ok {
			names = append(names, s)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return " (schema requires: " + strings.Join(names, ", ") + ")"
}

// TruncateHeadTail keeps head and tail within budget with a marker in
// the middle carrying the blob reference (Section 13.4 step 5).
func TruncateHeadTail(s string, budget int64, ref string) string {
	if budget <= 0 || int64(len(s)) <= budget {
		return s
	}
	marker := fmt.Sprintf(TruncationMarker, len(s), ref, ref)
	keep := int(budget)
	if keep > len(s) {
		keep = len(s)
	}
	// Reserve room for the marker.
	head := (keep - len(marker)) / 2
	if head < 0 {
		head = 0
	}
	tail := keep - len(marker) - head
	if tail < 0 {
		tail = 0
	}
	if head+tail >= len(s) {
		return s
	}
	return s[:head] + marker + s[len(s)-tail:]
}

// RepairJSON fixes minor JSON mistakes weak models make (trailing
// commas, smart quotes) before schema validation (Section 13.4 step 1).
// It returns the repaired bytes; inputs that are already valid pass
// through a compacting re-encode. Unrepairable input is returned as-is
// so validation reports the schema error.
func RepairJSON(raw json.RawMessage) json.RawMessage {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return raw
	}
	if json.Valid(t) {
		var v any
		if err := json.Unmarshal(t, &v); err == nil {
			if enc, err := json.Marshal(v); err == nil {
				return enc
			}
		}
		return json.RawMessage(t)
	}
	fixed := stripTrailingCommas(string(t))
	fixed = normalizeQuotes(fixed)
	fb := bytes.TrimSpace([]byte(fixed))
	if json.Valid(fb) {
		var v any
		if err := json.Unmarshal(fb, &v); err == nil {
			if enc, err := json.Marshal(v); err == nil {
				return enc
			}
			return json.RawMessage(fb)
		}
	}
	return raw
}

func stripTrailingCommas(s string) string {
	var out strings.Builder
	inStr := false
	esc := false
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			out.WriteByte(c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == quote {
				inStr = false
			}
			continue
		}
		if c == '"' || c == '\'' {
			inStr = true
			quote = c
			out.WriteByte(c)
			continue
		}
		if (c == ',' || c == ';') && isOnlyWSOrCloser(s, i+1) {
			continue
		}
		out.WriteByte(c)
	}
	return out.String()
}

func isOnlyWSOrCloser(s string, from int) bool {
	for j := from; j < len(s); j++ {
		switch s[j] {
		case ' ', '\t', '\n', '\r':
			continue
		case '}', ']':
			return true
		default:
			return false
		}
	}
	return true
}

func normalizeQuotes(s string) string {
	// Replace common smart quotes with ASCII quotes outside strings is
	// undecidable without parsing; the cheap repair handles payloads
	// that used single quotes for the whole document.
	r := strings.NewReplacer("\u201c", `"`, "\u201d", `"`, "\u2018", `'`, "\u2019", `'`)
	return r.Replace(s)
}

// ValidateAgainstSchema checks args against a JSON Schema 2020-12
// subset sufficient for the built-in catalog: object type with
// properties, required, additionalProperties; string/number/integer/
// boolean/array/object types; enum; minimum/maximum; minLength/
// maxLength; minItems/maxItems; items for arrays. Unknown keywords are
// ignored. A nil or empty schema accepts anything.
func ValidateAgainstSchema(args json.RawMessage, schema json.RawMessage) error {
	if len(bytes.TrimSpace(schema)) == 0 || string(bytes.TrimSpace(schema)) == "null" {
		return nil
	}
	var sch map[string]any
	dec := json.NewDecoder(bytes.NewReader(schema))
	dec.UseNumber()
	if err := dec.Decode(&sch); err != nil {
		return fmt.Errorf("bad tool schema: %v", err)
	}
	var v any
	dec2 := json.NewDecoder(bytes.NewReader(args))
	dec2.UseNumber()
	if err := dec2.Decode(&v); err != nil {
		return fmt.Errorf("arguments are not valid JSON: %v", err)
	}
	if err := checkValue(v, sch, "$"); err != nil {
		return err
	}
	return nil
}

func checkValue(v any, sch map[string]any, path string) error {
	if enum, ok := sch["enum"]; ok {
		want, _ := json.Marshal(enum)
		got, _ := json.Marshal(v)
		var wantList []any
		if err := json.Unmarshal(want, &wantList); err == nil {
			for _, e := range wantList {
				ej, _ := json.Marshal(e)
				if string(ej) == string(got) {
					goto checked
				}
			}
			return fmt.Errorf("%s: value %s is not one of %s", path, string(got), string(want))
		}
	}
checked:
	t, _ := sch["type"].(string)
	switch t {
	case "", "any":
		// Accept anything; still check nested object constraints below.
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: want object", path)
		}
		if req, ok := sch["required"].([]any); ok {
			for _, r := range req {
				name, _ := r.(string)
				if _, ok := obj[name]; !ok {
					return fmt.Errorf("%s: missing required property %q", path, name)
				}
			}
		}
		props, _ := sch["properties"].(map[string]any)
		for name, val := range obj {
			psch, ok := props[name]
			if !ok {
				if ap, ok := sch["additionalProperties"]; ok {
					if b, ok := ap.(bool); ok && !b {
						return fmt.Errorf("%s: unexpected property %q", path, name)
					}
				}
				continue
			}
			sub, ok := psch.(map[string]any)
			if !ok {
				continue
			}
			if err := checkValue(val, sub, path+"."+name); err != nil {
				return err
			}
		}
	case "array":
		arr, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s: want array", path)
		}
		if n, ok := numAsInt(sch["minItems"]); ok && len(arr) < n {
			return fmt.Errorf("%s: need at least %d items", path, n)
		}
		if n, ok := numAsInt(sch["maxItems"]); ok && len(arr) > n {
			return fmt.Errorf("%s: need at most %d items", path, n)
		}
		if items, ok := sch["items"].(map[string]any); ok {
			for i, item := range arr {
				if err := checkValue(item, items, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	case "string":
		str, ok := v.(string)
		if !ok {
			return fmt.Errorf("%s: want string", path)
		}
		if n, ok := numAsInt(sch["minLength"]); ok && len(str) < n {
			return fmt.Errorf("%s: string too short (min %d)", path, n)
		}
		if n, ok := numAsInt(sch["maxLength"]); ok && len(str) > n {
			return fmt.Errorf("%s: string too long (max %d)", path, n)
		}
	case "integer":
		if !isInteger(v) {
			return fmt.Errorf("%s: want integer", path)
		}
		if err := checkNumericBounds(v, sch, path); err != nil {
			return err
		}
	case "number":
		if !isNumber(v) {
			return fmt.Errorf("%s: want number", path)
		}
		if err := checkNumericBounds(v, sch, path); err != nil {
			return err
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s: want boolean", path)
		}
	case "null":
		if v != nil {
			return fmt.Errorf("%s: want null", path)
		}
	}
	return nil
}

func numAsInt(v any) (int, bool) {
	switch n := v.(type) {
	case json.Number:
		var i int
		if _, err := fmt.Sscanf(n.String(), "%d", &i); err == nil {
			return i, true
		}
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

func numAsFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case json.Number:
		var f float64
		if _, err := fmt.Sscanf(n.String(), "%g", &f); err == nil {
			return f, true
		}
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}

func isInteger(v any) bool {
	switch n := v.(type) {
	case json.Number:
		s := n.String()
		if strings.ContainsAny(s, ".eE") {
			return false
		}
		var i int64
		_, err := fmt.Sscanf(s, "%d", &i)
		return err == nil
	case float64:
		return n == float64(int64(n))
	case int:
		return true
	}
	return false
}

func isNumber(v any) bool {
	switch n := v.(type) {
	case json.Number:
		var f float64
		_, err := fmt.Sscanf(n.String(), "%g", &f)
		return err == nil
	case float64, int:
		return true
	}
	return false
}

func checkNumericBounds(v any, sch map[string]any, path string) error {
	fv, ok := numAsFloat(v)
	if !ok {
		return nil
	}
	if m, ok := sch["minimum"]; ok {
		if mf, ok := numAsFloat(m); ok && fv < mf {
			return fmt.Errorf("%s: below minimum %v", path, m)
		}
	}
	if m, ok := sch["maximum"]; ok {
		if mf, ok := numAsFloat(m); ok && fv > mf {
			return fmt.Errorf("%s: above maximum %v", path, m)
		}
	}
	return nil
}

// redactionWriteRefusalText is returned when a write-side call carries a
// redaction marker: the marker stands for a value hidden from the model,
// so writing it would persist a placeholder into the workspace.
const redactionWriteRefusalText = "refused: the text contains a redaction marker, which stands for a secret value hidden from you; edit the file with a pattern-based command (for example sed -E with a regex) that does not need the hidden value"

// redactionWriteRefusal refuses write_file, edit_file, apply_patch and
// exec calls whose arguments contain a redaction marker
// (EXCALIBUR_REDACTION_WRITE_GUARD, default on). Output redaction is
// unchanged; this guard only stops the marker from being written back.
// Nested calls through call_tool pass through the same executor path.
func redactionWriteRefusal(name string, args json.RawMessage) string {
	switch name {
	case "write_file", "edit_file", "apply_patch", "exec":
	default:
		return ""
	}
	if !RedactionWriteGuardEnabled() {
		return ""
	}
	var v any
	if err := json.Unmarshal(args, &v); err != nil {
		return ""
	}
	if containsRedactionMarker(v) {
		return redactionWriteRefusalText
	}
	return ""
}

// containsRedactionMarker walks decoded JSON values for marker text.
func containsRedactionMarker(v any) bool {
	switch t := v.(type) {
	case string:
		return vault.ContainsRedactionMarker(t)
	case map[string]any:
		for _, x := range t {
			if containsRedactionMarker(x) {
				return true
			}
		}
	case []any:
		for _, x := range t {
			if containsRedactionMarker(x) {
				return true
			}
		}
	}
	return false
}

// RecoveryClass maps Section 17.2 to an executor decision for a tool
// with intent but no result yet.
type RecoveryClass string

// Recovery decisions.
const (
	RecoverRerun      RecoveryClass = "rerun"
	RecoverCheckProbe RecoveryClass = "check_probe"
	RecoverResendKey  RecoveryClass = "resend_key"
	RecoverUncertain  RecoveryClass = "uncertain"
)

// RecoveryFor returns the crash-recovery behavior for an effect class.
func RecoveryFor(effect types.EffectClass) RecoveryClass {
	switch effect {
	case types.EffectReadOnly:
		return RecoverRerun
	case types.EffectWriteLocal:
		return RecoverCheckProbe
	case types.EffectExec, types.EffectWriteExternal, types.EffectCommunication:
		return RecoverUncertain
	default:
		return RecoverUncertain
	}
}

// RecoveryForIdempotency refines the decision with idempotency: keyed
// effects resend with the same key instead of staying uncertain.
func RecoveryForIdempotency(effect types.EffectClass, idem types.IdempotencyClass) RecoveryClass {
	if idem == types.IdemKeyed {
		return RecoverResendKey
	}
	if idem == types.IdemPure {
		return RecoverRerun
	}
	if idem == types.IdemIdempotent {
		return RecoverCheckProbe
	}
	return RecoveryFor(effect)
}
