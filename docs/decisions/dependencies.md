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

## Planned libraries (from the architecture, Section 3.4)

| Need | Planned library | License (to verify at adoption) |
|---|---|---|
| WebSocket | `github.com/coder/websocket` | ISC (verify) |
| SQLite with FTS5 | `github.com/mattn/go-sqlite3` | MIT (verify) |
| Vector search | `github.com/asg017/sqlite-vec-go-bindings/cgo` | MIT/Apache-2.0 (verify) |
| PTY | `github.com/creack/pty` | MIT (verify) |
| MCP SDK | `github.com/modelcontextprotocol/go-sdk` | (verify) |
| File watching | `github.com/fsnotify/fsnotify` | BSD-3-Clause (verify) |
| tree-sitter | `github.com/tree-sitter/go-tree-sitter` | MIT (verify) |
| Linux syscalls | `golang.org/x/sys/unix` | BSD-3-Clause (verify) |
| Vault crypto | `golang.org/x/crypto` | BSD-3-Clause (verify) |
| System keyring | `github.com/zalando/go-keyring` | MIT (verify) |
| Traces | `go.opentelemetry.io/otel` | Apache-2.0 (verify) |
| Token counting | `github.com/pkoukk/tiktoken-go` | MIT (verify) |
| YAML manifests | (TBD in H13) | (verify; must allow this use) |
| Cron expressions | `github.com/robfig/cron/v3` | MIT (verify) |

None of the above is adopted in wave 0. The YAML struct tags on the
extension manifest are inert until H13 picks a parser.
