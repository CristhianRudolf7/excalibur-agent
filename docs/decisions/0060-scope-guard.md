# 0060: Deterministic scope guard and eager input preservation

The director requests additional completion checks and input preservation around
Sections 7.5, 13.7 and 18.1. The engine now compares explicit filesystem scope
constraints with an initial snapshot and refuses completion when a failed
check's expected values change. The switch is `EXCALIBUR_SCOPE_GUARD`, default on.
Shared tool and connector contracts are unchanged. No dependency is added.

Copies are taken before any tool call rather than attempting to classify shell
commands as read-only. Database inspection may mutate or delete companion files;
eager preservation is the closest reliable interpretation of protecting before
the first mutating access. Copies retain companion filenames in private unique
directories under `/tmp/excalibur-originals/` and their paths are shown to the agent.

Natural-language constraints have a bounded deterministic grammar. Arbitrary
numeric semantics and universal quantification cannot be verified reliably from
file hashes. Supported counts operate on file bytes/lines and JSON arrays;
remaining literal constraints stay in verification context. The alternative,
guessing semantic acceptance tests, risks rejecting correct results. This is a
limitation of the requested deterministic approach, not a claim of general
natural-language verification. Failed-check matching is heuristic and may flag
legitimate expectation corrections; those must be reviewed rather than silently
accepted as evidence.

Initial snapshots and failed-check signatures are persisted outside the model
and restored by run ID after interruption. CLI evidence records activation and
finish findings in `scope_guard.json`; the existing event stream carries the
same report. Legacy text completion uses the existing bounded pending-state
policy, while explicit finish refuses until corrected or the budget expires.

A process started at the filesystem root has no identified working tree.
Enumerating the operating-system tree encounters virtual files rather than
project artifacts. At that boundary the guard snapshots explicitly named paths;
tree-wide restrictions require a project directory. This keeps input protection
active without silently treating system files as a project snapshot.
