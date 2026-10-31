# 0038 W8 bench fixes: TLS roots, retry classification, complete trial index

Date: 2026-10-31. Pilot: `/home/cristhian/bench-runs/tb21-pilot/2026-10-31__19-04-53`
(10 tasks, k=1, gpt-6-luna low). Six trials never reached the model
with `tls: failed to verify certificate: x509: certificate signed by
unknown authority`; the run-level `trials.jsonl` hid that outage as
six `verifier` failures with thin usage/timing fields.

## 1. Static bench binary carries its own root CAs

Task images without CA certificates (6 of the 10 pilot tasks) made
every provider HTTPS call fail at the TLS handshake. The fix is the
mechanism the task pointed at: `golang.org/x/crypto/x509roots/fallback`
(pinned `v0.0.0-20261030153154-30a33513ff6d`, BSD-3-Clause, recorded
in `dependencies.md`), blank-imported by `cmd/excalibur` only, per
the package docs ("only binaries, not libraries"). The verifier uses
the embedded Mozilla bundle only when the system pool is
empty/missing (pinned by `TestFallbackServesEmptySystemPool` and
Go's `initSystemRoots` semantics); images with their own CAs behave
exactly as before.

Proof is a container test, not a unit stub:
`TestBenchBinaryCarriesRootCAs` builds the static binary
(`CGO_ENABLED=0`), runs it in a `FROM scratch` image (no CA
certificates by construction) against the real provider endpoint
with a dummy key, and requires a post-handshake 401/auth rejection
with no `x509`/`certificate` text. It passed (8.4 s) and fails
without the import. Offline/container-less environments skip it
only when docker or the route is unavailable.

No provider code changed for this: the default transports resolve
roots per connection, so the fallback engages everywhere (providers,
embeddings, deciders) with zero behaviour change on CA-bearing
hosts.

## 2. TLS verification failure is non-retryable; retry policy verified in exec

The pilot classified the TLS failure as `transient` yet the run
failed on the first step. Both halves are now pinned:

- Classification: new `providers.IsCertificateError` /
  `providers.TransportKind`. `openaiwire.netError` (covers the Chat
  Completions family and the OpenAI Responses wire) and the
  Anthropic/Gemini transport-error branches map certificate failures
  to `KindInput` (no retry, no failover); anything else stays
  transient. Adapter unit tests hit real `httptest.NewTLSServer`
  endpoints with the default client.
- Retry policy in exec: the Section 19.5 gateway retry (bounded
  attempts, backoff, `Retry-After`) was already wired into exec via
  `BuildGateway`, and the pilot's ~1.9 s first-step latency matches
  three exhausted attempts. What was missing was observability: the
  gateway returned an empty `CallMeta` on failure, so recorders and
  metrics reported zero retries. Failure paths now carry
  `Attempts`/`Retried`/endpoint, and `TestGatewayFailureReportsAttempts`
  pins it.
- Exec-level proof (`internal/cli/exec_retry_test.go`): a transient
  that clears completes after exactly 3 provider calls with
  `model_retries == 2` and real `model_seconds`; a TLS-flavoured
  input error fails after exactly 1 call with zero retries.

Section 17.7 reading: the engine still fails the run once the
gateway exhausts its budget (correct for eval, where task timeouts
are tight); long-schedule autonomous retries are not applied to
provider calls in `exec`. Config-class errors fail fast by design.

## 3. trials.jsonl carries every Appendix F field

`indexTrial` (now `eval.IndexTrial`, unit-tested) was rewritten:

- Categories: Harbor exceptions map as before
  (timeout/oom/budget/blocked, else `harness_error`); reward > 0
  passes; otherwise the harness final state decides. `completed`
  with reward 0 is the only `verifier` case; `failed`/`paused`/
  `blocked`/missing records map to `harness_error` (exit 137 to
  `oom`, timeout/budget text to their categories, panics to
  `crash`). Re-indexing the pilot yields `harness_error` x 6
  instead of `verifier` x 6.
- Fields: tokens (with cache splits), cost, wall/model/tool
  seconds, harness and container RAM peaks, CPU, retries, call
  counts, calls by tool, subagents, compactions, mask batches,
  loop detections, gate attempts, interruptions. Precedence is
  metrics.json, then the incremental JSONL records (kill-safe),
  then Harbor agent_result. The pilot re-index fills model/tool
  seconds, cache splits and per-tool calls that the old metrics
  path left at zero.
- Metrics source fixes so new runs are complete at the source:
  `recordingModelEndpoint` observes every model call with real
  latency and per-retry efficiency counts (success and failure);
  `recordingToolRunner` observes tool latency and per-tool calls;
  `runEnginePath` syncs the engine's failed gate evaluations and
  loop detections into metrics (the engine `GateAttempts` field
  counts failed evaluations per its documented semantics; the
  index approximates total evaluations from the JSONL rows as
  before, exact for the common never-ran/first-try-pass cases);
  `RunExec` skips its post-hoc aggregate for recorded runs (no
  double counting). `BenchTrialIndex`/`TrialOutcome` gained the
  missing counters (`harness_peak_bytes`, `cpu_seconds`,
  retries, subagents, compactions, masks, loops, gates,
  interruptions).

Known limits: RAM/CPU peaks stay 0 when the platform sampler
reports 0 (unchanged `internal/platform` behaviour, another
agent's package); `max_depth` is not derivable from the current
records and is not indexed.

## Deferred

- Re-running the 6 affected pilot tasks with the fixed binary (task
  item 4) is recorded in the status file, not here.
