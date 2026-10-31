# Changelog

All notable changes to Excalibur are recorded here. Nothing below
describes as implemented what is only planned.

## [Unreleased]

### Added

- Wave 0 contracts skeleton (H0): `go.mod`, full `internal/` package
  tree with responsibilities, `cmd/excalibur` CLI command surface with
  stable exit codes, `pkg/extsdk`, `packages/contracts` JSON schemas.
- Shared domain types (`internal/types`): ULID identifiers with type
  prefixes and short project codes, run/task/criterion states with the
  Section 7.2 transition table, scopes, effect classes, model roles,
  event envelope with cursor, core planning structs.
- Wire contracts (`internal/api`): JSON-RPC 2.0 envelope, method names
  and request/response shapes for every family, typed event schemas;
  parity-tested exports under `packages/contracts`.
- Core interfaces with in-memory fakes: `Store` (+ porter, read pool,
  blob store, migrations), `Provider` (streaming chat, tool calls,
  usage with cache read/write, normalized errors), `Tool` (+ registry,
  `excalibur-extension.yaml` manifest), `internal/platform` (Linux
  placeholders, `!linux` stubs), `safego.Go` with panic isolation and a
  bare-`go` guard script, layered `internal/config` with Appendix B
  defaults.
- Verification harness: `Makefile` (`verify`, `test`, `lint`, `cross`),
  GitHub Actions workflow, `AGENTS.md`, architecture notes, decision
  records, per-area development docs.
