# 0091: Attach run-scoped web evidence to assistant messages

The engine attaches a trusted source-run context at tool execution. Existing
`tools.SourceRecorder` events now include that run ID; deferred dispatch retains
the context. The daemon owns the recorder shared by its web tools and attaches
only evidence belonging to the trusted delivered run tree. Blocked events,
unattributed events, other runs, invalid URLs and unsupported tools are excluded.

The existing assistant-output pipeline validates library citations first. It then
adds deduplicated web sources from `web_search`, `web_read` and `source_search`.
`SourceRef` already has `kind`; this change adds optional `domain`, `read_at` and
`tool` fields without changing existing document citation fields or numbering.
Both `message.created` and `conversation.history` use the same persisted sources.

Source identity contains URL, title, domain, timestamp and tool only. Neither
search snippets nor page bodies enter the message's sources. Search results carry
private provider metadata (`json:"-"`) so Brave results are not recorded by
search tools. A subsequent successful `web_read` records the page's identity.
Source adapters using the provider chain inherit this retention metadata.
Decision 0085's existing durable-provider storage-rights gate remains intact.

The existing source recorder remains an in-process execution-evidence buffer;
final sources use the existing message persistence seam. No new model tool schema,
provider adapter, gateway model protocol or evaluation pin is changed. The engine
change is a single additive context binding needed to prevent cross-run leakage.

Tests exercise a real engine tool call with a fake web backend, run isolation,
blocked URLs, library citations alongside web sources, published events, RPC
history, source-search attribution and Brave snippet/unread-result exclusion.

## Integration merge

The BE-1/BE-2/BE-4 merge combines run-scoped sources and voice with persistent
reminders, task queries, catalog management, usage and whiteboard services.
The source context adds no model tools. The merged catalog retains decision
0093's `cb7e3eb40a79a35320c2bd4bf3ba0c94aa875e1c57d8d252490190187dec3dbd`
names pin; no additional names, prefix or eval-profile pin adjustment is needed.
