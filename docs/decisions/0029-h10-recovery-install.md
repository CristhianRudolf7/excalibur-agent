# 0029: H10 recovery, install and update decisions (Sections 17.3, 17.5, 25)

## Turn-interrupt grace is one second

Section 17.5 prescribes a "brief margin" between cancelling the master
turn token and aborting it, without a number. Decision:
`TurnInterruptGrace = 1s`. Rationale: long enough for a clean tool
stop on loopback, short enough that the fast lane stays fast; the
constant is named and tested (`TestStopTurnLeavesBackgroundAlone`
pins the dispatch, the grace is the documented margin). Covered by
`internal/daemon/interrupt.go`.

## Ambiguous "para todo …" orders are rejected

"para todo" is pause-all (17.5) while "para X" pauses target X, so
"para todo ya" is ambiguous between the two. Decision: the fast lane
rejects `todo …` targets with an explicit error instead of guessing a
control target. Rationale: the lane must never guess (a wrong pause
or cancel is worse than an error asking for the exact form).

## CLI update consumes staged manifest and payload files

Section 25.3 describes channel download plus verification but the
transport (HTTP fetch, Harbor artifact) belongs to release tooling,
not the backend. Decision: `excalibur update --manifest F
--payload F --pubkey HEX` verifies signature and hash and runs the
`daemon.Updater` state machine; the agenda `update-check` internal
job stages those files. The same seam keeps the 25.5 rows (bad
signature, drain wait, migration rollback, post-boot rollback)
testable without a network.

## Installer never rewrites user config

`writeInitialConfig` skips an existing `config.toml`. Decision:
reinstalls keep user edits; new defaults arrive via additive merge,
never by overwrite. Covered by `TestInstallKeepsExistingConfig`.

## Boot seam stays narrow and daemon-owned

`internal/orchestrator` (H8) owns run control and `internal/engine`
(H3) owns per-run journal replay, both evolving in parallel.
Decision: `internal/daemon` depends only on narrow local interfaces
(`RunStore`, `RunReconciler`, `AgendaCatchUp`) and on the stable
`internal/journal` classifiers, never on orchestrator or engine
concretes. No shared contract was changed; no additive change was
needed elsewhere.
