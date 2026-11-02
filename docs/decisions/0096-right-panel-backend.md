# Right panel backend contracts

The additive `activity.list` and `files.open` methods use the existing gateway
passthrough seam. No core interface or eval profile changes are needed.
`activity.updated` uses the normal bus, replay cursor and WebSocket transport;
the web default subscription includes the event.

Activity belongs to a conversation only after a successful tool spawn, automation
creation or manual automation launch establishes a server-observed identity.
Reading another run's status does not grant ownership. Subagent descendants are
expanded from the orchestrator agent hierarchy, using the same trusted spawn
roots as assistant file changes. The shared master agent is never a root for
this expansion. Delegated task service records enrich matching runs with task
IDs and requested titles. Automation runs are read from the automation service;
created automations retain their originating conversation for future runs.
Unattributed CLI tasks and automations are not assigned to an arbitrary chat.
Attribution is in memory, consistent with the current conversation store
(decision 0095); a daemon restart does not reconstruct lost conversation links.

The activity list contains active items followed by items finished within the
last 30 minutes, ordered by most recent update with an ID tie-breaker, capped at
50. Engine-only step and plan changes require sampling live status in addition
to orchestrator state. A daemon-scoped `safego` observer samples every 100 ms and
coalesces changes to at most one event per item per 500 ms. Intermediate changes
inside a throttle window are represented by the latest snapshot, including a
terminal snapshot. The list reads current state independently of event delivery.
The event observer covers all matching items even when the list reaches its cap.

`files.open` resolves relative paths against a specified registered project's
roots, or all registered roots and the trusted workspace roots when no project
is specified. Paths and roots are canonicalized through symlinks and containment
is checked using path components and enforced with `os.OpenInRoot` while
opening the descriptor. Special files are rejected before opening. The production workspace root is the daemon's
working directory, matching relative file-tool execution. Other deployments can
supply context-dependent workspace roots without accepting roots from clients.
Files outside those roots return `outside workspace`. Only regular files of at
most 64 MiB are read; UTF-8 text without early NUL bytes is returned inline up to
2 MiB. Larger text uses the binary preview response.

Images are written to the existing content-addressed blob store and return its
`/blobs/<digest>` URL. Documents and sheets are submitted through the same
scope-enforcing library route as `library.ingest`, so digest deduplication and
library authorization remain shared. Opening waits for ready/partial extraction
with request cancellation and a two-minute bound; failed or canceled extraction
returns an error. The original blob URL is also returned for PDF/image viewers.
CSV and TSV expose anchored cell blocks; TSV uses its own delimiter in the same
bounded text reader. Modern Office files use the existing native readers.
Legacy XLS/DOC/PPT files convert to OOXML through an installed LibreOffice
executable, in a private temporary directory with an isolated profile and a
one-minute bound, then use those native readers. Missing LibreOffice is an
explicit unsupported-extraction error; the original file is still retained.
Non-Linux legacy conversion returns `errors.ErrUnsupported`.

`FileChange.binary` is optional. Either preimage or postimage containing invalid
UTF-8, or a NUL byte within its first 8 KiB, produces a binary change with zero
line counts, an empty hunk array and no inline content. Text snapshots keep the
existing bounds and net-diff behavior.
