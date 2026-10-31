// Durable journal (Sections 17.1, 17.2, H3 engine half).
//
// The journal is the append-only log of every run: orchestration
// replays it instead of re-executing nondeterministic steps, so work
// resumes after crashes without duplicating effects. The write-ahead
// rule is enforced by the call order: no effect runs without a
// confirmed tool_intent, nothing counts as done without a confirmed
// tool_result, and the model response is recorded before any tool
// runs, so a restart reuses the recorded response instead of calling
// the model again.
//
// Storage has two implementations sharing the Store interface: the
// in-memory MemStore for tests and the embedded profile, and the
// SQLStore over the porter (store.Writer lane journal plus a read
// source) over the Appendix A journal table.
package journal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Entry types of Section 17.1.
type EntryType string

// Journal entry types.
const (
	TypeRunStarted        EntryType = "run_started"
	TypeModelRequest      EntryType = "model_request"
	TypeModelResponse     EntryType = "model_response"
	TypeToolIntent        EntryType = "tool_intent"
	TypeToolResult        EntryType = "tool_result"
	TypeDirectiveReceived EntryType = "directive_received"
	TypeDirectiveApplied  EntryType = "directive_applied"
	TypeCompaction        EntryType = "compaction"
	TypeStateChange       EntryType = "state_change"
	TypeCheckpoint        EntryType = "checkpoint"
)

// CheckpointEvery is the run-state snapshot cadence (Appendix B:
// every 20 steps) so recovery rebuilds from the latest checkpoint
// plus the trailing entries.
const CheckpointEvery = 20

