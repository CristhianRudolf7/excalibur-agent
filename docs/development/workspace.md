# H7 workspace half — projects, scopes, leases, worktrees, history

Contract and backend tests of the H7 workspace area (wave
w2-workspace): project registry and scopes (Section 9),
work modes, write leases, work partitioning, worktrees and the
integration queue (Section 16), local edit history, three-way
revert and shadow snapshots with the five deferred tools
`file_history`, `revert_edit`, `workspace_snapshot`, `worktree`
and `integration_queue` (Section 13.7).

All logic lives in `internal/workspace` as orchestrator-independent
library code: it depends only on `internal/types`,
`internal/platform` (FileLock), `internal/tools` (Tool interface
and Registry) and `internal/safego`. The orchestrator (H8) adopts
it by calling `NotifyRunState` on run transitions, `Heartbeat` on
the run heartbeat, and `RegisterWorkspaceTools` to expose the five
tools. Persistence behind the store tables (`projects`, `leases`,
`file_versions`, Appendix A) has in-memory implementations used by
every test; the SQL binding lands with the storage milestone behind
the same row shapes.

Architecture reference: Sections 9, 13.7, 16. Appendix B values
used verbatim (lease wait 30 s, expiry 10 min without heartbeat).
Decisions in `docs/decisions/0022-workspace-watcher-shadows-revert.md`.

## Contract

- **Projects** (`projects.go`): `Registry` with `Register`, `Get`,
  `List`, `Update`, `Archive`; roots canonicalized
  (absolute, symlink-resolved, closest-existing-ancestor fallback
  so new files resolve too). `Resolve` implements Section 9.2
  deterministically: longest-prefix match, tie-break by run
  project, then mentioned project, then most specific root; the
  full candidate list is always returned. Archived projects never
  resolve.
- **Conversation scopes** (`projects.go`): `ConversationScopes`
  (default `general`), `DecideSwitch` (explicit command switches;
  high-confidence inference ≥ 0.7 switches for the turn;
  low-confidence inference followed by a write asks first,
  Section 9.3). `TagResults`/`RenderTagged` label multi-project
  answers so lists never mix silently (9.4). `DualScope` records
  the explicit double scope of transversal tasks (9.4).
- **Isolation** (`projects.go`): `EnforceRead`/`EnforceWrite`
  reject out-of-scope access with `*ScopeError`; every query
  carries a scope set (9.5). `DefaultReadScopes` is general plus
  the project. `ClassifyCandidate` parks scope-ambiguous facts in
  `conversation:<id>` until consolidation or the user clarifies
  (9.6).
- **Work modes** (`modes.go`): `ModeFor` implements the Section
  16.1 table (shared by default; worktrees for >1 parallel
  implementer in a git project; brief tasks and submodule projects
  always shared). `ModeStore` pins per-project overrides with the
  same safety rails (worktrees need git and no submodules).
- **Leases** (`leases.go`): `LeaseManager` serializes writers per
  canonical path with 30 s waits naming the holder and its
  activity (`*HeldError`, so the agent works elsewhere or asks
  the parent to arbitrate). `Heartbeat` renews running,
  waiting(tool) and waiting(subagents) leases through long tools;
  `NotifyRunState` releases on paused, blocked, waiting(user)
  and terminal states, so files are never immobilized for hours;
  re-acquire on resume meets the read fingerprint (13.6).
  `Expire` sweeps 10 min without heartbeat; `Snapshot` exposes
  the table over time. Cross-process coordination uses
  `platform.FileLock` when configured (consultative, like flock),
  preferring `AcquireContext` so a lost race never leaves an
  orphaned holder.
- **Partitioning** (`modes.go`): `AssignFileSets` keeps modules
  with their direct dependencies on one child (round-robin over
  sorted modules, deduped), detects overlaps and warns the parent
  via `ParentNotifier` with the files, child pairs and the
  serialize/single-owner/worktrees suggestion (16.3).
- **Worktrees and queue** (`worktree.go`): `WorktreeManager`
  creates the integration branch and per-child branches plus
  worktrees under `<dataDir>/worktrees/<project>/<child>`,
  outside the user tree; `HasSubmodules` (`.gitmodules` or
  `submodule status`) forces shared mode. `IntegrationQueue`
  integrates head-to-tail: rebase in the branch's own checkout
  (a branch checked out in a worktree cannot be rebased from
  elsewhere), parent-side `ResolveFunc` attempt with both diffs,
  impacted `TestFunc` gate, fast-forward from the integration
  checkout, or return to the child with the report. `Propose`
  offers the branch at task close; `ConfirmMerge` needs explicit
  user confirmation (`ErrNeedConfirmation` otherwise), and push
  is never performed (16.4, 6.4).
