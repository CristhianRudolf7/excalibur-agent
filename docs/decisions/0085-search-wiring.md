# Search wiring and retention

Section 14.3 uses the pinned `internal/websearch` backend, configured by
`[search]` with Tavily first and Brave second. Credentials are references:
`secret://tavily_api_key` and `secret://brave_api_key` by default, or `env:NAME`.
They resolve at invocation time with tool identity `web_search`, header
destination, run/project scope and delegate restrictions. Literal config keys
are rejected. Resolver and transport errors do not contain credentials; the
backend scrubs credential echoes from results. No new module versions are
introduced by wiring.

The existing SSRF guard applies to HTTP reads and renderer requests. It refuses
nonpublic destinations, DNS rebinding, unsafe schemes, URL credentials and
benchmark sources; redirects are checked at every hop. Browser tools can still
navigate local project sites. Their URL policy is distinct from public reading.

## Brave retention limitation

Brave defaults to transient use: no storage, training or benchmarking. The
daemon and embedded engine persist tool outputs, source records and model
traces. Without storage rights, the production composition's
`websearch.Config.Durable` refuses Brave before resolving its key or sending
a request. The chain continues to the next configured provider; exhaustion
reports the transient-only refusal. Source adapters using the web chain follow
the same rule. The standalone transient adapter remains available.

The owner may explicitly assert that their Brave plan grants storage rights:

```toml
[search.providers.brave]
storage_rights = true # default: false
```

`EXCALIBUR_SEARCH_BRAVE_STORAGE_RIGHTS=1` also enables this setting; `0`
disables it. With the opt-in, durable default/delegate and daemon execution may
use Brave in the configured order, with the same credential handling and
request protections. This setting does not acquire or verify plan rights: the
owner must hold a Brave plan that explicitly permits storing search results.
It does not grant training or benchmarking rights.

Startup logs once when Brave is configured and enabled for durable use:
`Brave enabled for durable use: storage_rights=true (requires a Brave plan with storage rights)`.
Set the field to false (or the environment override to `0`) and restart to
restore refusal before credential resolution.

Eval passes neither production web dependencies nor a browser engine. It keeps
the existing deterministic memory backend, catalog and profile fingerprints.
No benchmarks or live search calls are added to ordinary tests.
