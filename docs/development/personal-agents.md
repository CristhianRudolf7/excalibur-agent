# Personal agents backend

The daemon stores personal agents separately from orchestrator execution
identities. Migration 11 preserves existing conversation and message rows. On
first boot the registry creates one favorite owning `cnv_web_excalibur`, using
`EXCALIBUR_ASSISTANT_NAME` when configured and `Excalibur` otherwise. The existing
configuration has no structured assistant-name setting to migrate. Subsequent
boots retain the registry name. New agents own `cnv_agent_<id>` conversations.

Each chat turn selects its persona from the conversation's registered agent and
places its name, title, description and personality in the stable profile layer.
Updates to other agents and new chat activity do not change that layer. User
identity documents, memory, library and other services remain shared. Recent
history is loaded only from the selected conversation.

`agents.list`, `get`, `create`, `update`, `archive`, `restore`, `set_favorite` and
`mark_read` are available through JSON-RPC. Archive preserves history and rejects
new user sends. Archiving the favorite chooses the most recently used active
agent, with creation order as the fallback; the last active agent cannot be
archived. Registry lifecycle and unread events are exported in the shared
contracts.

Messages, receipts and unread metadata commit in one porter transaction.
Assistant output and relayed user messages increment unread; ordinary user
messages update recency without incrementing it. Mark-read resets the count.
History retains message timestamps and optional `relayed_by` metadata after a
restart. The conversation store also reads pre-migration message rows.

Classroom requests can carry `conversation_id` in open-topic, request-quiz and
submit parameters. The daemon associates emitted request events with that chat;
failed or cached requests leave no stale origin. Classroom receipt keys include
the event timestamp because bus sequence numbers restart with the daemon. Requests without an origin,
automation notifications and reminder output resolve the favorite at delivery
time. Existing chat delegations retain their originating conversation.

Three fast deferred tools are registered on the daemon chat catalog:

- `manage_agents` exposes registry lifecycle actions.
- `send_to_agent` delivers one user message to a different active agent and
  starts its normal chat turn. Keyed receipts deduplicate delivery, including
  after restart. The sender receives a delivery result. Relayed turns hide the
  relay tool and reject forced invocations.
- `search_agent_chats` returns at most 20 snippets, each bounded to 600
  characters, with agent identity, message identity and UTC timestamp. It searches
  at most 200 recent messages per agent and 50 agents, and reports truncation.
  Archived history remains searchable.

These tools have English and Spanish intent keywords. They remain outside the
frozen eval profile and the embedded catalog. The eval hash remains
`4ecb6a58986660326b7d20d912918d479e7c72140c6e04ab38d83a23315fec28`.
`design_avatar` is supplied by the separate avatar implementation. Registry
writes call `agents.ValidateAvatar`; the temporary implementation validates the
shared sprite enums and RGB colors.

Backend regression tests exercise migration with existing history, registry
lifecycle and favorite selection, persisted unread counts, per-agent prompt and
history isolation with shared user preferences, RPC lifecycle events, classroom
origins and bus-sequence restarts, system routing, relay delivery and retry behavior, loop prevention,
search bounds and deferred tool activation. Tests use simulated model providers
and never call a paid API.
