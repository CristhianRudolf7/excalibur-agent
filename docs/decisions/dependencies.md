# Dependencies and their licenses

Wave 0 keeps the module dependency-free: only the Go standard library.
Every package compiles with no entries in `go.mod` beyond the module
line and the Go directive.

## Policy for later waves

Before adding a dependency to `go.mod`:

1. Check that its license permits this use (the project license is
   source-available with commercial restrictions; copyleft or
   commercial-use-restricted dependencies need extra review).
2. Pin the version in `go.mod` and record it here with its license.

## Adopted for the Linux platform (w1-platform, H2/H3 support)

Verified against the module cache (license files shipped with each
module). All are permissive and impose no copyleft or
commercial-use restriction:

| Need | Library | Version | License (verified) |
|---|---|---|---|
| PTY | `github.com/creack/pty` | v1.1.24 | MIT (Keith Rarick) |
| Linux syscalls | `golang.org/x/sys` | v0.35.0 | BSD-3-Clause (Go Authors) |
| System keyring | `github.com/zalando/go-keyring` | v0.2.6 | MIT (Zalando SE) |
| D-Bus (via go-keyring) | `github.com/godbus/dbus/v5` | v5.1.0 | BSD-2-Clause (Georg Reinke et al.) |
| Windows credential stub (via go-keyring, non-Linux builds only) | `github.com/danieljoos/wincred` | v1.2.2 | MIT |
| Shell escaping (via go-keyring) | `al.essio.dev/pkg/shellescape` | v1.5.1 | MIT |

## Planned libraries (from the architecture, Section 3.4)

| Need | Planned library | License (to verify at adoption) |
|---|---|---|
| WebSocket | `github.com/coder/websocket` | ISC (verify) |
| SQLite with FTS5 | `github.com/mattn/go-sqlite3` | MIT (verify) |
| Vector search | `github.com/asg017/sqlite-vec-go-bindings/cgo` | MIT/Apache-2.0 (verify) |
| PTY | `github.com/creack/pty` | adopted (see above) |
| MCP SDK | `github.com/modelcontextprotocol/go-sdk` | (verify) |
| File watching | `github.com/fsnotify/fsnotify` | adopted (see below) |
| tree-sitter | `github.com/tree-sitter/go-tree-sitter` | MIT (verify) |
| Linux syscalls | `golang.org/x/sys/unix` | adopted (see above) |
| Vault crypto | `golang.org/x/crypto` | BSD-3-Clause (verify) |
| System keyring | `github.com/zalando/go-keyring` | adopted (see above) |
| Traces | `go.opentelemetry.io/otel` | Apache-2.0 (verify) |
| Token counting | `github.com/pkoukk/tiktoken-go` | MIT (verify) |
| YAML manifests | `gopkg.in/yaml.v3` for SKILL.md frontmatter (H8/H12, see below); extension manifests still TBD in H13 | MIT (verified for yaml.v3) |
| Cron expressions | `github.com/robfig/cron/v3` | MIT (verify) |

None of the above is adopted in wave 0. The YAML struct tags on the
extension manifest are inert until H13 picks a parser.

## Adopted in H8/H12 (agent w2-skills)

| Dependency | Version | License (verified) | Use |
|---|---|---|---|
| `github.com/fsnotify/fsnotify` | v1.9.0 | BSD-3-Clause (LICENSE file in module cache) | Filesystem watcher for the skill index roots, Section 12.2 |
| `gopkg.in/yaml.v3` | v3.0.1 | MIT (LICENSE file in module cache) | SKILL.md frontmatter parsing, Section 12.1 |

Both are permissive, impose no copyleft or commercial-use
restriction, and are pinned in `go.mod`. `fsnotify` builds on all
three harness platforms, so the `GOOS=darwin`/`GOOS=windows` builds
keep working; only the Linux backend is exercised in tests.

