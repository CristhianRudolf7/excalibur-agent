# RP chat delegation backend

Chat `subagent.spawn` now executes the admitted run in the daemon, independently
of the bounded master turn. The initial answer acknowledges the spawn. The
child's completion or failure produces a follow-up assistant message in the
originating conversation through the existing output boundary. File tool edits
carry the child run ID into the shared workspace history and appear in the
follow-up message's changes.

Software requests receive a system note at the end of the current turn context,
with up to ten registered projects (name, ID, first root and a short description).
The note explains that an implementer child can read and edit files, run commands
and tests, and start previews using `card.scope: "project:<id>"`. It requests a
clear goal, deliverable and done criteria. TDR/app-building requests and
code/repository/bug/test/implement/deploy terms activate the note; unrelated chat
does not. Project data is bounded JSON, kept out of the stable master profile
and conversation history. Embedded/eval definitions and compiler layers are
unchanged.

When no project fits or a new system is requested, the master can omit the card
scope. A root chat spawn with an omitted, empty or `general` scope creates a
fresh Git workspace under `AssembleOptions.DataDir/workspaces/<slug>-<random>`
and registers it through the existing project registry before spawning. The
project name comes from the card name (falling back to its goal) with a unique
suffix; the goal becomes its description. The daemon supplies the new project
scope to the orchestrator, so the child identity, run scope and working directory
agree. It does not reuse an unrelated project merely because only one exists.
Explicit scopes remain authoritative, and nested children keep the existing
workdir inheritance behavior. A repeated idempotency key within a root turn
reuses its prepared project scope.

Provisioning requires a configured daemon data directory and Git on PATH.
Directory creation uses a confined filesystem root, rejects a symlink at
`workspaces`, and uses bounded ASCII slugs with random suffixes. Git path and
template environment overrides are removed for initialization; no shell is
invoked. Failed initialization removes the partial workspace without registering
a project. Successful projects remain registered independently of child outcome.
The context and delegation tests cover the reported TDR request at the model
boundary, unrelated chat, prefix isolation, empty/existing project inventories,
queued workspace children, actual relative file writes, explicit scopes,
idempotent preparation, path containment and provisioning failures.

`internal/daemon/chat_delegations_test.go` exercises the actual conversation
intake, assistant engine, subagent tool, orchestrator admission, child engine,
relative file writes in a registered project, journal rows, history attribution,
mailbox delivery, and follow-up persistence. Its scenarios cover completion,
provider failure, pause/resume, cancellation, shutdown, queued admission, and SQL-backed orchestrator execution.
The child model is held open until after the initial acknowledgement, proving
that the master does not wait for long delegated work. Ordinary tests use a
scripted model endpoint and never call a paid provider.

Chat delegations also inherit document attachments from the last five user
turns of the originating conversation. The daemon recognizes the composer's
`[document:<id>]` markers, structured attachment IDs and legacy gateway lines.
Document IDs mentioned in the card take priority and can reference older
documents. Selection is captured at spawn, so queued work retains its inputs
when newer conversation turns arrive. At most 20 unique documents are included;
library scope checks still apply.

The child prompt's `Attached documents` section lists names, IDs, status and
extracted text, capped at 40,000 Unicode characters per document and 80,000
total text characters. Truncation, unavailable documents and pending extraction
are explicit. With a working directory, the daemon copies original blobs to
`docs/<source-name>` and streams complete Markdown text to `docs/<stem>.md`
(`.extracted.md` for Markdown originals), mentioning both paths in the prompt.
Existing files are retained and labeled as potentially different content.
Project-relative exclusive creation prevents overwrites and writes through
symlinks outside the project. Copy failures preserve the inline document text;
pending or failed extractions do not leave empty or partial Markdown files.

Attached-document children with implicit tool grants can use `library_search`
and its daemon page-reading companion `library_view` when the profile permits
library search. Explicit card tool grants and profile removals remain
authoritative. No attachments leave the prompt and filesystem unchanged.
`chat_attachments_test.go` covers selection, Unicode budgets, file preservation,
complete Markdown, path handling, missing inputs, cancellation and tool grants.
The delegation suite verifies document text and paths at the model boundary,
successful child `library_view` execution and attachment retention while queued.

`TestMasterCanSpawnImplementerAndAddressRunID` covers direct implementer
spawning, hierarchy identity, status and message run-ID aliases, mailbox rows,
and subtree authorization. Existing daemon catalog parity and eval pin tests
remain part of the package suite. The eval profile is unchanged.

Verification commands:

```sh
systemd-run --user --scope -q -p MemoryMax=6G nice -n 19 go test -p 2 -count=1 -race -timeout 20m ./internal/daemon ./internal/wireengine ./internal/eval
gofmt -l .
go vet ./internal/daemon
go build ./...
GOOS=windows go build ./...
GOOS=darwin go build ./...
```

These checks do not establish live-provider availability, vault unlock state,
or automatic continuation of parked work after daemon restart.

Project/workspace delegation validation passed: the complete daemon, wireengine
and eval race suites (including frozen embedded/eval tool-surface checks),
repository formatting, daemon vet, and Linux, Windows and Darwin builds. The
classroom delegation fixture now supplies a temporary data directory, matching
production startup; its delegated app-context/service check also passed alone.
No live provider calls were used.

Earlier attachment-delegation validation results:

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
