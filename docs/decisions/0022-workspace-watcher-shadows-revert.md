# 0022 — Workspace polling watcher, file-copy shadows, revert direction

Date: 2026-10-31. Area: workspace (H7: Sections 9, 13.7, 16).

## What

Three feasible simplifications of the architecture document, all
behavior-preserving for the verification tables:

1. **Polling watcher instead of fsnotify.** Section 16.5 asks for a
   file watcher per active project. `internal/workspace.Watcher`
   polls the project roots (2 s default) under `safego.Go` instead
   of adding `github.com/fsnotify/fsnotify`. No new dependency, no
   platform-specific code, identical Section 16.5 reactions
   (fingerprint invalidation, evidence staleness, graph update,
   directive to the lease holder) and deterministic tests.
2. **Shadow snapshots as file copies, not a git write-tree store.**
   Section 13.7 describes the shadow as a separate git repository
   writing trees through a private index without touching the
   project git. `SnapshotManager` stores content blobs plus a JSON
   manifest under `<dataDir>/shadows` (synchronous at take time, so
   a daemon kill stays restorable) with the same take/diff/restore
   guarantees. Git plumbing stays reserved for the 16.4 worktree
   path, where worktree semantics are actually required.
3. **Tool placement.** The five H7 deferred tools (`file_history`,
   `revert_edit`, `workspace_snapshot`, `worktree`,
   `integration_queue`) are implemented in `internal/workspace` as
   `tools.Tool` values registered through the shared
   `tools.Registry` (no privileged path, Section 14.12), not in
   `internal/tools`, keeping the H3-owned package untouched across
   parallel branches. The history `Revert` merges with
   base=postimage, ours=preimage, theirs=current (undo the recorded
   change onto the current content), so disjoint user edits survive
   and overlaps conflict without overwriting.

## Why

fsnotify would add a cgo-free but platform-dependent dependency for
no behavioral gain in tests; polling with content hashes is simpler,
portable (Linux/macOS/Windows stubs unaffected) and fully
deterministic under a controllable clock. A git-based shadow store
would couple snapshot durability to git CLI availability and
private-index plumbing; the blob+manifest store is crash-safe and
simpler to verify. The revert direction follows the Section 13.7
wording ("aplica una fusión a tres bandas" after later changes):
the merge must undo the recorded delta, not re-apply it.

## Alternative

Adopt fsnotify and a `git write-tree` shadow backend later if
profiling shows polling costs matter on huge trees; both seams
(`Watcher.scanSnapshot`, `SnapshotManager.persistLocked`) are
isolated for that swap without touching callers.
