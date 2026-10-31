# H3 (engine half) — durable journal and agent engine

Contract and backend tests of the milestone implementing the engine
half of Section 7 (agent cycle, engine side) and Section 17
(journal, effect classes, per-run recovery), wired to the real
packages: providers, the single-model gateway (`internal/models`),
context compiler, tools, and the tasks gate and loop detector.

## Contract

- **Journal** (`internal/journal`): append-only entries
  (`run_started`, `model_request`, `model_response`, `tool_intent`,
  `tool_result`, `directive_received/applied`, `compaction`,
  `state_change`, `checkpoint`) with sequence assignment inside the
  porter write (journal lane). `MemStore` for tests and the embedded
  profile; `SQLStore` over the Appendix A `journal` table, surviving
  reopen. Write-ahead order (intent before effect, result after,
  response before tools). `ModelResponsePayload` stores the full
  requested calls so a kill between the response and the intents
  still replays. `PendingIntents` matches intents to results by call
  id (falling back to the idempotency key); `MissingIntents`
  synthesizes intents for response calls that never landed.
  `ResolvePending` implements the 17.2 classes: pure reruns,
  idempotent checks the postcondition (rerun when safe without a
  probe), keyed resends with the same key, non-idempotent without a
  probe is uncertain and never repeats. `ModelReplay` tells whether
  the trailing request still needs its call.
- **Gateway** (`internal/models`, single-model subset of 19.2-19.6):
  per-role primary plus ordered fallback chain, bounded retries
  honoring `Retry-After`, breaker after 5 consecutive failures (or
  >50% in a minute) with 60s cooldown and failover, content-error
  retry with a doubled output limit, versioned price table with
  cached subsets priced separately, concurrency cap with an
  interactive-lane reserve, and a compact history-free expert
  consult (19.3 plumbing).
- **Engine** (`internal/engine`): one loop for every level,
  distinguished by `Profile` (tools, model role, budgets, context
  role). States enforce the 7.2 diagram; every change is journaled.
  Waiting consumes no model calls; every wake event (tool, user,
  subagents, agenda, capability, lease, any directive) resumes, while
  pause/cancel apply directly. Each step checks budgets (exhausted
  parks with `paused(budget)`), enters closing mode below 10%
  (definitions kept, `tool_choice: none`, stable prefix hash),
  records request/response, runs tool batches, observes the loop
  detector (note, expert escalation, `blocked(loop)`), and evaluates
  the gate through `verifying`: pass completes, otherwise the compact
  note is injected, and after two attempts the honest state
  (`completed_with_pending` or `blocked`) is recorded. Batches
  (7.4): reads share a batch, writes serialize per resource key,
  exclusive tools run alone, results return in request order;
  execution fans out through `safego.Go` and rejoins. Recovery
  (`Recover`, `ApplyRecovery`, `ReplayPending`) rebuilds the
  pre-crash state, classifies pending effects, decides
  resume/paused/blocked/waiting conservatively (>6h outage or
  uncertain effect parks), and replays reruns with their recorded
  keys without recalling the model. Delivery reports (7.6) carry
  state, reason, summary, files, gate pendings and metrics.

## Backend tests

All rows of the 7.7 table owned by the engine plus the first 17.8
suite, hermetic (scripted providers, in-memory and file SQLite):

- `lifecycle_test.go`: legal/illegal transitions from every
  non-terminal state with journaled `state_change`; waiting consumes
  no model calls and every wake event resumes; directives wake
  waiting runs; pause/cancel need no wake; every 7.5 defect (missing
  or stale evidence, failed tests, paused child, newer instruction
  revision, judgment without verifier, user confirmation) never
  completes and ends honest after two attempts; closing mode keeps
  the prefix hash with `tool_choice: none`; loop escalation fires
  note, expert, blocked; pause/block emit full delivery reports.
- `batches_test.go`: reads parallel, same-file writes serialized,
  independent writes parallel, exclusives alone; results in request
  order; overlap proof with rendezvous tools.
- `crash_test.go` (17.8 first suite): kills before/after intent,
  after effect, during compaction/checkpoint and inside the model
  leg; read repeat, idempotent postcondition, keyed single effect,
  non-idempotent blocked without repeat, trailing request repeated;
  file-DB kill and reopen with exactly-once effects and one model
  call; long-outage parking; waiting-user restoration.
- `e2e_test.go`: a simple file task (real gateway, compiler, file
  tools, gate) completing with journal, metrics and report; an
  interactive program resolved turn by turn through a scripted
  provider with ordered history.
- `internal/journal`: ordering, write-ahead helpers, model-leg
  replay, all four pending classes, checkpoint cadence, SQL round
  trip and reopen.
- `internal/models`: retry, fallback, no blind retry on auth,
  breaker open/skip, `Retry-After` honored, truncation doubling,
  price-table cost, lane reserve, unknown role, compact expert
  prompt.
