# Browser and search production wiring (BR-3)

Production daemon startup supplies `AssembleOptions.BrowserWeb`; embedded
non-eval exec uses the same `wireengine.NewBrowserWeb` composition. Nil assembly
options preserve offline fakes. The shared catalog's optional
`EmbeddedOptions.Web` supplies real search dependencies without changing tool
definitions, descriptions, schemas or prefix ordering. Browser operations join
the deferred catalog through the unchanged capability manifest.

## Configuration

The layered loader applies defaults, TOML files, environment and explicit flag
overrides in the existing order. `config.get` and `config.list` expose both new
sections. A fresh install relies on these defaults; `config.WriteFile` writes
all sections, and reinstall preserves existing user configuration.

```toml
[browser]
enabled = true
headless = true
sandbox = true
channel = "chrome"
idle_timeout = "10m"
max_tabs = 8
# Empty/omitted paths become <daemon-data>/browser/{profile,downloads,routines}.
# profile_dir = "/private/path/profile"
# downloads_dir = "/private/path/downloads"
# routines_dir = "/private/path/routines"

[search]
order = ["tavily", "brave"]
user_agent = "Mozilla/5.0 (compatible; Excalibur/1.0; https://github.com/CristhianRudolf7/excalibur-agent)"
[search.providers.tavily]
key_ref = "secret://tavily_api_key"
[search.providers.brave]
key_ref = "secret://brave_api_key"
storage_rights = false
```

Key references may instead be `env:NAME`. Values never belong in TOML, model
arguments or logs. References resolve only at execution. Form secrets use the
vault's domain-bound form policy with `browser_act`; search uses `web_search`
and header policy. Delegates cannot resolve another project's secrets.

`EXCALIBUR_BROWSER_ENABLED=0` keeps the fake with a warning;
`EXCALIBUR_BROWSER_SANDBOX=0` explicitly disables sandboxing with a warning;
`EXCALIBUR_SEARCH_ORDER=tavily,brave` replaces provider order. Generic double
underscore environment overrides also work for section fields; durations
accept Go duration strings. Unknown/duplicate providers, empty orders, literal
keys, nonpositive idle/tab limits and unsupported channels are rejected.

`excalibur install` installs only the Playwright driver, using system Chrome.
It prints `browser: ready`, a missing Chrome note or a driver failure note;
browser problems never fail installation. The codebase has no doctor command.
There is no web UI, live view or takeover. Browser blocks tell the model to
notify the user and continue with other sources or reading/search tools.

## Retention and public reading

Tavily and Brave follow the configured provider order. By default, durable
execution refuses Brave before key resolution or requests because its
transient-only policy excludes storing results in journals, source records or
traces. The chain continues to the next provider, and exhaustion explains the
refusal. The standalone transient backend remains available.

Set `storage_rights = true` under `[search.providers.brave]`, or
`EXCALIBUR_SEARCH_BRAVE_STORAGE_RIGHTS=1`, to permit durable Brave use when the
owner holds a Brave plan explicitly granting storage rights. The default is
false; `0` restores refusal. Restart the daemon/run after changing it. This is
an assertion of the owner's existing plan rights, not their acquisition or
verification; it does not permit training or benchmarking. Startup logs once:
`Brave enabled for durable use: storage_rights=true (requires a Brave plan with storage rights)`.
Eval still never acquires real search, even with this opt-in. See decision 0085.

Public reads preserve the SSRF/benchmark guards, including on all HTTP renderer
resources. Rendering uses an ephemeral context in the engine's Chrome process,
without persistent profile credentials, service workers or WebSockets. Local
projects remain accessible through browser tools. `web_read` refuses loopback,
even in the daemon integration test; no test-only production bypass exists.

## Backend verification

- Config tests: defaults, layered TOML, serialization round trip, duration and
  environment overrides, flag precedence, invalid settings and sandbox warning.
- Wiring tests: nil web dependencies preserve memory results, supplied backend
  is invoked, availability swaps/retains the capability, private directories,
  disabled browser avoids probing, runtime key resolution and vault policies.
- Adapter tests: all four browser blocks tell the model about human assistance,
  no takeover and alternatives, without changing tool definitions.
- Installer tests: success, missing Chrome and failed driver installation are
  reported without failing installation.
- `browser` + `EXCALIBUR_BROWSER_TESTS=1`: real daemon starts on temporary state,
  uses its executor for sessions, navigation, readiness, captures and form
  actions, then restarts and verifies persisted cookies. A local public-reader
  call verifies SSRF refusal. A separate renderer test verifies JavaScript.
- `live` + `EXCALIBUR_LIVE=1`: daemon executor Tavily search takes its key from
  `EXCALIBUR_TAVILY_HEADER_FILE`; skipped otherwise, no response contents logged.

The final verification record follows below. All Go tests
run at low priority with `-p 2`, MemorySwapMax=0 and a 3 GiB cap (6 GiB for the
repository-wide race regression). No benchmark is run.

## Shared-file changes

