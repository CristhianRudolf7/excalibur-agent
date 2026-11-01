# Exhausted provider credentials

Sections 19.5 and 7.2 distinguish provider availability from task correctness.
The director's W16 policy extends W13: a permanent billing or credit failure
quarantines that credential for the lifetime of the process. Transient rate
limits keep W13's wall-clock retries on the remaining credential. Retrying a
known exhausted fallback must never turn an otherwise recoverable primary
rate limit into a work failure.

The model-layer HTTP observer inspects bounded error bodies before adapter
normalization drops error codes, then restores the body for the adapter.
HTTP 402 and explicit quota/billing/credit markers count as permanent; ordinary
429 rate limits, authentication errors, and transport failures do not.
Credential identity uses an endpoint-scoped SHA-256 digest in process memory.
Only the first quarantine logs a fixed message, with no key, digest or error
body. Newly constructed provider wrappers share quarantine state. In-flight
requests may finish after another request quarantines the same credential.
Streaming never replays an emitted response.

When all configured credentials are exhausted, a typed
`provider_quota_exhausted` error parks the engine in `waiting(capability)`.
Exec records `waiting` with reason and model-call error class
`provider_quota_exhausted`; evaluation indexing preserves that infrastructure
category. This uses existing run states without changing shared contracts.
The alternative, `failed/work`, incorrectly attributes provider credit
availability to the task. Restoring credit requires restarting the process,
as explicitly required by the lifetime quarantine policy.

`EXCALIBUR_EXHAUSTED_KEYS=0` disables quarantine for comparison. W13's parent
switch `EXCALIBUR_BUDGET_RETRIES=0` restores the legacy provider construction,
including its original fallback behavior, and consequently bypasses this
observer. Defaults enable both policies. No connector, wire protocol, dependency
or public interface changes are needed.

This worktree initially preceded W13. Commits d2f29c2 and 654aa91 were applied
as prerequisites; their documentation was combined with the existing W10
section. Work remains on the assigned worktree branch.

Full verification exposed a W13 integration race: a deadline between steps (or
inside a model retry wait) could leave the engine running or failed instead of
paused on budget exhaustion. The engine now pauses on deadline in both places,
recording the transition with a cancellation-independent context. The existing
exec wall-budget regression and an already-expired engine deadline test cover
this fix. It preserves the specified wall-clock retry boundary.

Integration retains the existing bounded deadline persistence helper and W12
exec timeout reporting. The duplicate W13 retry decision is consolidated in
decision 0065; shared interfaces and provider wire contracts remain unchanged.
