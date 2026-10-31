# W6 live smoke: production model wiring

Live verification of the W6 production wiring (architecture Sections
19.1, 19.2, 19.4 and 10.8): real providers built from the operator
layers through `internal/providers/registry` and served through the
`internal/models` gateway in every production entry point. These
checks run only with `-tags live` and `EXCALIBUR_LIVE=1`, never under
plain `go test ./...`; they are tiny by design. Operator credentials
come from `EXCALIBUR_CREDENTIALS_FILE` (default
`~/.config/excalibur/credentials/openai.json`) and travel in
`Authorization` headers only; logs and this document record shapes,
counts and costs, never keys.

## What ran

Command shape (temporary git repository per task):

```sh
EXCALIBUR_CREDENTIALS_FILE=~/.config/excalibur/credentials/openai.json \
  go test ./internal/cli/ -tags live -run 'TestLive' -v
```

Operator file resolved to provider `openai`, model `gpt-6-luna`,
reasoning effort `low`, embedding model `text-embedding-3-small`,
decisions model `gpt-6-luna`. Live tests:
`internal/cli/live_test.go` (`TestLiveExecHello`,
`TestLiveExecTerminal`, `TestLiveEmbedding`, `TestLiveDecider`).

## Results (2026-10-31)

| Check | Result | Evidence |
|---|---|---|
| `exec` hello task in a temp git repo: create `hello.py` that prints hello and verify it runs | `completed`, exit `0` | `hello.py` created in the project dir, `python3 hello.py` prints `hello`; ATIF `trajectory.json` valid per `telemetry.ValidateFile` (ATIF v1.8); metrics: model `gpt-6-luna` / provider `openai`, input 4108, output 133, cost 0.013920 USD (operator-unpriced table, telemetry default rates) |
| `exec` interactive-terminal task: open a terminal session, run `echo terminal-ok-42`, report the output | `completed`, exit `0` | Tool result carries `terminal-ok-42` from the session; ATIF valid |
| One embedding call with `text-embedding-3-small` | ok | 2 vectors, 1536 dims; the request shape carries no `reasoning_effort` (pinned by `TestEmbeddingOmitsEffort` and `TestLiveDecisionsFallback`) |
| One decider call with `gpt-6-luna` (noul "2+2 equals 4") | ok | Probability 1.000, inside [0,1] |

## Re-validation through the binary (iteration 7)

Both tasks re-ran through the production binary as the audit
requires (`excalibur exec --json` in fresh temporary git repos,
same operator file):

| Check | Result | Evidence |
|---|---|---|
| `exec --json` hello task | `completed`, exit `0` | `hello.py` prints `hello`; metrics: in 4158/out 154, cost 0.014322 USD |
| `exec --json` terminal task | `completed`, exit `0` | `terminal-ok-42` in the tool result; ATIF v1.8, 4 steps; metrics: in 2693/out 69, cost 0.008907 USD |

## Findings fixed by this wiring

- Chat Completions rejects `reasoning_effort` with function tools
  on reasoning models (`http 400 ... use /v1/responses or set
  reasoning_effort to 'none'`), so the registry builds the OpenAI
  adapter on the Responses wire by default.
- The engine dropped the assistant tool-call message from history,
  so second turns sent orphaned `function_call_output` items and the
  Responses wire rejected them; the engine now records the assistant
  tool-call message with its calls (`internal/engine/engine.go`,
  covered by `TestEndToEndInteractiveProgram`).
- The embedded engine path never sent the request text or the
  project directory to the model; the snapshot now carries the L3
  request with executable-verification instructions and the project
  directory as project context, and engine usage folds into
  `metrics.json`.
- With no provider configured, `exec` used to run a silent stub;
  it now fails with a stable harness error (exit 1) naming the
  variables and files to set. The stub remains only under the
  explicit `EXCALIBUR_PROVIDER=stub`.
