# Actionable finish refusals

The director requested actionable finish refusals after evaluation auditing
identified repeated finish submissions without intervening corrective work.
This extends the explicit finish protocol in decision 0055 and Sections 7.5,
8.3 and 8.5.

Finish refusals carry an item ID, missing evidence and a concrete check.
Unchanged items are omitted on subsequent attempts; the response identifies
resolved items and supplies a next action. Every reason remains in the journal
and the final explicit finish record, including unchanged reasons.

On an extracted item's third refusal, a verifier-role call reviews only the
literal instruction and the extracted requirement. It receives neither the
executor's proposed evidence nor its reasoning. A withdrawal requires an
explicit false-requirement verdict, a reason and an exact quote present in the
instruction. Invalid responses, unavailable models and ambiguous requirements
retain the item. Review occurs once per item and instruction, keeping cost
bounded. A changed instruction invalidates previous withdrawals. Core criteria,
freshness, running sessions, instruction revisions and failed commands cannot
be withdrawn through this mechanism.

This is an explicit extension of the extraction/refinement behavior: an
extraction mistake can be withdrawn after persistent refusal, while executable
evidence still determines whether actual requirements pass. The alternative
was to repeat the same refusal until budget exhaustion. There is no relaxation
based solely on refusal count or on the executor's completion claim.

`EXCALIBUR_ACTIONABLE_REFUSALS=off` restores legacy refusal wording and artifact
aggregation. No shared interface or connector protocol changes are required.
The engine writes the additive `finish_refusal` journal entry type using the
existing recorder API; readers that do not consume this audit entry ignore it.

Audit entries include an instruction hash. A resumed explicit finish path
reconstructs its counters, previous checklist and review decisions from these
entries before validating completion, preserving the threshold across restart.
