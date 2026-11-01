# 0057 Muse family adaptation (agent quality)

Date: 2026-11-01. Branch: `agent-quality` (Muse family workstream,
director's design). Status: implemented.

## What

The model-family adaptation layer (`EXCALIBUR_Q_FAMILY`, on by
default) gains a Muse variant selected for model ids starting with
`muse-` or `meta-models/` (plus `muse-spark`/`muse-glimmer`
substrings, case-insensitive):

- `ctxcompiler.PromptMuse` with `DetectFamily` matching the Muse id
  shapes first, so a Muse id that happened to contain another
  family's substring still selects Muse. Unknown ids still fall back
  to the GPT default.
- `FamilyNote` Muse variant: exact-JSON arguments with no invented
  fields or target names, every parallel call answered with its own
  result in call order, never invent results, finish only via the
  `finish` tool with evidence from executed checks.
- `AdaptToolDescription` Muse variant of the model-visible tool
  descriptions: the `exec` and `finish` descriptions carry the Meta
  Model API discipline above; every other tool is unchanged.
- Wiring: `renderCore` adapts the L0 core-tool lines and the engine
  `DefaultBuilder` adapts the provider tool definitions, both only
  when family adaptation is on and the resolved model id is Muse.
  GPT (and every other family, and adapt-off output) is
  byte-identical.

## Why

Agentic Harness Engineering (arXiv 2604.25850): the optimal harness
is model-specific — the same harness code yields +5.1 to +10.1pp
cross-family gains without re-evolution, and the gains localize to
tools, middleware and long-term memory rather than the system
prompt. A prompt/tool-format variant tuned for Meta's Muse models
follows that result without touching the connectors.

## Research sources (fetched before coding)

- Meta Model API, Muse Spark Open prompting guide
  (`dev.meta.ai/docs/muse-spark-open/prompting`, fetched
  2026-11-01): conversation roles (`system`/`user`/`assistant`/
  `tool`); the template adds default lines, the system prompt, tool
  definitions, the numeric reasoning strength, and the recipient
  list; recipients are `self` (reasoning), `commentary` (progress
  updates), a tool name (tool call), `user` (final answer). System
  prompt guidance: most important rules first; Objective, Priority
  rules, Scope check, Decision procedure, Tools (native calls, never
  invent results), Output shape, Completion (stop only with
  support). Tool calling is OpenAI function-calling; parallel calls
  in one response must each be answered with their own `tool`
  message in call order (`tool_call_id` is not rendered, results
  carry only the function name). Each non-final call ends with
  `<|eom|>`, the turn ends with `<|eot|>`; never stop on `<|eom|>`.
  Reasoning cannot be turned off (`reasoning_effort` low/medium/
  high/xhigh only; anything else is rejected).
- Meta Model API, recommended settings
  (`dev.meta.ai/docs/muse-spark-open/settings`, fetched
  2026-11-01): sampling `temperature 1.0`, `top_p 1.0`
  (`do_sample true`); reasoning strength low=8 (classification,
  short tool loops), medium=32 (general agentic), high=256
  (multi-step reasoning, long tool loops), xhigh=512 default
  (depth over latency) — the harness `low` effort therefore maps to
  short tool loops, matching exec/eval use; `--reasoning-parser
  muse --tool-call-parser muse --enable-auto-tool-choice`;
  `eos_token_id` is `[200001, 200008]`; stream long generations.
- Meta Model API, tool calling (`dev.meta.ai/docs/tool-calling`,
  fetched 2026-11-01): Responses API recommended (preserves
  reasoning across tool turns for stronger multi-step
  performance); Chat Completions fully supported. `tool_choice`
  must be `"auto"` — `"none"`, `"required"` and named choices
  return HTTP 400. `max_tool_calls` caps built-in tools only.
  Always include the full assistant message with `tool_calls`;
  every `tool_call_id`/`call_id` must match (1-64 chars); strict
  schemas validate on `strict: true` only; arguments are never
  guaranteed valid — always parse and validate locally (the
  harness tolerant-args layer already does).
- Hugging Face model card `meta-models/Muse-Spark-Open-816B-A42B`
  (fetched 2026-11-01): 816B total / 42B active MoE, 1M context,
  trained for agentic work (multi-step planning, sequential tool
  invocation, failure recovery, long-horizon execution); use high
  or xhigh reasoning for coding/agentic tasks, low/medium for
  faster shorter responses; evals at temperature 1.0 / top_p 1.0 /
  xhigh; Terminal-Bench 2.1 76.4 with the Terminus 2 agent.
  Limitations: may still err in multi-step reasoning on novel
  scenarios; avoid exposing raw chain-of-thought; guard
  irreversible actions with human-in-the-loop confirmation.
- Meta build-with-Muse-Spark-Open post (`dev.meta.ai`, fetched
  2026-11-01): serving runtimes (SGLang, vLLM, TensorRT-LLM, …),
  hosted inference providers, early-user evidence on tool calling
  ("selected the right next tool", "multi-step and parallel calls
  with clean structured outputs").
- LangChain, "Improving Deep Agents with harness engineering" (Feb
  2026, fetched full text): 52.8 to 66.5 on TB2 with the model
  fixed; model-specific prompt tailoring; self-verification,
  environment onboarding, loop-detection nudges. Prior art for the
  shared working method this variant extends.
- Meta-Harness, arXiv 2603.28052 (abstract fetched 2026-11-01):
  harness code moves results as much as weights, with larger gains
  on weaker models; motivates the small-context + pinned-facts
  shape the Muse note preserves.
- Agentic Harness Engineering, arXiv 2604.25850 (abstract fetched
  2026-11-01): ten AHE iterations lift TB2 pass@1 69.7% to 77.0%;
  +5.1 to +10.1pp cross-family transfer; gains localize to tools,
  middleware and memory rather than the system prompt.

## Alternatives considered

- One shared note for all families: rejected — the Meta API's
  call-order rule, exact-JSON discipline and auto-only
  `tool_choice` are Muse-specific and would be noise for others.
- A separate `EXCALIBUR_Q_MUSE` switch: rejected — the existing
  `EXCALIBUR_Q_FAMILY` switch already ablates the whole family
  mechanism (off restores family-independent L0 and untouched
  tool definitions for every family); a per-family switch adds
  surface without a measurement need.
- Teaching ATEM/recipient syntax in the prompt: rejected — the
  server applies the chat template and the parsers; the harness
  stays on the OpenAI-compatible wire (connectors frozen) and the
  prompt teaches only behavioural discipline.

## Departures from the architecture document

None. Sections 7.3 (step loop), 11.1 (stable L0 prefix; the variant
only changes bytes when the model family changes, never inside a
run), 19.1 (model roles; a Muse family also serves the
"different family than executor" verifier guidance) and 19.2
(native tool calls over the OpenAI-compatible API, reasoning
effort fixed per role) already cover family-specific prompting.
Known API constraint recorded as a proposal, not a change: closing
mode (§7.3) sends `tool_choice: none` to keep the prefix hash, but
the Meta Model API accepts only `auto` (HTTP 400 otherwise). The
wire is untouched (connectors frozen); the Muse variant instead
teaches finish-only-via-tool so runs end through `finish` before
closing engages. See the status file Proposals section.
