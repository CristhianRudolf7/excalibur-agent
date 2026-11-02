# INT-2 skills catalog and usage backend

Decisions 0088 and 0089 describe the additive client methods, source precedence,
state mapping, backup locations, production roots and retained-data limits.

The client catalog uses the same index as the daemon's model-facing skill
search/load tools. Content changes rescan immediately. Backups preserve source
content; deletes retain the complete skill tree. Embedded builtins and the eval
catalog remain immutable.

Verification coverage:

| Behavior | Tests |
| --- | --- |
| Groups, localized labels, effective counts, list filters, full content | TestCatalogListGroupsAndContent |
| User/installed/project editing and deletion, installed hash refresh | TestWritableCatalogSources |
| Builtin override, state across rescan, invalid content, backup preservation, restoration, eval fingerprint | TestCatalogManagement |
| References preserved in builtin overrides | TestBuiltinOverrideKeepsReferences |
| Existing malformed override content survives repair in a backup | TestBuiltinUpdateBacksUpInvalidOverride |
| Client error codes, validation diagnostics, builtin deletion refusal, enabled/disabled | TestCatalogMutationsAndErrors |
| SQL rows after reopen, deduplication, tokens/cache/cost, buckets, models, tools, failure spans, durations and bounds | TestUsageFromSeededTelemetry |
| Empty arrays and recent-run bound | TestEmptyUsageAndRecentLimit |
| Durable plus metrics.query aggregation, invalid parameters, cancellation | TestUsageSummaryBackend |
| All seven methods through JSON-RPC transport and real daemon backends | TestCatalogAndUsageGatewayDispatch |
| Both switches off; existing skills.search still works | TestCatalogUsageSwitches |
| JSON API export parity | internal/api package tests |
| Frozen tool/eval surface | TestDaemonToolSurfaceMatchesEmbedded; TestEmbeddedSurfaceAndEvalProfileDoNotMove |

All tests use local files and simulated requests; none call a model provider.
The TypeScript edit only adds wire types and client method names; it adds no UI.

## Verification results

All Go test invocations used a systemd user scope with MemoryMax=3G,
MemorySwapMax=0, nice level 19, `-p 2` and `-count=1`. Race checks passed for
skills, telemetry, API, daemon, gateway and cmd/excalibur. The full daemon suite needed a
30-minute overall budget on the shared host. The two existing command signal
checks passed together on an isolated retry, followed by a passing complete
cmd/excalibur race run. The new catalog/usage tests and
both frozen-surface parity tests also passed together after the final backup fix.

Scoped go vet and staticcheck, repository gofmt and the bare-goroutine check
passed. Linux, Darwin and Windows builds passed again after the final backup fix.

Web typecheck, lint, formatting, RPC contract parity and production build passed.
The default full web test runs hit short deadlines in unchanged UI tests under
host contention. The same 33 files and 206 tests passed with:

```sh
pnpm exec vitest run --pool=vmThreads --maxWorkers=1 --testTimeout=30000
```

No existing test assertions, tool schemas, eval catalog bytes or parity pins were
changed. Usage reports only retained snapshots and recorded tool error spans;
it does not reconstruct missing historical attempts or missing model attribution.
