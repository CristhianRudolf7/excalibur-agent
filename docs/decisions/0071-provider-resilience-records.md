# Provider resilience and outcome records

Sections 19.5 and 23.2 require transient-provider recovery and auditable trial
outcomes. The model gateway keeps the provider wire contracts unchanged.

The gateway adds `CallMeta.CredentialSwitches`, an optional integer counting
fallback credential switches after confirmed quota exhaustion. The HTTP observer
retains only allow-listed billing identifiers. A minimal probe must confirm
billing evidence before process-local quarantine. The request deadline applies
independently of the budget retry switch.

Retry v2 uses exponential delays with jitter and honors server wait guidance.
Rate limits and server overloads consume the caller's wall budget. Transport
failures retain a six-attempt bound per endpoint, allowing configured endpoint
fallbacks to run. An output-limit cut receives one concise continuation before
the existing truncation recovery. Switches and defaults are documented in
`docs/development/agent-quality.md`.

Trial records add optional finish detail, actual command outcomes and output
preview metadata. Completed trial finals cannot be overwritten by later error
cleanup. Reports distinguish infrastructure trials from scored work, show both
rates, and preserve the raw reward when including infrastructure trials.
Infrastructure detection is independent of reward. Reports refresh older indexes
from retained trial evidence; index-only exports retain their saved metadata.

The added CLI seams only propagate gateway metadata and completed finish detail
into existing recording sites. No connector, tool protocol, Harbor invocation,
shared core interface or durable run-state vocabulary changes.