Integrator note (wave-2 merge): `w2-codeintel` adopted
`github.com/fsnotify/fsnotify` v1.10.1 while `w2-skills` adopted
v1.9.0; the merged `go.mod` pins v1.10.1 (same BSD-3-Clause
license, API-compatible for both the skill-index watcher and the
codeintel indexer).

## H1 wave w1-providers-a (2026-10-31)

No new dependencies: the OpenAI-wire adapters (`openaiwire`,
`openai`, `deepseek`, `openrouter`, `ollama`, `generic`) use the
standard library only (`net/http`, `encoding/json`, `bufio`,
`context`, `os`). `go.mod` unchanged, so no license review was
needed beyond confirming stdlib use.

## Adopted in H1 (agent w1-providers-b)

No new module dependencies. `internal/providers`,
`internal/providers/anthropic`, `internal/providers/gemini` and
`internal/providers/registry` use only the Go standard library
(`net/http`, `encoding/json`, `bufio`, `math/rand/v2`). No license
review needed. HTTP/SSE streaming is hand-rolled on `net/http`
because both wire protocols are plain JSON over POST plus
line-delimited events; an SSE or API client library would add
review and pinning cost for no contract benefit.

## Adopted in H2 (wave `w1-store`)

| Dependency | Version | License | Use |
|---|---|---|---|
| `github.com/mattn/go-sqlite3` | v1.14.52 | MIT | Production SQLite backend with FTS5 (`sqlite_fts5` tag), Section 4.1 |
| `github.com/asg017/sqlite-vec-go-bindings` | v0.1.6 | Apache-2.0 (upstream sqlite-vec license; bindings module carries no separate LICENSE file) | sqlite-vec extension (vec0), Sections 4.1/10.8. Pinned, behavior covered by tests |
| `modernc.org/sqlite` | v1.61.1 | BSD-3-Clause (plus bundled SQLite/amalgamation licenses) | Pure-Go fallback backend behind `Store` for tagless, darwin, windows and `CGO_ENABLED=0` builds (Section 3.4 note) |
| `github.com/BurntSushi/toml` | v1.6.0 | MIT | Layered TOML configuration, Section 25.2 point 1 |
| `github.com/klauspost/compress` | v1.20.3 | BSD-3-Clause | Pure-Go zstd for the content-addressed blob store, Section 4.2 |

All five permit this use (permissive licenses, no copyleft, no
commercial-use restriction). `modernc.org/sqlite` pulls its usual
transpiled-support tree (`modernc.org/libc`, `mathutil`, `memory`,
`ccgo`, `fileutil`, `token`, `strutil`, `opt`, `slices`, `hashicorp/golang-lru`...),
all BSD/MIT/Apache-2.0 licensed.

Integrator note (wave-1 merge): the versions above are the ones
actually pinned in `go.mod`. The `w1-store` branch text originally
listed `go-sqlite3` v1.14.32 and `sqlite-vec-go-bindings`
v0.1.7-alpha.2, which did not match its own `go.mod`
(v1.14.52 / v0.1.6); corrected here. Likewise `golang.org/x/sys`
is pinned at v0.49.0 in the merged `go.mod` (minimum required by
the `modernc.org/sqlite` tree) instead of the v0.35.0 adopted by
`w1-platform`; still BSD-3-Clause, API-compatible for the
platform backends.

## Considered but not adopted in w2-agenda (H10/H13, Section 21)

| Need | Candidate | License | Outcome |
|---|---|---|---|
| Cron expressions | `github.com/robfig/cron/v3` | MIT (would need verification at adoption) | Not adopted: `internal/agenda` ships its own 5/6-field parser (`cron.go`) for exact DST semantics (21.1) and to avoid a new dependency; see `docs/decisions/0014-agenda-cron-and-scope.md`. `go.mod` unchanged. |

## Adopted in H7 (wave w2-codeintel, 2026-10-31)

Verified against the module cache (license files shipped with each
module). Both are permissive and impose no copyleft or
commercial-use restriction.

