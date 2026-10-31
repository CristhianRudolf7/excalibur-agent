# Wave 0 — repository skeleton and shared contracts

Contract and backend tests of the wave-0 area: module shape, shared
types, wire contracts, core interfaces and the CLI surface. Later
milestones extend their own documents; this one only grows with
contract changes.

## Contract

Wave 0 delivers the frame other agents plug into, not the features:

- **Module**: `github.com/CristhianRudolf7/excalibur-agent`, stdlib
  only. Full `internal/` tree from the Section 3.4 package table, each
  package with a `doc.go` stating its responsibility and sections.
- **`internal/types`**: ULID identifiers (`prf_` + 26 chars, type
  prefixes, roundtrip-tested time ordering), short codes (`T45`,
  `R12`, `C12.3`, `M2`, never reused), run states with the full
  Section 7.2 transition table, wait/pause/block reasons, derived task
  states, computed criterion states (Section 8.3 algorithm), scopes
  with `ScopeSet`, effect/idempotency/cancel classes, model roles,
  event envelope with opaque cursor, core planning structs from
  Appendix A.
- **`internal/api`**: JSON-RPC 2.0 envelope (request/notification,
  result/error, standard + application codes), one constant per method
  of the Section 3.5 families plus the `task.*` delegation family,
  request/response shapes, typed event payloads. Exported to
  `packages/contracts/*.json` with a parity test: any new method or
  event must be added in both places.
- **Core interfaces + fakes**: `Store` (porter priorities, read pool
  with interactive lane, blob store, migration registry), `Provider`
  (streaming chat, tool calls, usage incl. cache read/write,
  normalized errors, context cancellation), `Tool` (+ validated
  definition, registry, normalized result, resource keys, optional
  probe/compensate, `excalibur-extension.yaml` manifest),
  `internal/platform` (nine interfaces; `*_linux.go` compiling
  placeholders, `*_other.go` `errors.ErrUnsupported` stubs),
  `safego.Go` (recover, slog, owner-failed callback), `internal/config`
  (layered types, Appendix B defaults, range validation).
- **CLI**: `cmd/excalibur` dispatches every Section 3.5/3.6 command;
  unimplemented commands exit 69 with `not implemented yet`. Final
  `exec` codes follow Section 6.5.
- **`pkg/extsdk`**: public, internal-free SDK (Tool, Capability,
  manifest) with a JSON parity test against `internal/tools`.

Naming: state enum values are English (`queued`, `running`, ...); see
`docs/decisions/0002-english-state-enum-values.md` for the mapping.

## Backend tests

- `internal/types`: ULID shape/alphabet/time/order, prefix validation,
  typed-id constructors, short-code parse/format/reject, transition
  table allow/deny, terminal sets, task-state derivation, criterion
  computation, scope roundtrip/reject, cursor roundtrip.
- `internal/api`: method/event uniqueness and shape, contracts parity
  (`methods.json`/`events.json` exact match), envelope roundtrip,
  error shapes, version/method validation.
- `internal/store`, `.../fake`: priority ordering, sentinel
  distinctness, write/read recording, canceled-write discard, blob
  put/stat/GC, migration register/validate/gap/dup.
- `internal/providers`, `.../fake`: usage addition, error retryability,
  scripted chat/stream/emit-abort/error paths.
- `internal/tools`, `.../fake`, parity: name validation (64-char
  boundary), definition validation matrix, registry dup/unknown,
  scripted tool, extsdk JSON parity both ways.
- `internal/platform`, `.../fake`: fake roundtrips (paths, keyring,
  lock, sampler, service, PTY session, isolator).
- `internal/safego`: success path, panic recovery with stack and owner,
  nil-callback survival.
- `internal/config`: defaults equal Appendix B spot values, validation
  accepts defaults and rejects violations.
- `cmd/excalibur`: help/unknown/not-implemented paths, exit-code
  stability.
- Repo-wide: `make verify` (gofmt empty, vet, staticcheck, bare-go
  guard, `go test ./... -race`, darwin+windows builds).

## Deferred (not in wave 0)

No behavior: no adapters, no SQL, no Linux implementation, no journal
or engine logic, no gate, no YAML parsing (tags only), no Harbor or
bench code, no store content. Each lands with its milestone.