// Entry is one append-only journal record.
type Entry struct {
	Seq            uint64    `json:"seq"`
	RunID          string    `json:"run_id"`
	Type           EntryType `json:"type"`
	Payload        []byte    `json:"payload,omitempty"`
	BlobRef        string    `json:"blob_ref,omitempty"`
	State          string    `json:"state,omitempty"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	At             time.Time `json:"at"`
}

// Store is the append-only journal storage. Appends assign the next
// sequence number for the run; entries are never updated or deleted
// (retention compacts them in the store package, 4.7).
type Store interface {
	Append(ctx context.Context, e Entry) (Entry, error)
	List(ctx context.Context, runID string) ([]Entry, error)
}

// MemStore is the in-memory Store.
type MemStore struct {
	mu   sync.Mutex
	rows map[string][]Entry
}

// NewMemStore returns an empty MemStore.
func NewMemStore() *MemStore { return &MemStore{rows: map[string][]Entry{}} }

// Append implements Store.
func (m *MemStore) Append(_ context.Context, e Entry) (Entry, error) {
	if e.RunID == "" {
		return Entry{}, fmt.Errorf("journal: append needs a run id")
	}
	if e.Type == "" {
		return Entry{}, fmt.Errorf("journal: append needs an entry type")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e.Seq = uint64(len(m.rows[e.RunID]))
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	m.rows[e.RunID] = append(m.rows[e.RunID], e)
	return e, nil
}

// List implements Store: entries in sequence order.
func (m *MemStore) List(_ context.Context, runID string) ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]Entry(nil), m.rows[runID]...)
	return out, nil
}

// Reader is the read half the SQLStore needs: satisfied by
// *store.DB.Read and the fake store.
type Reader interface {
	Read(ctx context.Context, fn store.ReadFunc) error
}

// SQLStore persists the journal in the Appendix A journal table
// through the porter: writes go on the journal lane (BEGIN
// IMMEDIATE, group commit), reads use the pooled connections.
type SQLStore struct {
	W   store.Writer
	R   Reader
	Now func() time.Time
}

// Append implements Store. Sequence assignment happens inside the
// porter write, so concurrent appends to one run stay ordered.
func (s *SQLStore) Append(ctx context.Context, e Entry) (Entry, error) {
	if e.RunID == "" {
		return Entry{}, fmt.Errorf("journal: append needs a run id")
	}
	if e.Type == "" {
		return Entry{}, fmt.Errorf("journal: append needs an entry type")
	}
	at := e.At
	if at.IsZero() {
		if s.Now != nil {
			at = s.Now().UTC()
		} else {
			at = time.Now().UTC()
		}
	}
	var assigned Entry
	err := s.W.Write(ctx, store.PriorityJournal, func(ctx context.Context, ex store.Execer) error {
		var max *int64
		row := ex.QueryRow(ctx, "SELECT MAX(seq) FROM journal WHERE run_id=?", e.RunID)
		var v *int64
		if err := row.Scan(&v); err != nil {
			return err
		}
		_ = max
		var next int64
		if v != nil {
			next = *v + 1
		}
		ms := at.UnixMilli()
		if _, err := ex.Exec(ctx,
			`INSERT INTO journal(run_id,seq,type,payload,blob_digest,status,idempotency_key,created_at)
			 VALUES(?,?,?,?,?,?,?,?)`,
			e.RunID, next, string(e.Type), string(e.Payload),
			e.BlobRef, e.State, e.IdempotencyKey, ms); err != nil {
			return err
		}
		assigned = e
		assigned.Seq = uint64(next)
		assigned.At = at
		return nil
	})
	if err != nil {
		return Entry{}, err
	}
	return assigned, nil
}

// List implements Store.
func (s *SQLStore) List(ctx context.Context, runID string) ([]Entry, error) {
	var out []Entry
	err := s.R.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx,
			`SELECT seq,type,payload,blob_digest,status,idempotency_key,created_at
			 FROM journal WHERE run_id=? ORDER BY seq`, runID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e Entry
			var typ, payload, blob, state, key string
			var seq int64
			var ms int64
			if err := rows.Scan(&seq, &typ, &payload, &blob, &state, &key, &ms); err != nil {
				return err
			}
			e.Seq = uint64(seq)
			e.RunID = runID
			e.Type = EntryType(typ)
			if payload != "" {
				e.Payload = []byte(payload)
			}
			e.BlobRef = blob
			e.State = state
			e.IdempotencyKey = key
			e.At = time.UnixMilli(ms).UTC()
			out = append(out, e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Payload helpers. Payloads are small JSON summaries; large outputs
// live in blobs and only their digest travels here (17.1).

// MarshalPayload encodes a payload value.
func MarshalPayload(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte(fmt.Sprintf(`{"encode_error":%q}`, err.Error()))
	}
	return raw
}

// HashRequest returns the stable hash of a model request recorded
// with the model_request entry (7.3).
func HashRequest(canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// ModelRequestPayload is the model_request body.
type ModelRequestPayload struct {
	Hash string `json:"hash"`
}

// ModelResponsePayload is the model_response summary. Calls carries
// the full requested calls so recovery can replay a step whose
// intents never landed (kill between the response and the intent
// entries): the effect provably never ran, so replaying with a fresh
// key is safe.
type ModelResponsePayload struct {
	Hash      string         `json:"hash"`
	Tools     []string       `json:"tools,omitempty"`
	FinalText bool           `json:"final_text"`
	UsageIn   int64          `json:"usage_in,omitempty"`
	UsageOut  int64          `json:"usage_out,omitempty"`
	Calls     []RecordedCall `json:"calls,omitempty"`
}

// RecordedCall is one requested tool call stored with the response.
type RecordedCall struct {
	CallID string          `json:"call_id"`
	Tool   string          `json:"tool"`
	Args   json.RawMessage `json:"args,omitempty"`
}

// ToolIntentPayload is the tool_intent body: everything recovery
// needs to classify the pending effect (17.2).
type ToolIntentPayload struct {
	CallID         string                 `json:"call_id"`
	Tool           string                 `json:"tool"`
	Args           json.RawMessage        `json:"args,omitempty"`
	IdempotencyKey string                 `json:"idempotency_key,omitempty"`
	Effect         types.EffectClass      `json:"effect,omitempty"`
	Idempotency    types.IdempotencyClass `json:"idempotency,omitempty"`
}

// ToolResultPayload is the tool_result body.
type ToolResultPayload struct {
	CallID         string `json:"call_id"`
	Tool           string `json:"tool"`
	Status         string `json:"status"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	Transient      bool   `json:"transient,omitempty"`
}

// StateChangePayload records a run transition (7.2).
type StateChangePayload struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason,omitempty"`
}

// CheckpointPayload snapshots the engine state every CheckpointEvery
// steps for fast rebuild (17.1).
type CheckpointPayload struct {
	Step  int             `json:"step"`
	State json.RawMessage `json:"state,omitempty"`
}

// Recorder writes the typed entries of one run, enforcing the
// write-ahead order at the call site: intent before effect, result
// after, response before tools.
type Recorder struct {
	Store Store
	RunID string
}

// Record appends one raw entry.
func (r *Recorder) Record(ctx context.Context, t EntryType, payload []byte, blob, state, key string) (Entry, error) {
	return r.Store.Append(ctx, Entry{
		RunID: r.RunID, Type: t, Payload: payload,
		BlobRef: blob, State: state, IdempotencyKey: key,
		At: time.Now().UTC(),
	})
}

// RunStarted records entry creation with its card.
func (r *Recorder) RunStarted(ctx context.Context, card any) (Entry, error) {
	return r.Record(ctx, TypeRunStarted, MarshalPayload(card), "", "", "")
}

// ModelRequest records the request hash before calling.
func (r *Recorder) ModelRequest(ctx context.Context, hash string) (Entry, error) {
	return r.Record(ctx, TypeModelRequest, MarshalPayload(ModelRequestPayload{Hash: hash}), "", "", "")
}

