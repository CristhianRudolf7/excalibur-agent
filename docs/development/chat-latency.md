# Chat latency

## Intake

Conversational daemon intake uses deterministic preanalysis before asking a
model. `EXCALIBUR_INTAKE_GATED_TRIAGE` defaults to `true`: only control
intents, task/run/ULID references, or the existing status vocabulary can
trigger the decider. Other messages reach the master turn with conversation
triage and `Decided=false`. Slash commands bypass classification as before.
Set the switch to `false` to restore classification for all non-slash messages.
Unset or invalid boolean values use the default.

The existing broad control lexicon is preserved, including `what is` and
`how is`; general questions containing these phrases can still be candidates.
The gate does not alter eval or Terminal-Bench execution paths.

`EXCALIBUR_INTAKE_SINGLE_QUESTION` defaults to `true`. The model answers
only the request-type question when there is at most one scope. A single
scope is filled deterministically; multiple scope options retain their
scope question. Durable-instruction and clarification questions are omitted;
`NeedsClarify` uses the existing deterministic `models.ShouldAsk` signals.
The triage fields remain compatible. Set this switch to `false` to restore
the four-question sequence.

`EXCALIBUR_INTAKE_DECIDER_TIMEOUT` defaults to `2500ms` and accepts Go
durations. Intake enforces the deadline even if a decider ignores context.
On timeout or cancellation, it keeps the existing deterministic triage
metadata, sets `Decided=false`, and delivers the message to the master.
A late result cannot execute a fast-lane control. Decider errors continue to
use the existing deterministic fallback. Invalid or negative durations use
the default; `0` explicitly disables the deadline for rollback. Deciders
should honor cancellation to release provider resources promptly.

`EXCALIBUR_INTAKE_FAST_LANE_GUARD` defaults to `true`: natural-language
fast-lane replies require a deterministic control intent or task/run reference,
in addition to the existing completed classification and confidence checks.
A world command classified as `quick_action` cannot become a run-status reply
from that label alone. The daemon preserves the `quick_action` type rather
than relabeling it as `status_query`. Set the guard to `false` to restore the
previous admission rule. Slash commands and mixed control/content splitting
retain their existing behavior; unresolved targets still reach the master.

`EXCALIBUR_INTAKE_TIMING` defaults to `true`. Each message routed through
the fast lane/master boundary emits one structured INFO event, `intake timing`:

- `gated`: classification was skipped by the candidate gate (slash commands
  bypass classification independently and report `false`).
- `decider_calls`: number of `Classify` attempts, including timeouts (zero or
  one). This counts intake invocations, not internal questions in rollback mode.
- `decider_ms`: elapsed classification wait in milliseconds, zero if skipped.
- `fast_lane`: the fast lane handled a control/reply, including mixed messages
  whose remaining content goes to a master turn.

The event includes no message text, identifiers, prompts, or credentials.
Set `EXCALIBUR_INTAKE_TIMING=false` to disable these events. Duplicate message
receipts are not routed a second time and do not emit another timing event.

Verification uses fakes only. Master tests cover bilingual candidate vocabulary,
zero-classification turn delivery through synchronous and deferred intake,
deadline fallback including an uncooperative decider, late-result isolation,
world-action misclassification, preserved control/mixed-message routing, and
one timing event per routed message. Daemon tests count the single question,
legacy rollback questions, multi-scope questions, deterministic clarification,
error fallback, and the preserved quick-action type.

The touched packages pass `go vet` and `go test -race`; full Linux, Darwin, and
Windows builds pass. The diff against the round's starting commit changes only
the assigned intake/decider files, their tests, and this document.

### Status questions while nothing runs

`EXCALIBUR_INTAKE_REQUIRE_ACTIVE_WORK` (default on) skips classification for
status-style candidates (no control intent other than `status`, no task or run
reference) while no run is active. With nothing running, the fast lane has no
status to serve, so phrases such as "what is" in ordinary questions go straight
to the conversational turn. Set it to `0` to classify them anyway.

## Turn

The daemon's chat and voice profile answers greetings, small talk and general
knowledge directly. Tools remain available for missing information and actions.
Clear commands should execute directly; independent tool calls should share one
step. Delegation, untrusted-document handling, exact citation markers and a final
text answer retain their existing instructions.

`EXCALIBUR_CHAT_TOOL_ECONOMY` defaults ON. Set it to `0`, `false` or `off`
(case insensitive, surrounding whitespace ignored) to restore the exact previous
base profile string. `TestAssistantToolEconomyProfile` checks both profiles and
all retained instructions.

