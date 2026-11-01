# Skills as cards: pull-only retrieval with error-triggered excerpts (Sections 12.3-12.4)

Paired measurements on the current version show the expanded skills
library (256 skills, task-start auto-suggest) lowers success by
about 7-10 points (all-on ~40.5% vs skills-off ~47.3% on the same
task set). Triage of the failures found knowledge gaps in only 1 of
44 failures; the dominant causes are discipline and verification.
This decision records why the library hurts small models and the
cards redesign that replaces push with pull.

Everything stays inside `internal/skills` (content and selection)
plus the existing minimal additive seam in
`internal/cli/exec_engine.go`; connectors, gateway, API, bench
invocation and the eval profile table are untouched.

## 1. Why skills hurt (read-only trial evidence)

Four mechanisms, all measured from the paired runs
(`all-on/2026-10-31__23-48-35`, 74 trials, 30 pass; paired
`skills-off-gcp/2026-11-01__00-48-41`, 74 trials, 35 pass):

1. Prompt bloat on every call. The programming L1 catalog (~13K
   chars) plus 3 injected suggestions ride the stable prefix, so
   they are re-sent on every model call. First-call input tokens
   average 4552 with the library on vs 1698 with it off (~2.7x);
   total input per trial averages ~123K vs ~80K tokens. The extra
   ~43K tokens buy nothing on most trials.
2. Almost nobody pulls. Only 11 of 74 all-on trials called
   `search_skills`/`load_skill` at all (3 searches, 8 loads); 63
   trials paid the catalog cost and never touched a skill.
3. Suggestions mislead. The BM25 ranker votes on generic words, so
   lexical neighbours win over task neighbours: a chess task got
   `docker-containers` + `backend-best-practices` ("best" matched);
   an R sampling task got `go-modules-toolchain`; a Bayesian
   modelling task got `javascript-node-notes`. A cheap model follows
   a confident-sounding injected suggestion more than its wording
   deserves.
4. On-demand use wastes steps when it happens. `search_skills {}`
   with no query dumped all 256 skills inline (tens of KB) in 3
   trials; 2 of the 8 loads were an empty-name error and an
   irrelevant `huggingface-search` load (browser skill with deferred
   tools the eval surface cannot run).

## 2. Cards: inject nothing, retrieve on demand or on error

New switch value `EXCALIBUR_SKILLS=cards` (keeping `off`, `on`,
`auto` for ablation), now the default:

- No catalog and no suggestions are injected in any prompt layer.
  The only skills text in the prompt is a one-line on-demand
  pointer (`CardsPointer`, <400 chars) riding the stable L1 layer.
- `search_skills`/`load_skill` stay wired: retrieval is pull-only,
  on the model initiative.
- Error-triggered retrieval: when a tool result fails, the harness
  matches the output against a deterministic, precision-first table
  (`MatchError`: concrete error strings and missing-binary names)
  and appends at most one compact card excerpt (capped at 40 lines
  / 4000 chars: title, when-to-use line, Procedure, Pitfalls,
  Verification, plus a `load_skill` pointer for the full text).
  Clean outputs, running sessions and runner-level errors never
  fire; unknown binaries never fire.
- `EXCALIBUR_SKILLS_ERROR_CARDS=0` disables the excerpts (default
  on; no effect outside cards mode).
- An unscoped empty `search_skills` query no longer dumps the
  catalog: it returns short guidance with the group list (all
  modes; StatusOK is preserved, so existing surface tests hold).

## 3. Content audit: card-grade vs advisory

All 42 skills reachable from the trigger table were audited for the
card bar (exact commands, pitfalls, verification steps): all pass,
with one template fix (`systematic-debugging` gains the missing
`## Verification` section). The vague tail (browser/search,
marketing, finance, compliance, creative-tools, methodology prose)
is advisory content, not command references; rewriting it into
fake command lists would destroy its purpose. The prune is
by-default instead: in cards mode advisory skills can no longer
reach the prompt uninvited, and the error table never fires them.
A backend test pins every trigger skill's name, state and template
sections, so a future edit that de-templates a trigger skill fails
the suite instead of silently degrading cards.

## 4. Default change: auto becomes cards

`DefaultLibraryMode` (and the unset/invalid fallback) moves from
`auto` to `cards`: the measured default hurt by ~7 points, so the
safe default injects nothing. `auto` stays available for ablation
(`EXCALIBUR_SKILLS=auto` restores the catalog + auto-suggest
bytes exactly). `skills.json` gains `error_cards` (and keeps mode,
compact flag, catalog hash, skill count, suggestions) plus the
`skills_wired` event, so arms stay comparable.

## 5. Switches and ablation

| Switch | Values | Default | Off behaviour |
|---|---|---|---|
| `EXCALIBUR_SKILLS` | `off`/`on`/`auto`/`cards` | `cards` | see mode |
| `EXCALIBUR_SKILLS_ERROR_CARDS` | `0`/`false`/`no`/`off` disables | on | no excerpts appended |
| `EXCALIBUR_SKILLS_COMPACT` | `1`/`true` or unset | unset (full) | unchanged |

Measure `cards` vs `off` vs `on` vs `auto` on the same task set
(Appendix F): success rate, first-call input tokens, share of
trials with any skill call, error-card fire rate (marker count in
`tool_calls.jsonl` blobs), and per-cause triage drift. Keep the
mechanism only if cards matches or beats off at a fraction of the
tokens; keep error-cards only if fired cards correlate with
recovery (a successful tool call within a few steps on the same
subsystem).

## 6. Alternatives rejected

- Smaller catalog / fewer suggestions in auto: keeps the push
  shape that the evidence blames (irrelevant suggestions still
  mislead; the catalog still costs ~3K tokens per call).
- Model-judged retrieval (LLM picks the skill): a cheap model is
  already the bottleneck; deterministic error matching costs no
  model call and is testable line-by-line.
- Deleting vague skills from the catalog: breaks the Section 12.5
  census pinned by contract tests and guts `on`/`auto` arms;
  prune-by-default achieves the prompt effect without touching
  the census.
- Appending cards on `err != nil` runner errors: the engine
  discards `res` when `err != nil`, so the card would never reach
  the model; runner errors already carry their own guidance.

Research surveyed before coding (general harness behavior only; no
benchmark task contents used): LangChain "Improving Deep Agents
with harness engineering" (prompt bloat dilutes the working
method; verification checklists beat more context), Meta-Harness
(arXiv 2603.28052: what the harness presents matters as much as
weights, larger gains on weaker models — motivates removing the
injected catalog for the small reference model), Terminus 2
(single-tool terminal loop as the neutral baseline our exec
records already follow), mini-SWE-agent (minimal bash-only surface;
few well-designed tools beat many advertised ones — motivates the
one-line pointer over the 256-line catalog). Full texts were
fetched during W1; see `docs/development/agent-quality.md`.
Evaluation records are standard evaluation and auditing (Section
23.2, Appendix F).
