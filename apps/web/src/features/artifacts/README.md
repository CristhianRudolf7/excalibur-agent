# Conversation diagrams

The shell subscribes to `artifact.created` and `artifact.ready` while a
conversation is active. Events for other sessions are ignored. Creation updates
the home card's count; readiness opens the diagrams view once for each nonempty
turn. General settings can disable automatic opening. The preference, artifact
metadata and handled turn IDs are stored in this browser so replaying events does
not reopen a dismissed turn.

The home card shows the newest artifacts and opens the conversation history.
The history list is virtualized for long conversations. Selecting an artifact
loads its content through `artifacts.get`. Artifacts sharing a group within one
turn appear together, with the earliest artifact as the overview and buttons for
details. New turn results initially show only that turn; “View all diagrams”
returns to the complete history observed by this browser. The event/RPC contract
does not provide a historical listing endpoint.

SVG previews use a strict SVG element/attribute subset and blob-backed images.
The sanitizer removes scripts, foreign objects, event handlers, external
references and unsafe styles. SVG downloads and whiteboard image layers use the
sanitized content. Source viewing and copying retain the original source as
inert text, with its dialect. PNG previews reuse the workspace image viewer;
PDF previews reuse the PDF canvas viewer with blob-backed bytes. Temporary blob
URLs are revoked when previews are replaced or unmounted.

SVGs can also be exported as PNG. Whiteboard import is offered when the existing
whiteboard belongs to the active conversation and the whiteboard feature is
enabled. Import appends an image layer through the existing versioned edit API.
It does not require a new whiteboard RPC.

Unit tests cover event isolation, replay, preferences, progress, grouping,
sanitization, loading failures, source actions, exports, history and image
navigation. `e2e/artifacts.spec.ts` uses mocked JSON-RPC and WebSocket events to
exercise desktop and mobile flows, downloads, preferences and PNG/PDF previews.