| Existing shared file | Reason |
| --- | --- |
| `internal/config/config.go` | Append browser/search sections and their defaults. |
| `internal/config/loader.go` | Apply named environment aliases and parse browser idle durations. |
| `internal/config/validate.go` | Invoke the new browser/search validation. |
| `internal/wireengine/catalog.go` | Pass optional `Web` dependencies while keeping nil behavior. |
| `internal/cli/exec_engine.go` | Admit production collaborators outside eval and close them after a run. |
| `internal/daemon/services.go` | Assemble optional collaborators and swap the available browser capability. |
| `internal/daemon/engine_wire.go` | Pass real web dependencies and attach the browser as deferred tools. |
| `internal/daemon/daemon.go` | Close the persistent browser on shutdown. |
| `internal/daemon/config.go` | Expose the two new section names. |
| `internal/daemon/install.go` | Add the injectable nonfatal browser step and report field. |
| `cmd/excalibur/daemon.go` | Supply production configuration, data directory and durable blobs. |
| `cmd/excalibur/install.go` | Invoke driver installation and print its report. |
| `internal/tools/capability_adapter.go` | Explain browser blocks without takeover and pace working-task polling. |

Changes within the merged implementation packages additionally add guarded
rendering, system Chromium channel selection and a durable Brave refusal.
The browser/search READMEs describe their production wiring; the root README
credits Playwright. Dependencies, third-party notices, decisions 0084/0085,
capability documentation and the agent-quality switches record the behavior.
No eval, provider, models gateway, API, contracts, integration, benchmark command
or dependency manifest file was changed.

## Verification record (2026-11-02)

- Final full regression **passed**: `GOMAXPROCS=2`,
  `systemd-run --user --scope -p MemoryMax=6G -p MemorySwapMax=0 nice -n 19
  go test -p 2 -count=1 -race -timeout 10m ./...`. This includes CLI, daemon,
  engine, memory, store, tools, browser, websearch, wireengine and all suites.
- Focused race tests **passed** under a 3 GiB cap for config, wireengine,
  daemon, tools, websearch and browser. The vault tests verify the resolving
  tool identity, form domain, delegate project and execution-time key lookup.
- The complete existing local-Chrome integration suite plus the new renderer
  and daemon persistence tests **passed** with `EXCALIBUR_BROWSER_TESTS=1`,
  `-tags browser -race -p 2 -count=1 -timeout 15m`, the same 3 GiB cap and
  sandbox enabled: browser **85.727s**, daemon **48.138s**. Cookie observation
  resets before restart, and capture references are checked in the daemon DB's
  blob store. The reader refuses the local fixture under the production SSRF
  policy. No real search API is used by this suite.
- The named daemon/embedded parity tests, prefix checks, API parity and eval
  production-admission test **passed**, unchanged. Pinned hashes remain:
  names `b9bedd828535af82488a5a7034279a376a3165377cdac3c87adddf7d7b4408eb`,
  prefix `95b1a140c09eb43f24691ca087d359a7630592d9c147ae2fb9b84e00fc06adae`,
  eval profile `6bfd22cc417b7ddefb81d2193c181dcc2406d5f951ab6a90f50d1db1f9f4e025`.
- Repository `go vet -p 2 ./...` and `GOMAXPROCS=2 staticcheck ./...`
  **passed**, as did vet/staticcheck with `browser,live` tags on the browser
  and daemon packages. Native, Darwin and Windows `go build -p 2 ./...`
  **passed**. Commands used low priority and GOMAXPROCS=2.
- `gofmt -l .` printed nothing; the bare-goroutine check and `git diff --check`
  **passed**. The pinned dependency manifests and protected contract/profile
  files have no diff.
- The live daemon Tavily test compiled and correctly **skipped** with
  `EXCALIBUR_LIVE=0`. This change did not run a paid search or benchmark.

### Earlier failures and remaining diagnostic

An earlier global run during host load above 30 failed the existing
`TestEvalTimeOnlySurvivesPast25Steps` deadline and `TestPowerCutBlobInvariant`
startup timing. Both were repeated in capped, isolated runs. The store test
passed (4.434s); the first isolated CLI retry still hit its 15-second context.
A read-only Go overlay of the pre-wiring code passed that CLI test in 13.481s
as load fell. The final unmodified repository-wide regression subsequently
passed; no timing assertions, eval pins or production deadlines were weakened.

That earlier global run also detected a real data race between the callback
and returned warnings slice in unchanged `internal/memory/review.go:625,652`,
during `TestServicesStartAndStopCleanly`. The isolated capped race rerun passed
(5.569s), and the final full regression did not reproduce it. This is recorded
as an existing memory cancellation diagnostic outside BR-3; passing reruns do
not establish that the race is fixed. No memory or store code was changed.

## BR-3 review round 1 verification

The storage-rights opt-in defaults to false. Tests cover TOML opt-in with the
key reference omitted, environment enable/disable and invalid values, NoEnv,
flag precedence, Brave requests against an `httptest` server when enabled,
default refusal without resolving credentials, fallback to the next provider,
the exact startup log once, and eval ignoring an enabled opt-in.
Provider field defaults are merged before TOML decoding so setting only
`storage_rights` preserves the default key reference.

- Capped race tests passed for config, websearch, wireengine, CLI, daemon,
  tools and API, including the daemon/embedded parity and frozen eval tests:
  `GOMAXPROCS=2 systemd-run --user --scope -p MemoryMax=3G
  -p MemorySwapMax=0 nice -n 19 go test -p 2 -count=1 -race -timeout 10m
  ./internal/config ./internal/websearch ./internal/wireengine ./internal/cli
  ./internal/daemon ./internal/tools ./internal/api`.
- Repository `go vet -p 2 ./...`, `GOMAXPROCS=2 staticcheck ./...` and native,
  Darwin and Windows `go build -p 2 ./...` passed at low priority with
  GOMAXPROCS=2. Formatting, the bare-goroutine check and diff whitespace check
  passed. No paid API or benchmark was called.
- Eval, prefix and parity pins remain unchanged. Protected contracts,
  provider/model code and dependency manifests remain untouched.
