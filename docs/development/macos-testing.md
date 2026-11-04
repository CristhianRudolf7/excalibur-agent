# macOS test-suite readiness

## Compilation baseline

On branch `mac-ci`, the initial `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64
go vet -p 2 ./...` passed on Linux. No test build tags or test-file renames
were needed for point 1. Existing Linux-suffixed tests and Linux build constraints
already exclude Linux-only symbols, including `LinuxFileLock` and `Pdeathsig`.
The cross vet includes Darwin tests but cannot execute them. Native CGO,
SQLite extension loading, subprocess behavior, and race detection require a Mac.

## Runtime audit and changed tests

Only test files change Go behavior in this task. Production behavior, tool definitions and the eval
profile hash are unchanged. Linux assertions remain intact.

| Test file | Test | Change and reason |
| --- | --- | --- |
| `cmd/excalibur/install_update_test.go` | `TestCLIInstallClean` | Skip outside Linux with an explicit XDG/systemd reason. The assertions verify Linux unit contents and directories; macOS autostart uses LaunchAgents. Update/signature tests in the same file still run everywhere. |
| `internal/codeintel/diagram_index_test.go` | `TestDiagramPathConfinement` | Resolve the expected root with `EvalSymlinks`, assert the exact canonical subtree, and pass the returned canonical root into indexing. macOS temporary paths can use `/var` while canonical paths use `/private/var`. Escape rejection and indexed-file assertions remain. |
| `internal/daemon/panel_test.go` | `TestFilesOpenWorkspaceAndKinds` | Canonicalize the expected opened-file path for all six content kinds. Project roots and file-open results are canonical, while `t.TempDir` may contain a symlink. Preserve exact content, kind and byte-size checks. |
| `internal/robotlab/environment_test.go` | `TestChildEnvironmentAllowlist` | On Darwin, check propagation of the rendering backend chosen by the per-OS helper; keep the independent literal `osmesa` expectation on Linux. Backend selection itself belongs in Darwin backend tests. |
| `internal/robotlab/sidecar_test.go` | `TestProtocolHelper` | Apply the same platform-aware environment expectation inside the subprocess helper, retaining the literal Linux OSMesa assertion. |
| `internal/tools/documents_write_office_test.go` | `TestOfficeToolsDeferredPipeline` | Compare the resource lock key with the canonical generated document path. The symlinked-temp run reproduced this failure too; keep the exact key and single-resource assertions. |
| `internal/tools/pdf_ops_test.go` | `TestPDFOpsDefaultAndSecretHook` | Resolve the expected generated file with `EvalSymlinks` before comparing it with the canonical PDF output path. Metadata, encryption, secret handling and confinement assertions remain. |
| `internal/tools/pdf_ops_test.go` | `TestPDFOpsConfinementAndResourceKeys` | Canonicalize the existing root before constructing expected input/output lock keys, including the output directory that does not exist yet. Keep exact key ordering and both confinement assertions. |

The audit searched all Go test files for live OS paths, process commands,
GNU-only flags, filesystem and path assumptions, sockets, and signal/error
assertions, then inspected the matching tests and their execution paths.
The following existing coverage requires no change:

