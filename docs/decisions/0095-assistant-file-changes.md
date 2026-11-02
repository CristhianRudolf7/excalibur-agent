# Assistant message file changes

The optional `changes` array extends assistant message events and conversation
history without changing existing methods or interfaces. Each item contains a
path, status, added/removed counts, up to 40 unified hunks, optional new content,
and an optional truncation flag. Messages contain at most 50 files, sorted by
path. Empty new files retain `content: ""`; deleted files omit content.

Attribution reuses the trusted run tree already returned by conversational
turns and used for citation delivery. A master turn receives a fresh run ID;
successful subagent spawn results establish child identities and the
orchestrator supplies their descendants and delegated runs. Merely reading an
unrelated run's status never grants attribution. The engine stamps the executing
run into tool context; shared file tools and revert tools prefer that identity
to the registry's static fallback. Atomic tools detach cancellation with
`context.WithoutCancel`, preserving the executing identity and other context
values while retaining their own timeout. Failed or limited turns retain their run tree
when producing an assistant failure reply. Canceled turns publish no answer.

The daemon now exposes the same workspace history that its engine file tools
use, rather than an unrelated empty history. For each path, the first matching
preimage and last matching postimage define the reply snapshot. Matching is by
trusted run ID, never by time or user/model-supplied text. This isolates
concurrent conversations. A reply reflects writes recorded before its output
boundary; later background writes can appear in their later delivery, not in an
already published reply. Interleaved writes by other runs are not independently
reconstructed or attributed to this turn.

The existing bounded LCS produces line opcodes; hunks carry three context lines.
Large inputs use the existing prefix/suffix replacement fallback, so counts can
include unchanged lines inside that replacement. Hunk output is additionally
bounded to 200 KiB per file, and content above 200 KiB is omitted. Both cuts set
`truncated`. File-list truncation retains the first 50 sorted paths; the contract
has no message-level truncation flag. Final-newline-only edits count as a line
replacement. Project roots produce relative paths, preferring the deepest root;
other paths retain their recorded spelling.

File versions now retain creation/deletion metadata, distinguishing an empty
file from absence. Existing mirror entry points remain available. `apply_patch`
records only successful writes immediately, rather than deferring entries that
could describe failed effects or reverse history order.

The production master currently uses `master.MemStore`: conversation messages
are not persisted across daemon restarts. Changes round-trip through that store
and are copied deeply to protect saved snapshots from event/history consumers.
No database migration or new conversation persistence subsystem is introduced.
