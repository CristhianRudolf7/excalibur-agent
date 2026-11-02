# Web search backends

`websearch.New(Config)` constructs a `tools.SearchBackend` for assistant and
`delegate` callers. This package does not register tools or change configuration,
contracts, eval profiles, manifests or catalog hashes. The eval profile must keep
its deterministic memory backend. BR-3 wiring owns profile admission and runtime configuration; importing this
package alone enables no network traffic. See
[production wiring](../../docs/development/browser-search.md).

## Configuration and credentials

Configure `Providers` with `Name` (`tavily` or `brave`) and `KeyRef`, and list
provider names in `Order`. An empty order, unknown name or duplicate name fails
construction. Entries in the order must have a provider configuration.
`ResolveKey(ref)` supplies a `KeyResolver`; alternatively set a provider's
`KeyResolver` directly. Resolvers run at request time and must resolve environment
variables or `secret://` references at the caller's authorized execution boundary.
This package never interprets secret references or reads credentials implicitly.
`GitHubKey` optionally supplies a GitHub token; nil means anonymous access.

Set `UserAgent` to identify the operator with a contact URL or email. The default
identifies Excalibur with its repository contact URL and a browser-like prefix.
`HTTPClient` controls provider and source API transport only. Clients are copied,
cookie jars disabled and redirects refused to avoid forwarding credentials.
Use a transport that never logs credentials. Persist Brave responses only when
the operator has explicitly opted in with a plan granting storage rights.
The page reader always uses its own guarded direct transport, with no environment
proxy, custom API transport or cookie jar that could bypass IP validation.

`Limits` defaults to 15 seconds per HTTP request, 2 MiB per API response, 5 MiB
per page, 256 KiB of Markdown and a 2-second maximum `Retry-After` wait.
Positive lower limits are supported. Byte limits and retry waits cannot exceed
these hard maxima (Markdown may be raised to 5 MiB). Markdown limits below the
truncation marker length are raised to that length. Page and source operations
also have a 15-second default total deadline, including rendering or both PubMed
calls. Callers should apply an overall context deadline to a provider chain.
No background goroutines, cache, database, response logs or persistence are used.

## Providers and usage terms