| Area | Disposition |
| --- | --- |
| `/proc`, `/sys`, cgroups | Live Linux backend tests already have Linux suffixes/constraints. `internal/envsnap` resource/OOM tests and `internal/tools/runtime_projection_test.go` populate explicit fixture roots and can run on macOS. Do not skip these parser/logic tests. |
| `/run/user`, XDG, systemd | The changed CLI installation test is the live Linux assumption. Daemon installer tests inject directories, service managers and probes; watchdog tests inject notification senders and never open `/run/notify`. |
| `Pdeathsig`, Linux process tables and namespace isolation | Platform tests have Linux constraints. Shared terminal-session, self-protection and diagram isolation tests already skip their Linux-specific live process assertions with reasons. Pure process classification/tree tests stay enabled. |
| GNU `sed -i`, `date -d`, `readlink -f`, `stat -c`, `timeout`, `nproc`, `free`, `ps` output | The `sed -i` strings in engine manual/probe trial tests are parsed with fake execution; they do not run sed. Task command examples likewise use simulated history. No additional live GNU-only invocation was found in editable untagged tests. |
| Optional command tools | Quick verifier tests already check `task.Needs` with `LookPath` (including `sha256sum`) and skip unavailable tools. Docker and live/provider checks retain their existing opt-in gates. No paid API is needed by the ordinary suite. |
| `/bin/true` versus `/usr/bin/true` | The absolute true path is in an existing Linux service test; portable tests use shell builtins or `LookPath`/`exec.Command("true")`. |
| Case-insensitive APFS | The `A`/`a` registry-name test creates no files with those names. No colliding on-disk `A`/`a` fixture pair was found. Case-sensitive lexical/parser assertions stay enabled. |
| Temporary directory aliases | The canonical-path assertions above needed fixes. A Linux race run with `TMPDIR` pointing through a symlink reproduced the diagram and Office lock-key failures before the fixes. This exercises canonical versus lexical paths without changing production code. |
| UNIX socket length | The only direct test listener using a filesystem UNIX socket is `internal/tasks/artifacts_ipc_linux_test.go`, already Linux-only. Shared daemon/gateway tests use TCP loopback. No macOS test socket shortening was needed. |
| `SIGPIPE`, `ECONNRESET` | No live test pins one platform's broken-pipe/reset errno or message. Error-classification fixtures are strings, and signal-exit tests launch owned POSIX children and assert POSIX status. Keep these enabled on macOS. |

Native macOS execution remains necessary for process groups, detached survival,
PTYs, file watching, RSS sampling, PDF workers, Keychain and LaunchAgents.
This branch starts from Darwin placeholders. Their replacement belongs to the
platform workers; retain the portable end-to-end assertions so CI exposes missing
backends rather than hiding them with blanket non-Linux skips. The Robot Lab
environment tests verify propagation of the selected Darwin rendering backend;
the Darwin backend worker must test the backend selection itself.

## GitHub Actions

`.github/workflows/macos.yml` runs on manual dispatch and pushes to
`platform-mac` only. The requested matrix is `macos-latest`/arm64 and
`macos-13`/amd64, with independent failures. Each Go job checks the native
architecture and default CGO availability, builds, vets and runs the full race
suite with two package workers. The test step has a 60-minute limit and Go's
internal test timeout is 55 minutes so ordinary hangs produce diagnostics before
the runner cancels the step. The separate `CGO_ENABLED=0` build also runs after
an earlier failure unless the workflow was canceled. Failed jobs upload logs.

The Makefile currently uses plain `go build ./...` and `go test ./... -race`;
it does **not** specify build tags. Section 25.1 and
`internal/store/driver_sqlite3.go` select the production mattn/sqlite-vec backend
with `sqlite_fts5`. The workflow uses `GOFLAGS=-tags=sqlite_fts5` for build, vet
and tests, so the CGO steps exercise that backend. The no-CGO build selects the
existing pure-Go fallback even with this tag. `verify.yml` is unchanged.

The web job runs on `macos-latest`. There is no `.nvmrc`; setup-node reads
`engines.node` (`>=22`) from `apps/web/package.json`. pnpm/action-setup reads the
root `packageManager` (`pnpm@12.16.1`). Installation uses the root frozen
workspace lockfile and filters to the web package and its dependencies. Then
`pnpm typecheck`, `pnpm lint`, `pnpm test`, and `pnpm build` run in `apps/web`.
Workflow YAML structure and each shell script were checked locally; the macOS
jobs have not been dispatched from this Linux worktree.

### Runner availability blocker

