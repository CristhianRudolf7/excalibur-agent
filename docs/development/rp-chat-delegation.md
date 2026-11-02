# RP chat delegation backend

Chat `subagent.spawn` now executes the admitted run in the daemon, independently
of the bounded master turn. The initial answer acknowledges the spawn. The
child's completion or failure produces a follow-up assistant message in the
originating conversation through the existing output boundary. File tool edits
carry the child run ID into the shared workspace history and appear in the
follow-up message's changes.

`internal/daemon/chat_delegations_test.go` exercises the actual conversation
intake, assistant engine, subagent tool, orchestrator admission, child engine,
relative file writes in a registered project, journal rows, history attribution,
mailbox delivery, and follow-up persistence. Its scenarios cover completion,
provider failure, pause/resume, cancellation, shutdown, queued admission, and SQL-backed orchestrator execution.
The child model is held open until after the initial acknowledgement, proving
that the master does not wait for long delegated work. Ordinary tests use a
scripted model endpoint and never call a paid provider.

`TestMasterCanSpawnImplementerAndAddressRunID` covers direct implementer
spawning, hierarchy identity, status and message run-ID aliases, mailbox rows,
and subtree authorization. Existing daemon catalog parity and eval pin tests
remain part of the package suite. The eval profile is unchanged.

Verification commands:

```sh
systemd-run --user --scope -q -p MemoryMax=6G go test -race -p 2 ./internal/daemon/... ./internal/orchestrator/... ./internal/master/... ./cmd/...
gofmt -l .
go vet ./...
```

These checks do not establish live-provider availability, vault unlock state,
or automatic continuation of parked work after daemon restart.

Validation results for this change:

- The memory-capped ordinary suite for daemon, orchestrator, master, and command
  packages passed.
- Focused race regressions passed, including all seven delegation scenarios,
  direct implementer spawning, run-ID controls, valid finish transitions, daemon
  catalog parity, and the frozen embedded/eval surface checks.
- The final complete daemon, orchestrator, and master race suites passed. The
  command-package race suite initially hit `TestSIGTERMContext` startup timeout
  and passed in an isolated rerun (`go test -race -p 2 ./cmd/...`, also capped
  at 6 GB).
- Repository-wide vet, staticcheck, formatting, the goroutine-launch check, and
  Darwin/Windows cross-builds passed.
- A broader repository race attempt reported `TestExecCommandSignal/terminated`
  exceeding its simulated-server deadline. The shared host was heavily loaded;
  the remaining broad run was stopped after that failure. An earlier superseded
  race run also hit library-processing and signal-test deadlines. These runs do
  not establish a green repository-wide race suite.
