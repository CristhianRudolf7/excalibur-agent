# 0036 TB21 Harbor pinning and static build

Date: 2026-10-31. Scope: Section 23.2, Appendix F (native Terminal-Bench
2.1 support).

## Pinned versions

- Harbor 0.25.0, PyPI `harbor==0.25.0`, git commit
  `fdba28fc411e47e8765420a1a063b4918fce3def` (tag `v0.25.0`, verified
  via the GitHub tags API 2026-10-31). License: Apache-2.0 (PyPI
  metadata; no Go dependency change).
- Docker Engine 29.1.3 (Ubuntu `docker.io` 29.1.3-0ubuntu3~24.04.2) plus
  Compose v2.29.7 (manual plugin install; Ubuntu `docker.io` ships
  without `docker compose`). User added to the `docker` group; socket
  mode 666 as a fallback for Harbor runs.
- TB21 dataset `terminal-bench/terminal-bench-2-1`, digest
  `sha256:7d7bdc1cbedad549fc1140404bd4dc45e5fd0ea7c4186773687d177ad3a0699a`
  (revision 6, version 2.0.2, tag `latest`). Verified via
  `harbor version show ... --json` before each bench run; mismatch is a
  warning, not a silent drift.
- TB40 dataset `terminal-bench/terminal-bench`, digest
  `sha256:39d9f44b40420cde8fdcc087579c0d72a7e14fa3656d603c3f0d22fb35e27732`
  (revision 4, version 4.0.0, tag `4.0.0`). Same shape ready in
  `bench tb40`, not yet run live.

## Static build

The architecture asks for cgo+musl static (SQLite inside). On this host
the musl sysroot lacks `sqlite3.h`/libc headers for the sqlite-vec cgo
binding, so `CGO_ENABLED=1 CC=musl-gcc` fails. The bench uses
`CGO_ENABLED=0` pure-Go static instead (modernc SQLite fallback): still
a single static binary with no system packages that runs in any task
image, with sha256 published per build. `scripts/bench/build.sh`
defaults to pure-Go static; `--musl` keeps a best-effort cgo+musl path
for hosts with a full sysroot. This is the only deliberate deviation
from Section 23.2 packaging, and it preserves the portability
requirement.

No new Go dependencies; `go.mod` unchanged.
