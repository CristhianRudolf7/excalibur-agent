# Classroom agent turns and trusted service boundary

The classroom agent uses `classroom.AgentService`, defined independently of the
persistent store. Its seven methods are the complete agent-facing surface.
`GetCourse` returns `AgentCourse`, including private quiz answers and attempts;
this value must never be returned from the browser-facing `classroom.get` RPC.
The backend service can implement this interface directly or through an adapter.
`AssembleOptions.Classroom` supplies the real service. `classroom.AgentAdapter`
connects its trusted history and write methods to the embedded tool registry.
`AssembleOptions.ClassroomAgent` optionally overrides that adapter in tests.

The `classroom` tool is deferred and fast on the default conversational surface.
It is absent from eval. Adding its name deliberately updates the default surface
pin; the core prefix and eval profile pins remain unchanged. The assistant course
skill uses the new tool. Its previous contents remain under `frozen_eval` to
preserve the existing benchmark catalog fingerprint.

Classroom generation events enter the existing master turn manager through the
additive `ReceiveSystem` entry point. Instructions are appended as system-origin
messages, bypass user-command parsing, and use event identities for receipts.
The daemon consumes `classroom.class_requested`, `classroom.quiz_requested` and
`classroom.attempt_graded`, reading `course_id`, optional `topic_id`, `final` and
`attempt_id` from their payloads. Ready events release coalesced operations. Feedback completion is keyed only
by `attempt_id`, so its ready event may omit course and topic fields.
The target is the existing local-user conversation `cnv_web_excalibur`; no
course-specific conversation is created. Channel adapters should use that same
conversation when presenting the local user's assistant chat.

The subscriber stops with the daemon context and periodically replays from its
cursor to recover dropped live notifications. Duplicate identities are bounded
to 512 entries, pending operations to 128, and pending suppression expires after
ten minutes if generation fails before publishing a ready event. Historical
requests from before subscriber startup are not regenerated. Persisted content
and new UI requests remain the source of work after a restart.

This follows Sections 6.1, 12.4, 13.3, 14.8 and 21.3. It changes no provider,
model gateway, channel protocol or benchmark connector.

`conversation.send` accepts optional `context` with an app discriminator and
bounded course/topic ids. The gateway discovers the additive
`ConversationContextBackend` interface, preserving existing Backend contracts.
The master clones this value into the normalized message, including mixed
control-and-text messages. Text-only adapters retain their current entry points.
For classroom messages the context compiler appends a structured note to the
activity tail with the course/topic titles, status and progress. It never alters
the stable agent profile or prior messages. A later message without context gets
no note, even when earlier history came from the classroom. Unknown future apps
are accepted without a resolver; classroom lookup failures produce a compact
unavailable status, allowing the conversation to continue.
