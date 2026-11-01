# 0063: Interrupted exec records and verified finish guidance

The director requests explicit interruption records and prompt completion once
all extracted criteria have passing checks. This supplements Sections 6.5, 7.3,
7.5, 17.5, 18 and 22, with evaluation records under Section 23.2 and Appendix F.

Exec artifacts use `stopped_by_timeout` for a signal or expired context, with
exit code 4. This is a record-level state, not a new durable engine state or a
change to shared state transitions. Delegated task callers retain their
paused/canceled outcome states so task status never treats an interruption as
a waiting question. It deliberately replaces the previous
paused/canceled exec record mapping for these interruptions. Ordinary budget
pauses and explicit engine cancellation outcomes retain their existing mapping.
The reason distinguishes cancellation from deadline expiry in the emergency
snapshot. The command catches SIGINT and SIGTERM and allows at most two seconds
of cleanup before exit. An independent snapshot writer uses synchronized metrics
and trajectory writers, so cancellation-resistant work cannot prevent records.
SIGKILL and filesystem failure cannot guarantee final records.

The completion prompt is advisory, defaults on, and is disabled with
`EXCALIBUR_PROMPT_FINISH=off`. It observes existing executable evidence and the
existing deterministic verification and artifact gates before the next model
turn. It makes no extra model or tool calls, skips vacuous criteria, and never
bypasses the normal finish confirmation or independent verifier. Repeated checks
with identical arguments and output are flagged when no recorded mutation or
criterion artifact metadata change occurred. The first all-checks-pass timestamp
is immutable and persists incrementally in `prompt_finish.json`; it describes
agent-owned checks, not an external evaluator's verdict.

The alternative was forcing automatic completion. It was rejected because a
passing agent check does not prove that all semantic requirements are covered.
No connectors or dependencies change.
