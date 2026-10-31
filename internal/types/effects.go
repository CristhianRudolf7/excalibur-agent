package types

// EffectClass classifies what a tool operation may change (Sections
// 13.1 and 17.2). The class drives recovery after a crash: pure and
// read-only operations rerun, idempotent ones check their postcondition,
// keyed ones resend with the same key, non-idempotent ones never repeat
// automatically.
type EffectClass string

// Effect classes.
const (
	EffectReadOnly      EffectClass = "read_only"
	EffectWriteLocal    EffectClass = "write_local"
	EffectExec          EffectClass = "exec"
	EffectWriteExternal EffectClass = "write_external"
	EffectCommunication EffectClass = "communication"
)

// IdempotencyClass classifies repeat behavior (Sections 13.1, 17.2).
type IdempotencyClass string

// Idempotency classes.
const (
	IdemPure          IdempotencyClass = "pure"
	IdemIdempotent    IdempotencyClass = "idempotent"
	IdemKeyed         IdempotencyClass = "keyed"
	IdemNonIdempotent IdempotencyClass = "non_idempotent"
)

// CancelMode describes how a tool call stops on interruption
// (Section 13.1).
type CancelMode string

// Cancel modes.
const (
	CancelImmediate       CancelMode = "immediate"
	CancelGraceful        CancelMode = "graceful"
	CancelAfterCompletion CancelMode = "after_completion"
	CancelCooperative     CancelMode = "cooperative"
)