`EXCALIBUR_ROBOT_TOOL_ECONOMY` also defaults ON with the same OFF values.
Its description directs explicit moves/home/gripper commands to execute directly;
look/locate remain appropriate for vision and pick and place. The tool still
checks approval internally, fails closed on missing policy and requires explicit
approval of each exact action when enabled. A denied action tells the model to
ask for direction, distance and speed approval before retrying with
`approved:true`. STOP, freedrive approval and all bridge safety limits are
unchanged. Successful move/gripper results preserve the bridge fields and append
`state` from the state refresh already performed by `Service.Call`, saving a
follow-up model/tool round trip. If that refresh fails, `state` is null and
`state_error` explains why; stale state is not presented as a fresh observation.
OFF restores the old description, refusal text and result payload.
`TestToolEconomyDescription`, `TestToolApprovalRefusalExplainsNextStep` and
`TestToolActionResultIncludesPostActionState` cover these changes with simulated
bridges; the existing approval, STOP and cancellation tests still apply.

`EXCALIBUR_CHAT_TURN_TIMING` defaults ON with the same OFF values. Each
`CompleteTurn` invocation emits one structured `slog.Info("turn timing", ...)`
record, including canceled, failed, disabled and unconfigured turns. Fields are
`model_calls`, `model_ms`, `tool_calls`, `tool_ms`, `total_ms` and `tool_names`.
Model counters measure real endpoint invocations (including failed calls), not
the synthetic subagent acknowledgement or provider retries inside the gateway.
Tool counters measure calls to the assistant runner, including failed/denied
attempts and browser calls. Tool names are sorted with one entry per call;
unavailable names are replaced with `unavailable` to avoid logging arbitrary
model text. Durations are summed before converting to whole milliseconds.
Parallel tool durations can sum to more than total wall time. Total time includes
snapshot construction and turn cleanup, but excludes intake classification and
queueing before `CompleteTurn`. No prompts, arguments, results, provider errors
or credentials are logged by this record. OFF skips collection and logging.

`TestAssistantTurnTiming` runs fake model/tool turns, including parallel calls,
failures and early returns, and verifies one record, counts, elapsed sums and
content exclusion. `TestAssistantTimingDurationSums` verifies exact fractional
duration sums and stable tool-name order. `TestAssistantTimingSyntheticReplyAndFailedCalls`
checks interactive lane selection, canceled calls, synthetic acknowledgements
and safe accounting of an unavailable tool.

Eval isolation was checked by reading the separate snapshot builder in
`internal/cli/exec_engine.go`: `exec --profile eval` uses a child-role snapshot
with no `AgentProfile` assignment. Its `engine.DefaultBuilder` consumes that
snapshot, never `assistantTurns.snapshot`. The chat profile is assigned only in
`internal/daemon/assistant_turns.go`; robot definitions are default-profile only
and excluded from the eval catalog. Read-only checks:

```sh
rg -n 'snap :=|Role: "child"|Snapshot: snap|AgentProfile' internal/cli/exec_engine.go
rg -n 'conversational assistant|CHAT_TOOL_ECONOMY' internal/daemon/assistant_turns.go
git diff b23789c -- internal/cli internal/engine internal/ctxcompiler internal/agents internal/models internal/providers internal/wireengine internal/tools internal/eval cmd/excalibur integrations packages/contracts apps/web extensions
```

Research basis: [SMART](https://arxiv.org/abs/2502.11435) reports 24% fewer tool
uses with improved accuracy; [OTC v1](https://arxiv.org/abs/2504.14870v1) reports
up to 73.1% fewer calls at comparable accuracy (the revised paper is titled
*Acting Less is Reasoning More* and reports 68.3%).
[LLMCompiler](https://arxiv.org/abs/2312.04511) motivates grouping independent
calls. These training and orchestration results motivate the prompt guidance;
they are not measurements of this change. The existing shared engine in
`internal/engine/batches.go` already executes compatible calls in parallel and
preserves resource serialization; it is unchanged.

Verified on 2026-11-03 with Go 1.27.3: formatting and whitespace checks, vet and
staticcheck for `internal/daemon` and `internal/robot`, race tests for
`internal/daemon/...`, `internal/robot/...` and `internal/master/...`, and
`go build -p 2 ./...` for Linux, Darwin and Windows all passed. Go commands ran
with `nice -n 19` and package parallelism 2; tests ran in a user systemd scope
with `MemoryMax=6G`. The daemon suite also passed its existing pinned embedded
tool-prefix and eval-profile hash checks. `git diff --stat b23789c` contains only
the two assigned source files, their test files and this document; every
forbidden path is unchanged. No live model or physical robot checks were run,
so prompt adherence and end-to-end latency gains remain unmeasured.
