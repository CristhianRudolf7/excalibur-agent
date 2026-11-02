# 0080: Run daemon conversation answers through the shared engine

The daemon constructed its master without a model caller. Intake accepted
messages, but conversation turns produced no assistant answer. Wire a
conversation-aware caller after daemon services and the configured models
gateway exist (Sections 5.3, 6.2, 7.1 and 13.2).

Each message uses `internal/engine` with the master profile, interlocutor role,
interactive model lane, existing executor and journal, a 12-step default and
a 120-second deadline. These task-specific defaults supersede the older
eight-step role default for this production caller only. Engine instances own
their per-turn context and response capture; the engine algorithm remains
shared with all other runs. Conversational answers use text-only completion
and do not run the autonomous artifact verification hooks.

The master gets a restricted direct tool list. Library browse operations are
added to the role profile, while generic dispatchers are omitted from this
production caller so they cannot expand into slow tools. The daemon and
embedded catalogs, eval tools, skills and profile hashes remain unchanged.
Existing delegation remains available; this change does not add a background
driver for recorded subagent runs.

The harness supplies the retrieval run id, then delivers final text through
`Master.SendAssistant` with trusted citation evidence. Provider failure and
unconfigured operation produce assistant text and notices. Turns drain pending
messages without dropping them, cancel without delaying intake, and join on
daemon shutdown.

Additive master seams are `TurnCaller`, `SetTurnCaller`, `TurnHistory`,
`ReceiveWithAttachments` and `TurnManager.Stop`. Daemon exposes
`WireAssistantTurns` and `ConversationSendAttachments`. Structured attachment
transport still needs a gateway-owned change: its existing backend interface
passes text only. Until then the existing textual attachment lines work.
No API, model protocol, provider or extraction contract changes are required.
