# Assistant file changes

Assistant replies expose optional `changes` in `message.created` and
`conversation.history`. See decision 0095 for attribution and bounds. The JSON
shapes are `FileChange`, `FileHunk`, and `ConversationMessage` in
`packages/contracts/events.json`; Go event types live in `internal/api` and
conversation history uses `gateway.StoredMessage`.

Backend coverage:

- `internal/workspace/changes_test.go`: creation (including empty files),
  modification, deletion, collapsed edits, net-identical omission, trusted run
  filtering, path normalization, final newlines, file/hunk/content limits.
- `internal/api/changes_test.go`: snake_case JSON, optional empty content,
  round-trip and contract field coverage.
- `internal/daemon/changes_test.go`: saved history, published event payload,
  unrelated-run exclusion and mutation isolation.
- `internal/wireengine/leases_history_test.go`: shared tools prefer the
  executing run identity for write, edit, patch and revert operations.
- Existing `TestAssistantToolBoundaryAndTrustedRunTree` covers nested children
  and exclusion of unrelated agents from the trusted turn tree.

Recorded write paths: `write_file`, `edit_file`, `apply_patch`, `revert_edit`.
The patch `delete` operation removes matching text from a file; it does not
unlink the file and therefore reports a modification. Actual deletion versions
are supported by the history aggregation API.

Remaining gaps: shell/terminal commands, document/PDF output helpers, external
MCP/extension/capability tools, and external user processes can write files
without recording workspace history. Their effects are not included in reply
cards. Automatically observing arbitrary filesystem writes is outside this
change. Conversation messages and edit history are currently in memory; this
feature does not survive a daemon restart independently of those stores.

## Validation

The final feature checks passed with `-race`: diff/net-change limits,
JSON/contracts, history/event delivery, trusted descendants, atomic context
identity/cancellation, successful-only patch history, and executing-run
attribution through write/edit/patch/revert. The complete final `wireengine`
race suite also passed.

`gofmt -l .`, `go vet ./...`, `staticcheck ./...`, the no-bare-goroutine check,
and both Darwin and Windows builds passed. Commands used the requested 6 GiB
systemd scope; package race runs used `-p 2`.

The requested master/workspace/daemon/API/command package command failed on
`TestSQLLeaseMirror`'s 80 ms live-lease window under load; its other packages
passed. A subsequent full workspace race run passed, as did a separate run
excluding that timing test.

The broader `go test -race -p 2 ./...` was not green: `TestThreeRoutesAgree`
observed an asynchronous route still in progress, `TestMCPDelegateFlow` timed
out in pipe cleanup, and `TestWalkthrough268EvalProfile` found a reachable
daemon. Those files and the eval profile were left unchanged. This broad run
started during implementation and also used the pre-fix atomic tool context;
its attribution failure was fixed and verified by the final wireengine run.
An earlier gateway websocket timeout did not repeat in the broader run.