// ModelResponse records the full response before executing tools.
func (r *Recorder) ModelResponse(ctx context.Context, p ModelResponsePayload) (Entry, error) {
	return r.Record(ctx, TypeModelResponse, MarshalPayload(p), "", "", "")
}

// ToolIntent records the intent before any effect, with the
// idempotency key.
func (r *Recorder) ToolIntent(ctx context.Context, p ToolIntentPayload) (Entry, error) {
	return r.Record(ctx, TypeToolIntent, MarshalPayload(p), "", "", p.IdempotencyKey)
}

// ToolResult records the outcome once the tool finishes.
func (r *Recorder) ToolResult(ctx context.Context, p ToolResultPayload) (Entry, error) {
	return r.Record(ctx, TypeToolResult, MarshalPayload(p), "", p.Status, p.IdempotencyKey)
}

// StateChange records a transition.
func (r *Recorder) StateChange(ctx context.Context, from, to types.RunState, reason string) (Entry, error) {
	return r.Record(ctx, TypeStateChange,
		MarshalPayload(StateChangePayload{From: string(from), To: string(to), Reason: reason}),
		"", string(to), "")
}

// Checkpoint snapshots the engine state.
func (r *Recorder) Checkpoint(ctx context.Context, step int, state json.RawMessage) (Entry, error) {
	return r.Record(ctx, TypeCheckpoint, MarshalPayload(CheckpointPayload{Step: step, State: state}), "", "", "")
}

// Directive records a received or applied directive.
func (r *Recorder) Directive(ctx context.Context, applied bool, payload any) (Entry, error) {
	t := TypeDirectiveReceived
	if applied {
		t = TypeDirectiveApplied
	}
	return r.Record(ctx, t, MarshalPayload(payload), "", "", "")
}

// ShouldCheckpoint reports whether step needs a checkpoint entry.
func ShouldCheckpoint(step int) bool { return step > 0 && step%CheckpointEvery == 0 }

// PendingIntent pairs an intent without a result with its sequence.
type PendingIntent struct {
	Intent ToolIntentPayload
	Seq    uint64
}

// PendingIntents scans entries for tool_intent rows with no matching
// tool_result (matched by call id, falling back to the idempotency
// key). Results before intents cannot happen under the write-ahead
// rule and are ignored.
func PendingIntents(entries []Entry) []PendingIntent {
	done := map[string]bool{}
	for _, e := range entries {
		if e.Type != TypeToolResult {
			continue
		}
		var p ToolResultPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			continue
		}
		if p.CallID != "" {
			done["call:"+p.CallID] = true
		}
		if p.IdempotencyKey != "" {
			done["key:"+p.IdempotencyKey] = true
		}
	}
	var out []PendingIntent
	for _, e := range entries {
		if e.Type != TypeToolIntent {
			continue
		}
		var p ToolIntentPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			continue
		}
		if p.CallID != "" && done["call:"+p.CallID] {
			continue
		}
		if p.CallID == "" && p.IdempotencyKey != "" && done["key:"+p.IdempotencyKey] {
			continue
		}
		out = append(out, PendingIntent{Intent: p, Seq: e.Seq})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

