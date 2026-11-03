# Personal agent conversations

Personal agents are distinct from orchestrator execution identities. Each owns
one conversation and a persona; all retain access to the same user memory and
services. This extends the identity and context rules in Sections 10.2 and 11.1.

Migration 11 adds a personal agent registry and an optional normalized message
payload. It preserves existing message rows and conversation identifiers. The
initial favorite owns `cnv_web_excalibur`; additional agents receive a new
conversation. Archive retains both identity and history. Favorite reassignment
uses latest message activity, then creation order. The registry serializes
changes and publishes events only after the porter commits.

Avatar fields use the shared enum and RGB color contract. The temporary
validator in `avatar_types.go` will be replaced when the separate avatar tool
is integrated; callers depend only on `ValidateAvatar`.

Validation: registry tests cover upgrading a database with existing history,
restart persistence, creation and updates, archive and restore, favorite
selection, validation, and concurrent unread updates.

The daemon conversation store now persists normalized message metadata alongside
legacy message columns. The optional atomic receipt interface leaves the shared
`ConversationStore` contract intact while allowing deferred intake, system turns
and relays to commit receipt and message together with unread metadata. Public
history and message events add optional relay metadata and timestamps.

The conversational master resolves personas by conversation; its profile excludes
the legacy shared agent identity document when a personal identity is present.
The user identity document remains shared. System outputs resolve the favorite
at delivery time unless an active originating conversation is known. Classroom
RPC origins are associated with emitted event identities in the daemon.

Registry RPCs and events are additive API exports. Personal-agent tools register
as deferred daemon tools only, preserving the embedded surface pins and the
frozen eval profile. Search results carry agent and time citations with explicit
bounds. Relay turns hide and deny further relay calls.
