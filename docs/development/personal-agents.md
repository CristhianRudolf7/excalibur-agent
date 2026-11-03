# Personal agents

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
`design_avatar` validates model-selected sprite fields and returns a normalized
spec and preview. Registry writes use the same validator, including enum, RGB
and contrast handling. The web renders the full palette and accessories with
`AgentAvatar`. Avatar design remains deferred and outside eval.

Backend regression tests exercise migration with existing history, registry
lifecycle and favorite selection, persisted unread counts, per-agent prompt and
history isolation with shared user preferences, RPC lifecycle events, classroom
origins and bus-sequence restarts, system routing, relay delivery and retry behavior, loop prevention,
search bounds and deferred tool activation. Tests use simulated model providers
and never call a paid API.

Secure credential cards retain their requesting conversation, increment its
agent's unread count and use a generic preview without storing a chat message.
An absent origin resolves the current favorite. Credential links to a registered
conversation select that agent's header and chat; explicit external channel
conversations remain supported. Shared web event types cover both registries,
and sidebar ordering accepts null message timestamps for agents with no history.

`TestPersonalAgentsAvatarRelayAndFavoriteEndToEnd` drives public daemon RPC with
a simulated model. The favorite designs an avatar, creates Pablito and relays
one message. The test verifies the listed avatar, durable user-role relay
metadata, exactly one target turn, unread increments and favorite handover on
archive. Credential-origin tests cover both an explicit agent and favorite
fallback; web tests cover card ownership, deep links and unread badges.

Integration verification passed the full Go race suite with two package workers,
low process priority, a 4 GiB memory cap and swap disabled. No Go timing retries
were needed. Go vet, staticcheck, formatting, the bare-goroutine check and Linux,
Darwin and Windows builds passed. Agent and credential contract regeneration is
idempotent, and the embedded/eval pin checks passed.

The tagged real-Chrome suites passed, including deterministic-model secure login
through daemon chat. Web verification passed frozen-lockfile installation,
typecheck, lint, formatting, build and all 566 unit tests in 109 files. The 37
agent, credential, classroom and Robot Lab Playwright cases passed across the
combined run and one isolated retry: the initial Robot Lab ballplate case timed
out while loading its starting 3D scene, then passed unchanged. No test deadlines
were relaxed. Existing lint and bundle-size warnings remain.

Python Robot Lab tests were not run because pytest and the physics dependencies
were unavailable in the checked environments. Successful live-model credential
dashboard reporting and remembered reuse remain unverified as documented in
`secure-credentials.md`; integration used simulated inference and made no paid
API calls.
