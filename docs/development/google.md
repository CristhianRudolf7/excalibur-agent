# Google account connection

## OAuth agent

The daemon account provider lives in `internal/googleapi`. Its shared
`TokenProvider` contract is unchanged. `ConnectionStatus` adds safe configuration,
reconnect, storage-warning and missing-scope details for Settings.

The authorization-code flow uses S256 PKCE, random single-use state with a
10-minute lifetime, offline consent and the five agreed scopes. Refresh and
account changes are serialized. `invalid_grant` clears credentials and persists
a reconnect reason. Disconnect forgets local credentials even when revocation
fails. No HTTP callback route is added to the daemon.

Tokens use the unlocked daemon vault. A locked or unavailable vault uses an
atomically replaced 0600 `google-connection.json` in the daemon config directory.
The next status request migrates a fallback token into the unlocked vault.
A credential-free import marker prevents re-import after disconnect; a disconnect
tombstone takes precedence over stale vault credentials after a locked-vault
write. The original import file is never modified.

Configuration: `EXCALIBUR_GOOGLE_OAUTH_CLIENT`,
`EXCALIBUR_GOOGLE_REDIRECT_URI`, and `EXCALIBUR_GOOGLE_REFRESH_TTL` (Go duration,
default `168h`, `0` means unknown). The default client and initial import paths
are `~/.config/excalibur/credentials/google-oauth.json` and
`~/.config/excalibur/credentials/google-oauth-token.json`. Restart the daemon
after adding or changing the client configuration. Imported issue dates come
from the local ID token's `iat`, falling back to the token file modification time;
refreshing does not extend the recorded testing lifetime.

Provider tests use only `httptest` servers and fake credentials. They cover
PKCE, consent parameters, state replay/expiry, refresh concurrency, revocation,
invalid grants, restart persistence, one-time import, partial scopes, expiry,
unknown TTL, vault storage, locked fallback and migration.

The four additive `google.*` RPC methods are registered in the shared method
registry, JSON schema definitions, web wire types, gateway and daemon backend.
`Services.Google` and `BackendServices.Google` expose the agreed token provider.
An HTTP gateway test exercises status, start, complete, disconnect and malformed
callbacks, checks account transitions and asserts that access tokens stay private.

Settings includes a Connections section with a Google card, readable permission
chips, reconnect and confirmed disconnect, refresh-expiry countdown and a warning
below two days. Configuration and storage warnings are localized in English and
Spanish. Its dedicated CSS uses existing theme tokens and settings-card classes;
`Settings.tsx` adds only an import, section entry and render branch.

