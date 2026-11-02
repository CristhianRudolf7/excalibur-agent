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

## W17 shell inspection

`mvdan.cc/sh/v3 v3.12.0` (BSD-3-Clause, reviewed upstream
[license](https://github.com/mvdan/sh/blob/v3.12.0/LICENSE)) supplies the
shell syntax parser for the terminal self-kill guard. No shell interpreter
or additional runtime executable is introduced.

## Adopted for PDF operations

The PDF backend uses `github.com/pdfcpu/pdfcpu` v0.11.1 (Apache-2.0),
verified from the module's LICENSE. It is pure Go. New transitive modules
are pinned in `go.mod`: `github.com/hhrutter/lzw` v1.0.0 and
`github.com/hhrutter/tiff` v1.0.2 (BSD-3-Clause),
`github.com/hhrutter/pkcs7` v0.2.0, `github.com/pkg/errors` v0.9.1,
`github.com/mattn/go-runewidth` v0.0.19 and
`github.com/clipperhouse/uax29/v2` v2.2.0 (MIT),
`golang.org/x/image` v0.32.0 and `golang.org/x/text` v0.43.0
(BSD-3-Clause), and `gopkg.in/yaml.v2` v2.4.0 (Apache-2.0/MIT).
License texts were checked in the downloaded module cache. These dependencies
introduce no copyleft or commercial-use restrictions.
## Browser and web search (BR-1, BR-2, BR-3)

Pinned versions are unchanged by wiring. License texts were read from each
module cache directory; derived-code licenses and bundled notices are retained
in [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md). This table includes
shared transitive and test dependencies. Playwright 1.62.1 itself is Apache-2.0;
its Microsoft/Puppeteer NOTICE and driver third-party notices are reproduced
there as well. No third-party code is vendored or modified.

| Module | Version | Verified license | Cache files read |
| --- | --- | --- | --- |
| `github.com/JohannesKaufmann/html-to-markdown` | v1.6.0 | MIT | LICENSE |
| `github.com/PuerkitoBio/goquery` | v1.9.2 | BSD-3-Clause | LICENSE |
| `github.com/RadhiFadlillah/whatlanggo` | v0.0.0-20240916001553-aac1f0f737fc | MIT | LICENSE |
| `github.com/andybalholm/cascadia` | v1.3.2 | BSD-2-Clause | LICENSE |
| `github.com/araddon/dateparse` | v0.0.0-20210429162001-6b43995a97de | MIT | LICENSE |
| `github.com/davecgh/go-spew` | v1.1.1 | ISC | LICENSE |
| `github.com/deckarep/golang-set/v2` | v2.8.0 | MIT | LICENSE |
| `github.com/elliotchance/pie/v2` | v2.9.0 | MIT | LICENSE |
| `github.com/forPelevin/gomoji` | v1.2.0 | MIT | LICENSE |
| `github.com/go-quicktest/qt` | v1.101.0 | MIT | LICENSE |
| `github.com/go-shiori/dom` | v0.0.0-20230515143342-73569d674e1c | MIT | LICENSE |
| `github.com/go-shiori/go-readability` | v0.0.0-20241012063810-92284fa8a71f | MIT | LICENSE |
| `github.com/go-stack/stack` | v1.8.1 | MIT | LICENSE.md |
| `github.com/gogs/chardet` | v0.0.0-20211120154057-b7413eaefb8f | MIT | LICENSE |
| `github.com/google/go-cmp` | v0.7.0 | BSD-3-Clause | LICENSE |
| `github.com/hablullah/go-hijri` | v1.0.2 | MIT | LICENSE |
| `github.com/hablullah/go-juliandays` | v1.0.0 | MIT | LICENSE |
| `github.com/jalaali/go-jalaali` | v0.0.0-20210801064154-80525e88d958 | MIT | LICENSE |
| `github.com/kr/pretty` | v0.3.1 | MIT | License |
| `github.com/kr/text` | v0.2.0 | MIT | License |
| `github.com/markusmobius/go-dateparser` | v1.2.3 | BSD-3-Clause | LICENSE, LICENSE_dateparser.txt |
| `github.com/markusmobius/go-domdistiller` | v0.0.0-20240926050704-25b8d046ffb4 | MIT, Apache-2.0 and BSD-3-Clause (derived code) | LICENSE, LICENSE-boilerpipe.txt, LICENSE-domdistiller.txt, NOTICE-boilerpipe.txt |
| `github.com/markusmobius/go-htmldate` | v1.9.1 | Apache-2.0 | LICENSE |
| `github.com/markusmobius/go-trafilatura` | v1.12.2 | Apache-2.0 | LICENSE |
| `github.com/mattn/go-colorable` | v0.1.13 | MIT | LICENSE |
| `github.com/mattn/go-isatty` | v0.0.24 | MIT | LICENSE |
| `github.com/mxschmitt/playwright-go` | v0.6201.1 | MIT | LICENSE |
| `github.com/pmezard/go-difflib` | v1.0.0 | BSD-2-Clause | LICENSE |
| `github.com/rivo/uniseg` | v0.4.7 | MIT | LICENSE.txt |
| `github.com/rogpeppe/go-internal` | v1.14.1 | BSD-3-Clause | LICENSE |
| `github.com/rs/zerolog` | v1.33.0 | MIT | LICENSE |
| `github.com/stretchr/testify` | v1.9.0 | MIT | LICENSE |
| `github.com/tetratelabs/wazero` | v1.8.1 | Apache-2.0 | LICENSE, NOTICE |
| `github.com/wasilibs/go-re2` | v1.7.0 | MIT and BSD-3-Clause (embedded RE2 notices) | LICENSE, NOTICE.txt |
| `github.com/wasilibs/nottinygc` | v0.4.0 | MIT | LICENSE |
| `github.com/wasilibs/wazero-helpers` | v0.0.0-20240620070341-3dff1577cd52 | MIT | LICENSE |
| `github.com/yosssi/gohtml` | v0.0.0-20201013000340-ee4748c638f4 | MIT | LICENSE |
| `golang.org/x/exp` | v0.0.0-20241009180824-f66d83c29e7c | BSD-3-Clause | LICENSE |
| `golang.org/x/net` | v0.60.0 | BSD-3-Clause | LICENSE |
| `golang.org/x/sync` | v0.24.0 | BSD-3-Clause | LICENSE |
| `golang.org/x/sys` | v0.49.0 | BSD-3-Clause | LICENSE |
| `golang.org/x/text` | v0.43.0 | BSD-3-Clause | LICENSE |
| `gopkg.in/check.v1` | v1.0.0-20201130134442-10cb98267c6c | BSD-2-Clause | LICENSE |

The cached go-domdistiller `LICENSE-domdistiller.txt` is empty; its README
identifies the Chromium-derived code as BSD-style and source headers retain
the Chromium authors notice. The upstream BSD text is reproduced in the root
notices with its source. The other cached texts above are reproduced with trailing whitespace normalized.

Additional shared runtime and upstream test dependencies verified in the cache:

| Module | Version | Verified license | Cache files read |
| --- | --- | --- | --- |
| `github.com/coder/websocket` | v1.8.13 | ISC | LICENSE.txt |
| `github.com/sebdah/goldie/v2` | v2.5.3 | MIT | LICENSE |
| `github.com/sergi/go-diff` | v1.3.2-0.20230802210424-5b0b94c5c0d3 | MIT | LICENSE |
| `github.com/yuin/goldmark` | v1.7.1 | MIT | LICENSE |
| `gopkg.in/yaml.v2` | v2.4.0 | MIT and Apache-2.0 | LICENSE, LICENSE.libyaml, NOTICE |
| `gopkg.in/yaml.v3` | v3.0.1 | MIT and Apache-2.0 | LICENSE, NOTICE |
