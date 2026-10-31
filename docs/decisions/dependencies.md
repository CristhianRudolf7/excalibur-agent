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
| File watching | `github.com/fsnotify/fsnotify` | BSD-3-Clause (verify) |
| tree-sitter | `github.com/tree-sitter/go-tree-sitter` | MIT (verify) |
| Linux syscalls | `golang.org/x/sys/unix` | adopted (see above) |
| Vault crypto | `golang.org/x/crypto` | BSD-3-Clause (verify) |
| System keyring | `github.com/zalando/go-keyring` | adopted (see above) |
| Traces | `go.opentelemetry.io/otel` | Apache-2.0 (verify) |
| Token counting | `github.com/pkoukk/tiktoken-go` | MIT (verify) |
| YAML manifests | (TBD in H13) | (verify; must allow this use) |
| Cron expressions | `github.com/robfig/cron/v3` | MIT (verify) |

None of the above is adopted in wave 0. The YAML struct tags on the
extension manifest are inert until H13 picks a parser.

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

