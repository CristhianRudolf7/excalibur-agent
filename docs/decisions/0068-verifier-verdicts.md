# 0068: Require an independent verifier verdict

The director requested bounded recovery for independent verification under
Sections 7.5, 8.7 and 18.1. A provider error or absent verdict cannot establish
that work is verified. The prior permissive fallback could accept both.

With `EXCALIBUR_VERIFIER_VERDICT` enabled (default), verifier hooks reserve
8192 output tokens, request compact structured responses, and retry once after
truncation or an absent verdict using a smaller prompt. The strong verifier
requires an overall `VERDICT` line; per-item lines alone do not establish an
overall verdict. Exhausted recovery returns an unverified pending through the
existing bounded finish gate. Refinement retains deterministic criteria on
failure; requirement review retains the requirement. Neither fallback grants
verification. Cancellation and other provider errors do not incur this retry.

Metadata-only checks cannot verify behavioural requirements. Deterministic
filtering runs before filename substring matching, supplemented by explicit
instructions for semantic review. Mixed checks still require independent
assessment of the exercised behaviour and assertions. Disabling the switch
restores the earlier budgets, parsing and evidence recognition.

Call sites travel as local context metadata, leaving provider wire contracts
unchanged. Trial records add `call_site`, `usage_available`, and
`finish_reason_source`. Existing token fields preserve all returned usage on
success and error paths. A typed truncation is recorded as inferred `length`;
otherwise the finish reason is explicitly `unavailable`.

## Discrepancy and alternative

The protected provider response contract has no finish-reason field. The
provider adapters and gateway can discard usage when returning an error.
Exact raw finish reasons and discarded usage cannot be recovered by callers.
This implementation records their absence explicitly rather than fabricating
values. Extending those contracts or observing provider transports would be
necessary for exact raw error telemetry; both are outside this assignment's
connector restriction. No shared contract or dependency changes are made.

Trial records also include the requested `max_output_tokens` and the
`verifier_verdict` switch value, so auditing can establish the active
configuration without reconstructing prompt hashes. These are harness request
values; an operator's explicit role routing cap can override the request in
the existing gateway.