| Need | Library | Version | License (verified) |
|---|---|---|---|
| tree-sitter parsers (cgo) | `github.com/smacker/go-tree-sitter` | v0.0.0-20240827094217-dd81d9e9be82 | MIT (Maxim Sukharev) |
| File watching | `github.com/fsnotify/fsnotify` | v1.10.1 | BSD-3-Clause (Go Authors / fsnotify Authors) |

## Adopted in H10 (agent w2-vault, secrets vault Section 20.1)

| Dependency | Version | License | Use |
|---|---|---|---|
| `golang.org/x/crypto` | v0.58.0 | BSD-3-Clause (Go Authors; LICENSE file in module cache) | XChaCha20-Poly1305 per-entry encryption and Argon2id passphrase KDF, Section 20.1. Pinned in `go.mod`; resolved by `go mod tidy` against the existing `golang.org/x/sys v0.49.0`. |

Permissive license, no copyleft or commercial-use restriction; use
reviewed against the project license policy before adoption.

## Adopted in H13 (wave w3-capabilities, Sections 14.1 and 14.12)

| Dependency | Version | License (verified) | Use |
|---|---|---|---|
| `github.com/modelcontextprotocol/go-sdk` | v1.8.0 | MIT/Apache-2.0 transition (LICENSE file in module cache: all new contributions Apache-2.0, unconsented older code stays MIT) | MCP client over stdio and streamable HTTP, tool listing and calls, `_meta` execution fields, Sections 14.1 and 14.12. Pinned in `go.mod`; resolved by `go mod tidy` (adds `github.com/google/jsonschema-go`, `github.com/golang-jwt/jwt/v5`, `github.com/segmentio/encoding` as indirect). |

Permissive license, no copyleft or commercial-use restriction; use
reviewed against the project license policy before adoption. The
planned-libraries table entry for the MCP SDK is superseded by this
adoption. `gopkg.in/yaml.v3` (already adopted, MIT) additionally backs
`excalibur-extension.yaml` parsing in H13 via `pkg/extsdk`.

## Adopted in H9 (agent w3-daemon, Sections 3.5/5/6)

| Dependency | Version | License | Use |
|---|---|---|---|
| `github.com/coder/websocket` | v1.8.13 | ISC (LICENSE.txt in module cache: copyright Anmol Sethi) | WebSocket side of the gate (`internal/gateway` `/ws`: JSON-RPC over sockets, cursor subscriptions, live event push), the architecture's Section 3.4 choice. Pinned in `go.mod`; builds on darwin/windows (pure Go, no cgo), so cross builds keep working. |

Permissive license, no copyleft or commercial-use restriction; use
reviewed against the project license policy before adoption.

## Adopted in H4/H8 (agent w3-exec-eval, exec and delegation, 2026-10-31)

No new dependencies. `internal/cli` (profiles, exec, task store,
MCP server, MCP install, ACP adapter), the client fragments in
`integrations/clients/` and the bench scripts use the standard
library only; the MCP stdio subset and the JSON-schema subset for
`--output-schema` are hand-rolled (see
`docs/decisions/0025-exec-delegation-no-new-deps.md`). `go.mod`
unchanged.

## Adopted in W8 bench fixes (2026-10-31, Section 23.2)

| Dependency | Version | License | Use |
|---|---|---|---|
| `golang.org/x/crypto/x509roots/fallback` | v0.0.0-20261030153154-30a33513ff6d | BSD-3-Clause (Go Authors; LICENSE file in module cache) | Embedded Mozilla root CAs for the static bench binary: blank-imported by `cmd/excalibur` only, so task images without CA certificates still verify TLS. The verifier uses the fallback only when the system pool is empty/missing. Pinned in `go.mod`. |

Permissive license, no copyleft or commercial-use restriction; use
reviewed against the project license policy before adoption. Pure
Go, no cgo, so `GOOS=darwin`/`GOOS=windows` builds keep working.
