# Personal agents and secure credential requests

The personal-agent and credential-request contracts compose at the daemon chat
boundary. Each request retains the conversation of its turn. A request without
an origin uses the current favorite. A new card updates that agent's durable
unread count and preview without inserting a message or secret into chat history.
Cross-channel delivery continues to send only the existing secure form link.

Credential deep links resolve a registered conversation to its active agent, so
the header, avatar and chat agree. External channel conversations remain reachable
through their explicit conversation identifier. Agent and credential methods and
events share the generated contract registries. A new agent's nullable last-message
timestamp is represented in the client contract and handled by sidebar ordering.
Avatar rendering uses one normalized shared spec and the complete pixel renderer.

The default embedded tool-name pin includes the deferred `design_avatar` tool,
as recorded by its implementation. Daemon-only personal-agent management and
credential tools do not change the embedded surface or the eval profile. The eval
profile remains `4ecb6a58986660326b7d20d912918d479e7c72140c6e04ab38d83a23315fec28`.
No provider, model gateway or vault cryptography changes are part of integration.

This composes the conversation boundary in Section 5.3, application context in
Section 14.8 and secure form boundary in Section 20.1.