// MissingIntents synthesizes pending intents for recorded response
// calls that never reached tool_intent: the kill landed between the
// model_response entry and the intent entries. The effect provably
// never ran (intent precedes every effect), so these synthesize with
// an empty key and effect metadata filled by the caller.
func MissingIntents(entries []Entry) []PendingIntent {
	intended := map[string]bool{}
	for _, e := range entries {
		switch e.Type {
		case TypeToolIntent:
			var p ToolIntentPayload
			if err := json.Unmarshal(e.Payload, &p); err == nil && p.CallID != "" {
				intended[p.CallID] = true
			}
		case TypeToolResult:
			var p ToolResultPayload
			if err := json.Unmarshal(e.Payload, &p); err == nil && p.CallID != "" {
				intended[p.CallID] = true
			}
		}
	}
	var out []PendingIntent
	for _, e := range entries {
		if e.Type != TypeModelResponse {
			continue
		}
		var p ModelResponsePayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			continue
		}
		for _, c := range p.Calls {
			if c.CallID == "" || intended[c.CallID] {
				continue
			}
			out = append(out, PendingIntent{
				Intent: ToolIntentPayload{CallID: c.CallID, Tool: c.Tool, Args: c.Args},
				Seq:    e.Seq,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

// ModelReplay describes what the model leg of the journal holds: the
// last request hash and whether its response is already recorded. A
// request without a response has no effects and is simply repeated
// (with its cost); a recorded response is reused and the model is
// NOT called again for that step (7.3, 17.2).
type ModelReplay struct {
	LastRequestHash string
	HasResponse     bool
	Responses       int
	Requests        int
}

// ScanModelLeg counts requests and responses and reports whether the
// trailing request still needs its call.
func ScanModelLeg(entries []Entry) ModelReplay {
	var rep ModelReplay
	for _, e := range entries {
		switch e.Type {
		case TypeModelRequest:
			rep.Requests++
			var p ModelRequestPayload
			if err := json.Unmarshal(e.Payload, &p); err == nil {
				rep.LastRequestHash = p.Hash
			}
			rep.HasResponse = false
		case TypeModelResponse:
			rep.Responses++
			rep.HasResponse = true
		}
	}
	return rep
}

// PendingAction tells recovery what to do with a pending intent.
type PendingAction string

// Pending actions.
const (
	// ActionRerun repeats the effect: safe for pure and idempotent
	// tools and for keyed tools resent with the same key.
	ActionRerun PendingAction = "rerun"
	// ActionDone marks the step done: the probe shows the effect
	// already happened.
	ActionDone PendingAction = "done"
	// ActionUncertain never repeats automatically: the effect is
	// non-idempotent and no probe confirms it. The run goes to
	// recovering and the decision is taken with information (diff of
	// the shadow snapshot, destination state) or asked (17.2).
	ActionUncertain PendingAction = "uncertain"
)

// PendingDecision is the 17.2 treatment of one interrupted effect.
type PendingDecision struct {
	Intent ToolIntentPayload
	Action PendingAction
	Reason string
}

// Prober checks whether an interrupted effect already happened.
// It mirrors tools.Prober without importing internal/tools, so the
// journal stays usable from every package.
type Prober interface {
	Probe(ctx context.Context, tool string, args json.RawMessage) (bool, error)
}

// ResolvePending classifies one pending intent (17.2):
//
//   - pure / read-only: rerun.
//   - idempotent: probe when available (done when the postcondition
//     holds, rerun otherwise); rerun when no probe exists, since
//     repeating an idempotent effect is safe by definition.
//   - keyed: rerun with the same idempotency key; the external
//     service deduplicates, so one effect lands.
//   - non-idempotent: probe when the tool declares one (done when it
//     holds); otherwise uncertain and never repeated.
func ResolvePending(ctx context.Context, p PendingIntent, prober Prober) PendingDecision {
	in := p.Intent
	if in.Idempotency == "" {
		in.Idempotency = inferIdempotency(in.Effect)
	}
	switch in.Idempotency {
	case types.IdemPure:
		return PendingDecision{Intent: in, Action: ActionRerun, Reason: "pure effect reruns"}
	case types.IdemIdempotent:
		if prober == nil {
			return PendingDecision{Intent: in, Action: ActionRerun, Reason: "idempotent effect without probe repeats safely"}
		}
		done, err := prober.Probe(ctx, in.Tool, in.Args)
		if err != nil {
			return PendingDecision{Intent: in, Action: ActionUncertain, Reason: "idempotent probe failed: " + err.Error()}
		}
		if done {
			return PendingDecision{Intent: in, Action: ActionDone, Reason: "postcondition holds: effect already happened"}
		}
		return PendingDecision{Intent: in, Action: ActionRerun, Reason: "postcondition unmet: effect did not happen"}
	case types.IdemKeyed:
		return PendingDecision{Intent: in, Action: ActionRerun, Reason: "keyed effect resent with the same idempotency key"}
	default:
		if prober == nil {
			return PendingDecision{Intent: in, Action: ActionUncertain, Reason: "non-idempotent effect without probe never repeats automatically"}
		}
		done, err := prober.Probe(ctx, in.Tool, in.Args)
		if err != nil {
			return PendingDecision{Intent: in, Action: ActionUncertain, Reason: "non-idempotent probe failed: " + err.Error()}
		}
		if done {
			return PendingDecision{Intent: in, Action: ActionDone, Reason: "probe shows the non-idempotent effect already happened"}
		}
		return PendingDecision{Intent: in, Action: ActionUncertain, Reason: "non-idempotent effect unconfirmed: needs information or a question"}
	}
}

// ResolveAll classifies every pending intent of a run.
func ResolveAll(ctx context.Context, pendings []PendingIntent, prober Prober) []PendingDecision {
	out := make([]PendingDecision, 0, len(pendings))
	for _, p := range pendings {
		out = append(out, ResolvePending(ctx, p, prober))
	}
	return out
}

func inferIdempotency(e types.EffectClass) types.IdempotencyClass {
	switch e {
	case types.EffectReadOnly:
		return types.IdemPure
	case types.EffectWriteLocal:
		return types.IdemIdempotent
	case types.EffectWriteExternal, types.EffectCommunication:
		return types.IdemKeyed
	default:
		return types.IdemNonIdempotent
	}
}
