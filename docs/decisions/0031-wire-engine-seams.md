# 0031 Wire engine seams and closest-feasible readings

- Interrupt model: per-step cancellable contexts plus run flags. The
  architecture (7.2, 15.8) requires the in-flight model call and tool
  to stop on interrupt with toolOnly vs full semantics. The engine
  previously shared one context across all steps, so a tool-only
  cancel would poison later steps. Reading: each step derives a child
  context stored as the current step cancel; RequestInterrupt cancels
  it and sets flags. Full exits the loop gracefully without failing
  (the orchestrator owns the state); tool-only injects a note and the
  run continues. Alternative (separate model/tool contexts plumbed
  through every ToolRunner) was rejected as invasive for the same
  guarantee.
- Shared history bridge: `tools.FileDeps.History` changed from
  `*MemoryHistory` to the `FileHistoryStore` interface (same methods).
  Existing constructors pass `*MemoryHistory`, which still satisfies
  it, so the change is additive. The workspace bridge records under
  the same version id in both stores (`RecordWithID` is a new additive
  workspace method; duplicates are ignored so the mirror is
  idempotent). Alternative (tools importing workspace) was rejected:
  it would create an import cycle (workspace already imports tools).
- `RecoverProber.Probe` now takes `json.RawMessage` instead of
  `[]byte` to satisfy `journal.Prober`. `json.RawMessage` is defined
  over `[]byte` and all call sites pass message args, so no caller
  changes. This fixes a latent seam mismatch, not a behavior change.
- Codeintel tools always register, even without a graph opener or
  language servers. Rationale (13.3): deferred tools must be
  searchable; invoking without collaborators reports the missing owner
  instead of "unknown tool". No prefix impact: registration happens
  after the prefix snapshot via deferred marks.
- Adoption goes through recovering when the 7.2 diagram requires it
  (running cannot move directly to queued). The helper tries the
  direct edge first, then recovering, then the target.
- Eval never opens the vault (23.2): the exec engine path passes no
  vault for the eval profile; keys arrive by environment. Delegate
  tasks scope env resolution to their project; general-scope entries
  do not resolve for delegates. Provider credentials resolve without
  project scoping (entries are credential-typed, not project-scoped).
- File-byte history is kept exact for reversibility and is not
  redacted: the on-disk file already holds those bytes, and redacting
  blobs would corrupt `revert_edit`. Redaction (values, base64/URL
  variants, known patterns) applies to tool outputs before truncation,
  the journal, context, telemetry, ATIF and blobs. Recorded as the
  closest feasible reading of "history and snapshots pass through the
  filter": output-derived records are filtered; file bytes stay exact.