- **Watcher** (`watcher.go`): polling `Watcher` per active roots
  under `safego.Go` with `DefaultWatcherExclusions`
  (`.git`, `node_modules`, `target`), project `.gitignore`
  support (subset: `*`, `**`, dir rules, `!` negation) and
  `MarkAgentWrite` suppression. Each external event invalidates
  fingerprints, marks evidence stale, updates the graph and
  directs the lease holder ("<path> changed outside Excalibur"),
  per Section 16.5.
- **History and revert** (`history.go`): content-deduplicated
  blobs (`blob-sha256:` refs, like the H3 file tools) with
  versions carrying run, tool call and time, so a kill between
  intent and effect stays reversible. `Revert` restores the
  preimage when the file still holds the postimage, else merges
  base=postimage/ours=preimage/theirs=current; conflicts return
  `*ConflictError` with both diffs and leave the file untouched.
  `Merge3` is line LCS with a prefix/suffix fallback past 4000
  lines per side.
- **Snapshots** (`snapshot.go`): `SnapshotManager` takes shadow
  images of the project tree (take at exec start and after each
  step), skipping watcher exclusions, binaries and files over
  1 MiB (recorded in `Skipped`). Manifest plus blobs persist
  synchronously under `<dataDir>/shadows`, so images survive a
  daemon kill; `Diff` and `Restore` (with `PruneAdded`) undo
  command effects without touching the project git. In shared
  mode no commit is made on the user branch unless the project
  sets `auto_commit`; reversibility comes from history and
  shadows (13.7).
- **Tools** (`wtools.go`): the five deferred tools implement
  `tools.Tool` with validated definitions (names, effects,
  cancel modes per 13.1) and register through the shared
  `Registry` like any extension (14.12). `RegisterWorkspaceTools`
  wires them; `WorkspaceToolNames` lists them for the eval
  profile.

## Backend tests

- `projects_test.go` (9.6 rows): nested-root resolution by
  specificity with the full candidate list, run-project and
  mentioned-project tie-breaks, symlink canonicalization,
  unknown-file error, duplicate names, switch decisions
  (explicit/high/low-confidence+write), tagged rendering, scope
  enforcement plus dual-scope transversal reads, candidate
  holding scope, conversation scope defaults.
- `leases_test.go` + `leases_lock_test.go` + `leases_flock_test.go`
  (16.6 row 1, 16.2): 30 s-class wait reporting holder and
  activity; heartbeat renewal across 9 simulated minutes during a
  long tool and 10 min expiry without heartbeat; release on
  pause/block/waiting(user), keep on running/waiting(tool)/
  waiting(subagents), release on terminal; sorted snapshots and
  sweeps; holder-scoped idempotent release; concurrent
  serialization; two-daemon coordination through a cancel-safe
  shared lock (portable fake) and through real Linux flock.
- `modes_test.go` (16.1, 16.6 row 3): mode table incl. submodule
  and brief-task rails, override with safety fallbacks, overlap
  notice naming the shared file with the parent suggestion,
  disjoint silence, dependency colocation, duplicate-free sets.
- `worktree_test.go` (16.6 row 4): two clean children plus one
  planted conflict — rebase, impacted-test gate (failure
  returns with the report), fast-forward of the integration
  branch, proposal without touching the user branch,
  `ErrNeedConfirmation` without the user, submodule detection
  forcing shared mode (real git repos in temp dirs; skipped
  without git).
- `watcher_test.go` (16.6 row 5): leased-file edit invalidates
  fingerprints, stales evidence, updates the graph and directs
  the holder; `node_modules` ignored; `.gitignore` with `!`
  re-inclusion; agent writes suppressed.
- `history_test.go` + `snapshot_test.go` (13.10/13.7 rows): clean
  revert, disjoint three-way merge keeping the user line,
  conflict preserving the file with both diffs, preimage
  survival (crash between intent and effect), snapshot
  take/diff/restore with prune, disk persistence across manager
  restarts, binary/huge skips.
- `concurrency_test.go` (23.5 `concurrency` suite): two agents
  race on one file — the lease serializes them and both disjoint
  edits land with two recorded versions; plus tool registration
  (unique validated names through the shared registry) and
  `file_history`/`revert_edit`/`workspace_snapshot` round trips
  through the tool surface.

## Deferred (not in w2-workspace)

SQL bindings of the registry, lease table and file versions
behind `Store`/porter (H2 storage milestone owns the tables;
shapes match Appendix A); orchestrator wiring (`NotifyRunState`
on transitions, `Heartbeat` on the run heartbeat, queue ownership
by the parent run, impacted-test selection from the code graph in
`internal/codeintel`); LSP diagnostics in edit results (13.8);
`task_db` project operations against this registry (H5 tasks area
owns that tool); archival retention of snapshots and versions
(4.7).
