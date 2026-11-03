# Personal agents

The shell loads the personal agent registry through the typed RPC client and
subscribes to agent lifecycle and unread events over the existing resumable
WebSocket transport. Lifecycle events refresh the registry; unread events update
previews, timestamps and badges directly. The initial fallback preserves access
to the installation's existing conversation if the registry is unavailable.
Initial content waits for that registry request, so a deep link cannot load the
fallback identity's history while its actual conversation ID is still unknown.
Agent selection and creation stay disabled during this initial request.

The favorite opens by default. An explicit selection carries across the chat,
classroom, robot and robot lab headers. The shared panel header also exposes the
selector for spreadsheets, documents, diagrams and whiteboards. Archived agents
are excluded from selectors. If a selected agent is archived, the selection falls
back to the current favorite.

History, incoming messages, sends and voice turns use the selected agent's
server-provided `conversation_id`. The default identity therefore retains
`cnv_web_excalibur` even when its agent ID changes. Relayed messages retain user
styling and show the sending agent's name, including archived senders.

Creation and avatar requests open the favorite's chat with a localized composer
prefill and keyboard focus. They require the user to send the message. Settings
apply confirmed RPC results, keep failed edits available for retry, confirm
archiving, explain favorite hand-over and prevent archiving the last active
agent. Shared user resources remain accessible through the existing interfaces.

`types.ts` follows the Go registry RPC and event contracts. `AgentAvatar`
renders normalized palettes and accessories through the shared pixel sprite
renderer. Agent persistence, tools and avatar design run in the daemon.

Unit coverage exercises ordering, badges, prefill isolation, keyboard switching,
settings mutations and errors, event refreshes, conversation isolation, relay
metadata and concurrent unread acknowledgements. Playwright mocks `/rpc` and
`/ws` to exercise the same flows in English and Spanish, including the favorite
landing route and the narrow-screen avatar selector. Run `pnpm check`,
`pnpm build` and `pnpm test:e2e` from `apps/web`.