The SPA callback renders through the root route independently of the chat shell,
shows a reduced-motion-aware loading animation, clears callback parameters from
browser history, exchanges the code once even in React Strict Mode, and returns
to Connections with dismissible success or error feedback. Denied access,
malformed callbacks, browser-state mismatch and partial consent are handled.
New-account email comes from Google's verified userinfo response. The provider
uses Google's documented token and revocation endpoints:
[OAuth web-server flow](https://developers.google.com/identity/protocols/oauth2/web-server).

# Google connection

## Agent tools (google/tools)

The daemon chat exposes `gmail`, `drive`, and `docs` as deferred tools in the
`default` profile. Registration happens in `WireAssistantTurns`, after the
embedded engine has been built. The tool names and definitions remain present
while disconnected so connection changes do not invalidate the model tool list.
Every invocation checks the daemon-owned `googleapi.TokenProvider`; a missing
account directs the assistant to Settings → Connections → Connect Google, and an
access-token failure or HTTP 401 directs it to Reconnect Google.

`googleapi.RegisterTools` accepts the existing `tools.Registry` interface,
a `TokenProvider`, and `ToolOptions`. Options inject an HTTP client, four API base
URLs (including the API version prefix), and the daemon blob store. No SDK or
additional dependency is required. Requests use context deadlines, refuse
redirects, bound responses to 25 MiB, and do not retry external writes. Provider
errors, transport errors and Google error bodies are never echoed to the model.

### Actions and arguments

| Tool | Implemented actions | Main arguments |
| --- | --- | --- |
| `gmail` | search, read, draft, send, modify, archive, trash, untrash, delete, labels | `query`, `max_results`, `page_token`, `id`, `thread_id`, `draft_id`, `reply_to_id`, `to`, `cc`, `bcc`, `subject`, `body`, `attachments`, `add_labels`, `remove_labels`, `read`, `starred`, `name` |
| `drive` | search, list, get, download, upload, create_folder, move, rename, copy, share, trash, delete | `query`, `plain_text`, `max_results`, `page_token`, `id`, `folder_id`, `name`, `format`, `digest`, `text`, `mime_type`, `role`, `email`, `anyone` |
| `docs` | read, create, append, replace_text, insert | `id`, `title`, `text`, `find`, `replacement`, `after_heading` |

Gmail search returns metadata summaries and pagination. Read accepts a message
`id` or a `thread_id`, prefers plain text over stripped HTML, and lists attachment
names, MIME types, sizes and attachment IDs. Bodies share a 12,000-character
budget per read. `labels` lists labels unless `name` requests creation.
`draft_id` updates an existing draft; `send` with `draft_id` sends that draft.
To/Cc/Bcc are arrays of mailbox strings. Attachments are objects with a SHA-256
`digest`, `name`, and optional `mime_type`, using an existing daemon blob.
Library documents can be attached once their original blob digest is known.
Replies use `reply_to_id` to obtain the parent Message-ID, References and thread;
conflicting thread IDs and header injection are rejected. Headers use RFC 2047
for UTF-8 subjects and mailbox display names; bodies use quoted-printable and
attachments use MIME base64 with RFC 2231 filenames.

Drive search accepts Drive query syntax or ordinary text, converted to
`fullText contains`; `plain_text:true` forces literal search. List defaults to
`root`. Downloads store bytes in the daemon blob store and return a digest and
openable `/blobs/{digest}?mime=...` URL. Native Google documents default to PDF;
Docs additionally support DOCX, Markdown and plain text, Sheets support XLSX,
and Slides support PPTX and plain text. Invalid source/format pairs fail before
export. Upload accepts either `text` or `digest` plus `name` and optional
`folder_id`. Move reads existing parents and replaces them; share supports
reader/commenter/writer for an email or `anyone:true` with discovery disabled.

Docs read renders named headings as Markdown, including tables and document
tabs, with a 16,000-character budget. Create/append/insert accept plain text or
Markdown source, stored verbatim; Markdown is not converted to rich formatting.
Insert without `after_heading` appends; insertion after a heading requires one
unambiguous match and uses the server's UTF-16 document index and revision guard.
`replace_text` is case-sensitive and replaces every match across the document.

### Confirmation policy

`gmail.send`, `gmail.delete`, `gmail.trash`, `drive.delete`, `drive.trash`,
`drive.share`, and `docs.replace_text` require `confirmed:true`. Otherwise the
tool returns a denied result before acquiring a token or issuing HTTP, explains
the proposed effect and arguments, and instructs the assistant to show the user
recipients, subject, attachments, file IDs, sharing access or exact replacement
and ask for explicit chat approval. `EXCALIBUR_GOOGLE_CONFIRM=0` or the daemon's
explicit `ToolOptions.DisableConfirmation` option disables this gate. Tool
arguments cannot disable the policy.

### Merge notes

The tools branch temporarily declares the shared token-provider contract in
`internal/googleapi/provider_contract_tmp.go`. Delete that file when merging the
OAuth implementation's `provider.go`. The additive `Services.Google` field must
be kept once, assigned by OAuth startup before serving requests. The tools use
that field through a forwarding provider, without owning token persistence,
refresh, OAuth RPCs, callback routes or Settings UI. The only production edits
outside `internal/googleapi` are the services field and chat registration/allowlist.
No embedded/eval tools, core profiles or frozen tool-prefix hashes are changed.

### Verification

HTTP fakes exercise every action, MIME with Spanish accents and emoji,
attachment bytes, reply headers, export formats, blob round trips, query escaping,
confirmation gates and overrides, missing/revoked accounts, and sanitized HTTP
failures. Daemon tests exercise deferred registration, default-only profiles,
stable prefix hashes, provider forwarding and actual conversational tool calls.
The existing `TestEmbeddedSurfaceAndEvalProfileDoNotMove` pins eval and embedded
surface hashes. Full verification results are recorded below after execution.

API references: [Gmail REST](https://developers.google.com/workspace/gmail/api/reference/rest),
[Gmail threading](https://developers.google.com/workspace/gmail/api/guides/threads),
[Drive REST](https://developers.google.com/workspace/drive/api/reference/rest/v3),
[Docs batch requests](https://developers.google.com/workspace/docs/api/reference/rest/v1/documents/request).

# Google connection round

## Settings visual modernization (settingsui)

Baseline browser audit completed at 1440 × 900 and 390 × 844, in light
and dark themes, using fake RPC responses. Screenshots are kept outside
the repository in the settingsui status notes directory. The existing
mobile page did not overflow, but its tabs disabled the active indicator.
The General checkbox appeared above its text, redundant dividers created
excess spacing, and active text had weak contrast in dark mode.

The implementation restores desktop sidebar navigation and adds mobile pill
tabs with an animated indicator in both layouts. Direct links and viewport
changes reveal the selected mobile pill automatically. Keyboard arrows, Home
and End move focus; Enter activates the focused section. General uses
consistent preference rows, dividers, a labeled artifact switch, and
segmented language/theme controls with a sliding thumb. Both themes use
semantic tokens, visible focus rings, readable active text, and reduced
motion support. Profile reserves its loading height; Passwords displays
metadata skeletons. Existing section transitions and Usage/Skills
skeletons remain in place.

The sections array and section routing remain the integration points for
the oauth agent's Connections section. Shared `.settings-card` and
`.settings-connection` classes style account integrations without changes
to GoogleConnect files. Styles live in `features/settings/modern.css` and
use scoped selectors to override the older global redesign sheet.

Verification evidence:

- `gofmt -l .` produced no paths; `git diff --check` passed.
- Web TypeScript project checks, production build, oxlint and repository-wide
  Prettier check passed. Existing lint and bundle-size warnings remain.
- Full Vitest run: 119 files, 631 tests passed. The Settings suite includes
  switch semantics and persistence, keyboard navigation, translated controls,
  and named profile loading skeleton coverage.
- Browser checks cover every section at 1440 × 900 and 390 × 844 in both
  themes, horizontal overflow, animated indicator alignment, active control
  text contrast (at least 4.5:1), reduced motion and stable loading height.
  The final direct-link and resize check is being verified.
- Linux, Darwin and Windows `go build -p 2 ./...` passed. Go vet passed for
  daemon, tools, CLI and skills; `internal/wireengine` race tests passed.
  The remaining daemon/tools/CLI/skills race suite is in progress.

All Go commands use low priority and two package workers. Tests use fake
services; no Google credentials or live Google APIs are involved. There
are no changes to Go sources, contracts, eval/embedded wiring or any other
forbidden paths. Before/after screenshots and command logs remain outside
git in the settingsui status notes directory.