- **Tavily:** bearer-authenticated POST to `/search`, with basic depth and general
  topic. Maps result count, domain restrictions, day/week/month/year freshness
  and language to the [Search API](https://docs.tavily.com/documentation/api-reference/endpoint/search).
  Publication dates are returned when present. Usage is subject to the
  [Tavily Platform Terms](https://www.tavily.com/terms) and the operator's plan.
- **Brave:** token-authenticated GET to `/res/v1/web/search`, with web-only
  results, count, freshness and search language. Domain restrictions use grouped
  `site:` operators. `page_age` is preferred over relative `age` when present.
  The default is **transient use only: no caching, storage, training or
  benchmarking use**. Without storage rights, callers must not persist results
  in tool logs, source records, journals or model traces. `Config.Durable`
  refuses Brave before key resolution or requests unless its
  `ProviderConfig.StorageRights` is true. Refusal falls through to the next
  provider and is explained if the chain is exhausted.
  Production configuration exposes `storage_rights = false` under
  `[search.providers.brave]`, overridden by
  `EXCALIBUR_SEARCH_BRAVE_STORAGE_RIGHTS=1`. Opting in asserts that the owner
  holds a [Brave Search API plan](https://brave.com/search/api/) granting storage
  rights; it does not acquire or verify those rights or permit training or
  benchmarking. Startup logs the enabled state once. Set the switch to `0` and
  restart to restore the safe default.


The `Chain` tries configured providers in order. Missing/unresolvable keys,
transport failures, timeouts, HTTP 429 and 5xx permit fallback. HTTP 429's
`Retry-After` (seconds or HTTP date) is respected before fallback up to the small
configured cap. Other HTTP errors, malformed responses and oversized bodies
stop the chain. Caller cancellation interrupts requests and retry waits.
`ProviderError` contains safe status/classification fields, never raw server,
transport or credential-resolver errors. `AllFailedError` records exhausted
fallback attempts. `SearchWithProvider` returns the answering provider name
separately for transient telemetry; normal search results contain no provider
identity. Benchmark-source URLs are removed through the shared blocklist.

## Guarded page reads

Only HTTP and HTTPS URLs without embedded credentials are accepted. DNS is
resolved in the custom dialer, every returned address must be public, and the
connection dials a validated IP directly, preserving hostname TLS verification.
This avoids DNS rebinding between validation and connection. Loopback, private,
link-local, unspecified, multicast, shared address space, metadata endpoints
(including Azure's wire server) and reserved/documentation/transition ranges
are refused. Every redirect hop is checked, even when a connection is reused;
at most five redirects are allowed. Benchmark URLs are refused initially and
on redirects with the memory backend's benchmark error text.

HTML uses go-trafilatura main-content extraction followed by html-to-markdown.
If extraction returns fewer than 80 text bytes, the full HTML is converted.
Scripts, styles, templates and noscript nodes are stripped. Titles, final URLs,
UTC read times and a `sha256:` hash of the returned Markdown populate the page.
Markdown is bounded at a UTF-8 boundary and ends with `[truncated]` when cut.
Plain text/Markdown pass through; JSON is indented; PDFs and other content types
return an unsupported-content-type error. Oversized fetches are refused.

If converted HTML has fewer than 80 bytes, an optional `Renderer` can provide
HTML. Its output and final URL are checked and bounded again. **The Renderer is
a trusted execution hook:** it must enforce equivalent SSRF and benchmark
policies on all browser navigation, redirects and subresources itself. Checking
its final URL cannot protect requests already made inside another subsystem.
No browser is launched by this package.

## Source adapters

All source results set `SourceID`, enforce the benchmark blocklist and return at
most 20 results (default 10). API responses are bounded and cancellable. These
public endpoints are used without required credentials; upstream availability,
quotas and future access policies remain outside this package's control.

| Source | API / behavior |
| --- | --- |
| wikipedia | MediaWiki `action=query&list=search`, English Wikipedia |
| arxiv | HTTPS export API, Atom entries |
| hackernews | Algolia story search; HN item URL when an external URL is absent |
| github | REST repository search; optional token, anonymous otherwise |
| stackoverflow | Stack Exchange advanced search |
| openalex | Works search; DOI preferred, OpenAlex ID otherwise |
| pubmed | E-utilities JSON esearch followed by esummary, one total deadline |
| semantic-scholar | Graph paper search with title, URL, abstract and publication date |
| huggingface | Models search; model IDs become model page URLs |
| npm | Registry search, package descriptions and npm page links |
| pypi | JSON API **exact package name only**; explicitly stated in each snippet |
| crates | crates.io package search |
| dev | Public DEV article keyword search API (`/api/articles/search`), Forem v1 headers |
| techmeme, digg, youtube | Web chain with a domain/site restriction; subject to provider terms |
| polymarket | Clear “not supported without a provider” error |

DEV uses the [current Forem keyword search API](https://developers.forem.com/api/v1),
which searches titles, tags and article bodies without a required API key. PyPI
has no public search API and does not scrape its search UI. Set a real operator
contact in the User-Agent before deployment and observe each upstream's rate limits.

## Extending and verifying

Implement `Provider.Name` and `Provider.Search`, returning `ProviderError` for
classified failures, then add its constructor and validation in `New`. Keep keys
behind a `KeyResolver`, discard raw error bodies and redact echoed key values.
Do not add result persistence or provider-specific fields to shared contracts.
Add local fixture tests for mapping, bounds, cancellation and secret handling.
Source adapters belong in `sources.go`; add fixtures for both requests and
responses. Reader tests inject a resolver and dialer inside this package to map
a validated public address to local `httptest` servers; no public guard bypass
is available to production callers. Ordinary tests perform no internet calls.

```sh
export PATH="/usr/local/go/bin:$HOME/go/bin:$PATH"
systemd-run --user --scope -p MemoryMax=3G -p MemorySwapMax=0 \
  nice -n 19 go test -p 2 -count=1 -race -timeout=120s ./internal/websearch/...
```

The live test requires both the `live` tag and `EXCALIBUR_LIVE=1`. It makes one
paid Tavily request, reads example.com and searches Wikipedia; it never logs
credentials or response contents. It reads a single `Authorization: Bearer …`
header from `EXCALIBUR_TAVILY_HEADER_FILE` only when explicitly enabled.

```sh
EXCALIBUR_LIVE=1 \
EXCALIBUR_TAVILY_HEADER_FILE="$HOME/.config/excalibur/tavily.header" \
  systemd-run --user --scope --expand-environment=no \
  -p MemoryMax=3G -p MemorySwapMax=0 \
  nice -n 19 go test -p 2 -count=1 -tags live \
  -run '^TestLiveSearchReadWikipedia$' -v -timeout=75s ./internal/websearch/...
```

## Pinned dependencies

No third-party code is vendored or modified. Direct extraction/conversion
libraries and their newly required runtime dependencies are permissively
licensed. All versions are pinned in the root `go.mod` and `go.sum`.

| Module | Version | License |
| --- | --- | --- |
| github.com/markusmobius/go-trafilatura | v1.12.2 | Apache-2.0 |
| github.com/JohannesKaufmann/html-to-markdown | v1.6.0 | MIT |
| github.com/PuerkitoBio/goquery | v1.9.2 | BSD-3-Clause |
| golang.org/x/net | v0.60.0 | BSD-3-Clause |
| github.com/RadhiFadlillah/whatlanggo | v0.0.0-20240916001553-aac1f0f737fc | MIT |
| github.com/andybalholm/cascadia | v1.3.2 | BSD-2-Clause |
| github.com/araddon/dateparse | v0.0.0-20210429162001-6b43995a97de | MIT |
| github.com/elliotchance/pie/v2 | v2.9.0 | MIT |
| github.com/forPelevin/gomoji | v1.2.0 | MIT |
| github.com/go-shiori/dom | v0.0.0-20230515143342-73569d674e1c | MIT |
| github.com/go-shiori/go-readability | v0.0.0-20241012063810-92284fa8a71f | MIT |
| github.com/gogs/chardet | v0.0.0-20211120154057-b7413eaefb8f | MIT |
| github.com/hablullah/go-hijri | v1.0.2 | MIT |
| github.com/hablullah/go-juliandays | v1.0.0 | MIT |
| github.com/jalaali/go-jalaali | v0.0.0-20210801064154-80525e88d958 | MIT |
| github.com/markusmobius/go-dateparser | v1.2.3 | BSD-3-Clause |
| github.com/markusmobius/go-domdistiller | v0.0.0-20240926050704-25b8d046ffb4 | MIT |
| github.com/markusmobius/go-htmldate | v1.9.1 | Apache-2.0 |
| github.com/mattn/go-colorable | v0.1.13 | MIT |
| github.com/rivo/uniseg | v0.4.7 | MIT |
| github.com/rs/zerolog | v1.33.0 | MIT |
| github.com/tetratelabs/wazero | v1.8.1 (selected over v1.7.3) | Apache-2.0 |
| github.com/wasilibs/go-re2 | v1.7.0 | MIT |
| github.com/wasilibs/wazero-helpers | v0.0.0-20240620070341-3dff1577cd52 | MIT |
| github.com/yosssi/gohtml | v0.0.0-20201013000340-ee4748c638f4 | MIT |
| golang.org/x/exp | v0.0.0-20241009180824-f66d83c29e7c | BSD-3-Clause |
| golang.org/x/text | v0.43.0 | BSD-3-Clause |

## Verification record (2026-11-02)

- Full repository race regression passed, split into a 3 GiB scope for all
  packages except engine and a separate 6 GiB engine scope, with swap disabled,
  low priority, `-p 2`, `-count=1` and a 180-second per-package timeout.
- Package race tests passed with more than 91% statement coverage, including
  both providers, all source adapters, SSRF, DNS rebinding, redirect limits,
  benchmark refusals, cancellation, response bounds and credential redaction.
- `go vet ./...`, `GOMAXPROCS=2 staticcheck ./...`, the bare-goroutine check,
  repository formatting and native, Darwin and Windows builds passed.
- The live-tagged check passed Tavily search, example.com reading and Wikipedia
  search with `EXCALIBUR_LIVE=1` and the header file at
  `$HOME/.config/excalibur/tavily.header`. No credentials or response contents
  were printed or recorded.

All implementation and documentation files are new under `internal/websearch`.
Only the root dependency manifests were updated outside this package. BR-3 adds optional production wiring without changing tool contracts. DEV uses the current public
keyword-search endpoint rather than approximating search with a recent feed.
