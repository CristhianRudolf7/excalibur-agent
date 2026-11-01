# Excalibur

Excalibur is a personal AI assistant and programming harness: a single
Go binary that receives requests, converses, plans, executes tools,
coordinates subagents, remembers, verifies and recovers from failures.

This repository holds the harness. The web interface is out of scope;
it talks to the daemon through the JSON-RPC 2.0 API in
[`packages/contracts`](packages/contracts). The full text of the
project license is in [`LICENSE`](LICENSE) (source-available, commercial
use requires written permission).

## Status

Wave 0 (contracts skeleton) is done: shared types, API contracts, core
interfaces with in-memory fakes, the CLI command surface and the
verification harness. No milestone behavior is implemented yet; see
[`ROADMAP.md`](ROADMAP.md) and `docs/development/`.

## Layout

- `cmd/excalibur` — the single binary: daemon, CLI and exec.
- `internal/` — the core packages (types, config, store, providers,
  tools, platform, gateway, engine, memory, ...).
- `pkg/extsdk` — public SDK for building extensions.
- `packages/contracts` — shared JSON schemas of the wire protocol.
- `docs/` — architecture notes, decisions, per-milestone development
  docs, research, store and user-guide docs.
- `integrations/` — Harbor adapter and client fragments.
- `scripts/bench` — benchmark launchers.
- `store/` — skill/tool catalog, examples, installer.
- `tests/` — integration tests and benchmark suites.

## Development

See [Adding functionality safely](docs/development/extending.md) for extension
contracts, registration paths, store limitations and pre-merge checks.

## Verification

```sh
make verify   # gofmt, vet, staticcheck, bare-go check, tests -race, cross builds
make test     # go test ./... -race
make lint     # gofmt -l ., vet, staticcheck, bare-go check
make cross    # GOOS=darwin / windows builds
```

## Source of truth

Behavior is specified by the architecture document (outside this
repository, never committed here). Code references its sections by
number only.
