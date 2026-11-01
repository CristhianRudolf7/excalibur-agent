# Terminal-Bench native runs (Section 23.2, Appendix F)

Operational instructions for `excalibur bench tb21|tb40`. Contract and
backend tests live in `internal/eval` (`trialrec`, `harboratif`,
`benchrun`) and `internal/cli` (exec recorder wiring); this file is
usage only.

## Prerequisites

- Docker Engine + Compose v2 (`docker compose version` must work).
  Ubuntu `docker.io` needs the manual Compose v2 plugin install; the
  user must be in the `docker` group.
- Harbor 0.25.0 (`pip install harbor==0.25.0`, commit `fdba28f`).
- Operator credentials JSON (OpenAI example):
  `{"base_url": "...", "api_key": "...", "fallback_api_key": "...",
  "model": "gpt-6-luna", "reasoning_effort": "low", ...}`.
  Keys travel to the container only via `--ae` environment variables
  and never land in logs, ATIF, metrics or Harbor config dumps.

## Commands

All commands run from the repository root.

- Build the static bench binary (pure-Go static, sha256 published):
  `scripts/bench/build.sh --arch amd64`
- Cheap pre-flight (no Harbor, no Docker, no paid API):
  `excalibur bench tb21 smoke [--dir DIR]`
- Full run (Appendix F frozen conditions: pinned dataset digest,
  `-k 5` default, no timeout/resource changes, no upload unless
  `--upload`):
  `excalibur bench tb21 run --credentials-file CREDS.json
  --tasks task-a,task-b
  --k 1 --concurrency 1 --jobs-dir jobs/tb21-test [--dry-run]`
  Provider/model/effort come from flags (`--provider --model --effort
  --base-url`) or fall back to the credentials file. `--tasks` maps to
  Harbor `-i` include names; `--concurrency` maps to Harbor `-n`.
- Wrapper entry point (calls the binary above):
  `scripts/bench/run.sh tb21 openai/gpt-6-luna 1 -- --credentials-file ... --tasks ...`
- Status and report (Section 23.7 statistics in JSON and Markdown):
  `excalibur bench tb21 status --jobs-dir jobs`
  `excalibur bench tb21 report <run-dir> [--out DIR] [--k N]`
- TB40 has the same shape (`bench tb40 ...`) with dataset
  `terminal-bench/terminal-bench@4.0.0`; not yet run live.

## Records

Per trial, under the Harbor trial directory:

- `/logs/agent/trajectory.json`: Harbor-compatible ATIF-v1.8
  (audited; incremental, survives kills).
- `/logs/agent/excalibur/`: custom `trajectory.json`, `metrics.json`,
  `profile.json`, `environment.json`, `model_calls.jsonl`,
  `tool_calls.jsonl` (+ `blobs/<sha256>` full outputs),
  `transitions.jsonl`, `gate_attempts.jsonl`, `loops.jsonl`,
  `masking.jsonl`, `compaction.jsonl`, `budget.jsonl`,
  `resources.jsonl`, `journal.jsonl`, `final.json`, plus
  `verifier_*` (reward/output) and `harbor_*` (trial logs) collected
  host-side next to them.
- Run dir: `manifest.json` (Excalibur commit, binary sha256, Harbor
  version/commit, dataset+digest, model/provider/effort, k, budgets,
  machine), `trials.jsonl` index, `report.json` + `report.md`.

### trials.jsonl index (Appendix F minimums, W8 fixed)

One row per trial (`internal/eval.BenchTrialIndex`, built by
`eval.IndexTrial`): reward and Appendix F failure category, token
usage with cache read/write splits, cost, wall/model/tool seconds,
harness and container RAM peaks, CPU seconds, provider and tool
retries, model/tool call counts, calls by tool, subagents,
compactions, mask batches, loop detections, gate attempts,
interruptions. Precedence is `metrics.json`, then the incremental
JSONL records (kill-safe), then Harbor `agent_result`.

Failure categories: Harbor exceptions map to
timeout/oom/budget/blocked (else `harness_error`); reward > 0
passes; otherwise the harness final state decides. Only a
completed harness run with reward 0 is `verifier`; a run the
harness never completed (provider/config failure, exit 137 OOM,
panic, missing records) is `harness_error` (or the matching
timeout/oom/budget/crash category).

Static binary TLS: the bench binary embeds Mozilla roots via
`x509roots/fallback` (binary-only import), used only when the task
image provides no system pool. TLS verification failures classify
as non-retryable configuration errors (`KindInput`) in every
adapter; transient provider errors retry through the gateway
(Section 19.5) with attempts reported on failure.

## Live verification (2026-10-31)

Static build + `eval smoke` pass. Two TB21 tasks with k=1 and
`gpt-6-luna` (effort `low`). Both trials produced complete
records and a valid report with zero Harbor exceptions; both failed on
the verifier (reward 0), which is a task outcome, not a harness error.
Job dir (ephemeral `/tmp`): `manifest.json`, `trials.jsonl`,
`report.json/md` per above. No trial data is committed; rerun the
command above to reproduce (costs ~$0.06-0.14 in model usage).
