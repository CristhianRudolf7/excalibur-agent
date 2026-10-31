# H12 — Model roles, routing, escalation and decider

Contract and backend tests of the H12 models area (architecture
Sections 19.1, 19.3, 19.4 with wiring into the
deterministic/decider/LLM boundary of Table 2.1 where Sections 6
and 10 use it; Appendix E row H12). The H1/H3 provider adapters,
single-model gateway, retries, breaker and price table live in
`docs/development/providers.md` and `internal/models/gateway.go`;
this document covers what H12 adds on top.

## Contract

- **Packages**: all in `internal/models` on top of the
  `internal/providers` contract:
  - `roles.go`: Section 19.1 table. `RoleProfiles` (role, use,
    wanted profile), `AllRoles` (12 roles), `CoreRoles` (the 7
    chat roles every deployment configures), `ValidateRoles`
    (primary plus fallbacks non-empty, unknown roles rejected)
    and `RequireRoles` (fail-fast missing list).
    `VerifierSharesFamily` is the cheap prefix heuristic for the
    "different family than executor when possible" guidance.
    Code never names concrete models; configuration assigns each
    role a primary plus an ordered fallback chain.
  - `router.go`: Section 19.3 escalation. `EscalationSignal`
    (loop-second, test-repeat-fail, high-complexity,
    user-quality, gate-repeat-fail), `EscalationPolicy`
    (defaults: 2 fix attempts, 2 gate failures, 2nd detection,
    8 requirements / 20 steps / 3 milestones), `PlanSize`,
    `ShouldEscalate*` predicates, `EscalationState`
    (Escalate/Resolve, no stacked calls, `ExpertCalls` count)
    and `ExpertInput`/`RequestExpert` (one compact brief:
    objective, state, exact error, tried, fragments — never
    history, so the run prefix cache survives). After an
    escalated step resolves, `Resolve` drops back to the
    default model; the router never alternates models inside a
    run except for these isolated expert queries.
  - `decider.go`, `backends.go`, `fake.go`: Section 19.4
    bounded decider. Kinds `choice`/`score`/`noul` with
    `Question.Validate` (shape plus the never-final-verdict
    rule), `Answer.Normalize` (probability clamped to [0,1],
    value checked against options), `Thresholds` with
    `DefaultThresholds` (low 0.35, high 0.75, initial values
    per Appendix B) and `Decide` (p >= high acts, middle
    defers to the LLM inside the normal flow, p <= low takes
    the alternative). Port constants `AskThreshold` (0.7,
    Section 6.4) and `StuckJudgeThreshold` (0.9, Section
    18.3). Backends behind one `Decider` interface: `HTTPDecider`
    (`NewJevDecider`, `NewPplxDecider`: Jev API, pplx API or
    the local 4B sidecar URL over a shared
    state-plus-typed-questions shape; the wire never carries
    `reasoning_effort`), `LLMDecider` (cheap-model fallback
    with restricted JSON, probabilities from the JSON number
    clamped; no `reasoning_effort` of its own — the gateway
    role entry fixes it per role and ReasonOmit adapters drop
    it), `FakeDecider` (scripted probabilities for wiring
    tests). Every backend rejects `FinalVerdict` questions
    before any network use.
  - `calibration.go`: own calibration. `Sample`
    (probability, label), `ECE` (equal-width bins),
    `SelectiveAccuracy` (accuracy over decided samples plus
    coverage), `Calibrate` (grid search over low in
    [0.1,0.5], high in [0.6,0.95]; best selective accuracy,
    ties to coverage then band width; needs >= 10 samples).
    Vendor figures (pplx ~0.05 ECE) are not trusted.
  - `decisions.go`: decisions-model configuration.
    `DefaultDecisionsModel` (`gpt-6-luna`, configurable per
    deployment), `DefaultEmbeddingModel`
    (`text-embedding-3-small`), `DecisionsConfig` with
    `ChatRequest` (carries the configured effort) and
    `EmbeddingRequest` (model plus input, never a
    `reasoning_effort` field) plus `OmitReasoningEffort`
    (embeddings true, chat false).
  - `ports.go`: Table 2.1 wiring. `TriageQuestions` (type
    choice, scope choice, durable-instructions noul,
    needs-clarification noul) with `FastLaneClass`;
    `ShouldAsk` (mandatory/irreversible always asks, else
    the 0.7 rule with costly rework); `ClassifyAdmission`
    (deterministic bands: >= 0.95 same-entity duplicate,
    < 0.80 new, middle band needs the decider) with
    `AdmissionQuestion`; `StuckQuestion`/`CountsStuck`
    (from step 30, every 10, only >= 0.9, note-only);
    `NotifyQuestion` (5.4 immediate notice),
    `SkillScoreQuestion` (12.3/12.4),
    `ToolRerankQuestion` (13.3).
  - Cost rule preserved: triage never adds an LLM call. With
    no decider, or below confidence, the preanalysis result
    goes to the master inside its own turn (Section 6.1).
