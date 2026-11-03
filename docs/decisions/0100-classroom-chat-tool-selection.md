# Classroom tool selection in daemon chat

Classroom app context previously changed only the current turn's prompt note.
The master selected deferred tools from the message text, so a contextual reply
without study keywords could offer no classroom tool. A delegated run also lost
the app envelope and selected tools from its delegation goal alone. The daemon
already supplied the shared classroom service to both registries.

Daemon chat now combines normal intent activation with explicit application
selection. A classroom envelope offers `classroom`, `whiteboard` when enabled,
and the `course-creation` skill through `load_skill`. Study intent without an
envelope selects the same teaching tools. Selection is recorded in the turn's
activation journal; it does not mutate the deferred catalog or stable prefix.

Classroom request events supply an app envelope containing their course and
topic identifiers. `Master.ReceiveSystem` additively reads, validates and saves
the app context from its caller's Go context in the normalized system message,
so queued system turns use the same selection as user turns. Its signature and
receipt semantics are unchanged. Delegated workers inherit the originating
message's envelope, including for nested delegation, and append the existing
bounded app note to their own activity. Explicit delegation tool grants remain
authoritative and cannot be expanded by app context.

An embedded classroom tool without its owning service explains that classroom
requires `excalibur daemon start`. Tool definitions and the eval profile remain
unchanged, including eval hash
`4ecb6a58986660326b7d20d912918d479e7c72140c6e04ab38d83a23315fec28`.

This implements the chat and application-context behavior of Sections 6.2,
12.4, 13.3, 14.8 and 15.9 without changing providers or the model gateway.