GitHub retired the hosted `macos-13` image, so the workflow uses `macos-15-intel` for the Intel job in December 2025; see the
[official runner-images retirement notice](https://github.com/actions/runner-images/issues/13046).
The requested label remains in the matrix to preserve the assigned baseline,
but its Intel job cannot execute on GitHub-hosted runners. Before dispatching,
the integration owner must select an available Intel label (for example
`macos-15-intel`, subject to repository availability) or provide a suitably
labeled Intel runner. This workflow does not silently replace the requested OS.
The [GitHub runner image catalog](https://github.com/actions/runner-images)
tracks the available architectures and labels. Native macOS runtime validation
and a successfully scheduled Intel job remain open.

## Makefile cross-build audit

`make cross` currently runs `GOOS=darwin go build ./...` and
`GOOS=windows go build ./...`. It inherits `GOARCH`, so on this amd64 Linux
machine it does not cover Darwin arm64. The Makefile is unchanged in this task.
The proposed additive target for the integration owner is:

```make
.PHONY: cross-darwin
cross-darwin:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./...
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build ./...
```

The existing `cross` target could then depend on `cross-darwin` while retaining
its Windows build. These builds exercise the pure-Go fallback; native CGO and
FTS5 remain separate checks in the macOS workflow.

## Shared production blocker: canonical harness roots

The full tools race suite with a symlinked `TMPDIR` reproduced an additional
failure in the unchanged `TestBoundaryReadsRefusedAndSymlinkResolved`:
`symlink to harness log readable`. There were no other failures in that run;
the corrected Office and PDF path assertions passed.

`CheckPathBoundary` in `internal/tools/taskboundary.go` resolves the candidate
file through `EvalSymlinks`, but `pathOutsideBoundary` compares it with the
unresolved `ExecScope.HarnessPaths`. If a root uses a temporary-directory alias,
a link resolves to its canonical spelling and misses that prefix comparison.
`internal/cli/exec_scope.go` currently makes roots absolute without resolving
aliases. The same mismatch can occur with macOS `/var` and `/private/var`.

This is a production behavior issue, not an incorrect test expectation. The
existing assertion is retained. This task may edit shared tests but may not edit
these shared production files, so it cannot close this blocker. The integration
owner needs a minimal Darwin-aware canonical-root comparison in
`internal/tools/taskboundary.go` (with a per-OS helper if necessary to preserve
Linux behavior), retaining lexical roots as well as resolved aliases. Existing
symlink-confinement assertions must pass after that change; do not hide this
failure by canonicalizing only the test fixture or skipping the test on Darwin.

## Final local verification

All commands ran at nice level 19 with two Go package workers where supported.
Race runs used systemd user scopes with `MemoryMax=4G`, `MemorySwapMax=0`, and
final package checks used `GOMAXPROCS=2`.

| Check | Result |
| --- | --- |
| `gofmt -l .`, `git diff --check` | Clean |
| `go vet -p 2 ./...`, `staticcheck ./...` | Pass on Linux |
| `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go vet -p 2 ./...` | Pass, including Darwin tests |
| `CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -p 2 ./...` | Pass |
| `CGO_ENABLED=0 GOOS=windows go build -p 2 ./...` | Pass |
| `go build -p 2 ./...` | Pass on Linux |
| `scripts/check-no-bare-go.sh` | Pass |
| Workflow YAML, trigger/matrix/tag assertions and shell syntax | Pass |

Full Linux package race runs (`-race -p 2 -count=1`) passed:

| Package | Duration | Environment |
| --- | --- | --- |
| `cmd/excalibur` | 79.158s | Symlinked temp root; `-timeout 55m` |
| `internal/codeintel` | 55.384s | Symlinked temp root |
| `internal/daemon` | 350.963s | Symlinked temp root; `-timeout 55m` |
| `internal/robotlab` | 3.105s | Symlinked temp root |
| `internal/tools` | 108.352s | Normal Linux `TMPDIR=/tmp`; `-timeout 55m` |

Earlier loaded-host runs hit unchanged subprocess startup deadlines and the
unchanged three-second OCR preparation deadline. The initial daemon run also
exhausted Go's default ten-minute package budget near the end of the suite.
Retries passed without modifying those individual assertions; the daemon and
command retries used the workflow's 55-minute global test budget. The separate
symlinked-temp tools run failed only the existing harness-root boundary test
(84.820s), which remains the shared production blocker described above.

No Darwin executable, native CGO/FTS5 test suite or macOS web job ran on this
Linux machine. The required native execution and the retired Intel runner label
remain open; these local passes do not establish that the full macOS suite passes.