- **Reasoning effort**: fixed per role (Section 19.2). The LLM
  fallback sets none itself; the role entry may pin `low`
  for cache stability and ReasonOmit contracts (DeepSeek,
  Ollama) drop it on the wire. Embedding operations never
  carry it.
- **Live**: `live_test.go` (`-tags live`, `EXCALIBUR_LIVE=1`)
  asks one short bounded question of the real decisions
  model (bounded retry with the fallback key on
  quota/rate-limit only) and asserts the embedding
  no-effort rule. Never runs under plain `go test`.

## Backend tests

All against `httptest` servers or scripted callers; no test
calls a paid API.

- `roles_test.go`: 12 profiles with use and wanted profile;
  validation rejects empty refs and unknown roles;
  missing-list helper; verifier family heuristic.
- `router_test.go` (row 3 of 19.7): every escalation signal
  fires, consults the expert with one compact history-free
  prompt served by the expert role, counts one call, refuses
  stacked escalation, and drops back to default on Resolve;
  first loop detection only notes; small plans stay default;
  empty briefs fail before spending a call.
- `decider_test.go` (row 4 of 19.7): question shape
  validation; all three backends plus the fake reject
  final-verdict questions before network use; abstention
  bands (act/defer/alternative with boundary equality);
  LLM fallback parses fenced JSON, clamps, keeps the
  configured decisions model (`gpt-6-luna` default),
  rejects garbage, and sends no `reasoning_effort` itself
  through the decider role.
- `calibration_test.go` (row 4 of 19.7): ECE near zero on a
  separable labeled set and large on an overconfident set;
  `Calibrate` reaches >= 0.95 selective accuracy with the
  middle band deferring; fewer than 10 samples fail.
- `ports_test.go`: triage builds 2 choice + 2 noul
  questions; fast lane only on resolved high-confidence
  control queries; `ShouldAsk` matrix (mandatory,
  irreversible, 0.7 costly-rework, trivial); admission
  deterministic bands plus decider in the 0.80-0.95 band;
  stuck judge gating (30/10/0.9); notify/skill/tool ports
  validate and answer through the fake.
- `backends_test.go`: Jev and pplx shapes answer bounded
  questions with probabilities over `httptest` (no
  `reasoning_effort` on the wire); missing URL, 5xx and
  cancelled contexts fail; decisions model configurable;
  embedding requests omit the effort while chat keeps it;
  a decider role with pinned `low` through a ReasonOmit
  wire omits the field.
- Live (`-tags live`, `EXCALIBUR_LIVE=1`):
  `TestLiveDecisionsFallback`: one short bounded question
  of the configured decisions model with bounded fallback-key
  retry on quota/rate-limit, plus the embedding no-effort
  rule. Tiny by construction.

## Deferred (not in this wave)

Remote image/video/voice/GPU providers (19.2 tail); vault-backed
credential resolution (H10 hook: backends take an explicit key,
the registry resolves vault first); provider `logprobs` as a
second probability source for the LLM fallback (H1 adapters do
not expose them yet, so the JSON number is the source).
