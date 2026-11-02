# Right panel backend

The backend contract is recorded in decision 0096 and the JSON definitions in
`packages/contracts/{methods,events}.json`. The implementation adds no model calls,
paid API checks, npm dependencies or eval profile changes.

Backend verification is in `internal/daemon/panel_test.go`:

- `files.open` through `/rpc`: UTF-8/empty text, image blobs through `/blobs`,
  binary and oversized inline text, project roots and context workspace roots,
  path traversal, absolute escapes, symlink escapes, and invalid parameters.
- Real library submission and digest deduplication for workspace PDF, CSV and TSV;
  `library.get` and `library.pages` work immediately after open, including cells.
- Conversation-only activity attribution, nested agents, exclusion of unrelated
  siblings and expired results, active-first order, the 50-item cap, task-service
  enrichment, and automation progress conversion.
- Per-item event throttling/coalescing and live `/ws` running/terminal delivery.

`internal/workspace/changes_test.go` covers invalid UTF-8, early NUL bytes,
text-to-binary transitions, binary deletion, and the 8 KiB NUL boundary.
`internal/docextract/legacy/legacy_test.go` runs a simulated converter with real
OOXML reader output for XLS/DOC/PPT, plus missing-converter and cancellation
checks. These tests do not require a host LibreOffice install. The API tests
check method/event registry parity and every new DTO's exported schema fields.

Run with the requested memory bound, for example:

```sh
systemd-run --user --scope -q -p MemoryMax=6G go test -race -p 2 ./internal/... ./cmd/...
systemd-run --user --scope -q -p MemoryMax=6G go vet -p 2 ./...
gofmt -l .
```

Operational limit: activity attribution shares the lifetime of the current
in-memory conversation store. Legacy Office preview requires LibreOffice on
Linux; modern Office, PDF, CSV and TSV readers do not need it.

## Verification results

The targeted panel tests pass, including a complete standalone
`go test -race -p 2 ./internal/daemon` run against the final implementation.
The SDK and repository suites (`./pkg/... ./tests/...`) pass with race detection.
`go vet ./...`, `staticcheck ./...`, formatting, the no-bare-goroutine check,
and Darwin/Windows builds pass. The final daemon/CLI cross-builds and daemon
static analysis were repeated after the containment change.

The broad memory-capped internal/CLI race run failed in unchanged tests:

- `internal/automations/TestScriptFailureAndTimeout`: its 30 ms script deadline
  expired before the expected shell error. The isolated rerun also failed.
- `internal/cli/TestExecSignalRecords`: helper readiness timeout; isolated rerun
  passed.
- `internal/daemon/TestEngineAssistantRestartsAndKeepsAttachments`: assistant
  completion timeout; the complete standalone daemon suite passed.
- `internal/gateway/TestWebSocketRPCAndSubscribe`: WebSocket read timeout;
  isolated rerun passed.
- `internal/store/TestExternalLockBusy`: the package timed out after ten minutes,
  with test cleanup blocked in `DB.Close` and its write callback waiting on a
  test channel. The store implementation and test were not modified.

The remaining packages, including workspace, library, extractors, tools and the
binary, passed in that broad run.

These tests and their assertions were not modified. The shared host was under
heavy load during verification. These results do not establish an entirely green
full race run. No frontend build or screenshots were required for this backend
work; the sole web edit adds the event to the default subscription allow-list.
