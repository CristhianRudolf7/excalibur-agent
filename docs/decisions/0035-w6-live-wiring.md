# 0035 — W6 production model wiring

Date: 2026-10-31. Scope: make the whole binary work with real model
providers (audit finding: production never constructed one).

## What changed

- `internal/providers/registry`: registers the OpenAI-family kinds
  (`openai`, `deepseek`, `openrouter`, `ollama`, `generic`) next to
  Anthropic/Gemini; `EXCALIBUR_API_KEY` alias after the
  kind-specific variables; fallback-credential resolution
  (vault, env, config) with a decorator that retries
  quota/rate-limit failures exactly once on the fallback key and
  never touches it for auth/input/content errors; streaming falls
  back only before the first emitted fragment. Operator
  credentials-file loader (`base_url`, `api_key`,
  `fallback_api_key`, `model`, `reasoning_effort`,
  `embedding_model`, `decisions_model`).
- `internal/config`: additive `[models]` section (provider, model,
  base URL, reasoning/effort/embedding/decisions models,
  credentials path, key and fallback refs). The env loader skips
  single-segment `EXCALIBUR_*` names: they are explicit aliases
  resolved in `internal/models`, and mapping them into the merged
  table collided with the same-named section tables (`provider`).
- `internal/models` (`operator.go`): `ModelSettings` resolution
  (env aliases, layered config, credentials file, built-in
  defaults), fail-fast `NoProviderError`, single-model role table
  for every core role, gateway config with breaker tuning, and the
  decisions config pinning the no-`reasoning_effort`-on-embeddings
  rule.
- `internal/cli` (`modelwiring.go`, `exec.go`, `exec_engine.go`):
  production resolution with vault-backed `secret://` refs (nil in
  eval), fail-fast key checks, full role table, configured (never
  hard-coded) reasoning effort, engine usage folded into
  `metrics.json`. `exec` fails with exit 1 when unconfigured and
  keeps the deterministic step only under explicit
  `EXCALIBUR_PROVIDER=stub`.
- `cmd/excalibur/daemon.go`: intake decider (`LLMDecider` over the
  gateway) and memory embedder (`memory.HTTPEmbedder`) built from
  the same wiring; unconfigured operators keep the deterministic
  seams with a stderr notice. Master turns and daemon run engines
  keep their H9-owned model seams.
- `internal/engine/engine.go`: records the assistant tool-call
  message with its calls so follow-up turns carry complete
  function-call pairs (required by strict wires; fixes Chat
  Completions fidelity too). `internal/cli/exec_engine.go`
  snapshot now carries the L3 request and the project directory.
- Live checks: `internal/cli/live_test.go` (`-tags live`,
  `EXCALIBUR_LIVE=1`) covering `exec` file and terminal tasks, one
  embedding call and one decider call; results in
  `docs/development/live-smoke.md`.

## Alternatives considered

- Adapter-level fallback keys (one per OpenAI-compatible client)
  instead of a registry decorator: rejected, the decorator covers
  every kind uniformly including Anthropic/Gemini.
- Forcing the vault open in `exec` for `secret://` keys: rejected,
  unlocking is interactive (H10); refs resolve when a vault handle
  is wired, otherwise env/config/credentials file apply, and the
  gap is explicit, not silent.
- Wiring a full `memory.Service` with embeddings into `exec`:
  deferred; the builder and daemon integration land here, exec
  memory tools stay on their current seams (H11 owns the service
  wiring).

## Discrepancies and cross-area notes

- Section 19.2 lists the OpenAI adapter table; the Responses wire
  is now the registry default for `openai` because Chat Completions
  rejects effort+tools on reasoning models (observed 400). Direct
  adapter construction is unchanged.
- Section 19.1 wants a different verifier family when possible;
  with one configured model every role shares it (recorded in the
  role-table docs); operator fallback chains stay future work.
- Price table is zero (`operator-unpriced` version) until the
  operator price table lands; per-call cost records 0 from the
  gateway, while telemetry falls back to its documented default
  rates in `metrics.json`.
- Durable daemon stores (`master.NewMemStore`,
  orchestrator/journal/agenda memory stores) and the master turn
  loop plus daemon run-engine model endpoints stay H9-owned; the
  `eval smoke` scaffold stays intentionally model-free. None of
  these is a model-provider stub, so W6 leaves them untouched.
- Live spend for this verification: a few model calls plus one
  embedding call (see `live-smoke.md` for tokens/cost).
