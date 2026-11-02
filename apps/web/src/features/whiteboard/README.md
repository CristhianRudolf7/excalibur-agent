# Conversation whiteboard

`EXCALIBUR_WEB_WHITEBOARD=1` enables the feature by default. Set it to `0`
at build time and rebuild to remove its tab and event connection. No model
profiles, tool catalog, backend methods or parity pins are changed here.

The shell mounts the event connection independently of the chat or voice view.
The existing typed RPC transport receives additive Whiteboard methods through
`contract.ts`; `types.ts` mirrors the INT-2 Whiteboard wire contract. The backend
methods are being implemented separately. Until they are available, the board
list shows an offline state and a retry button, without invented scene data.
The exported-backend `clientMethods` parity list stays unchanged until the
backend contract is merged.

Excalidraw and its CSS load only when a real board opens. Mermaid conversion
loads separately when a diagram appears. Native primitives retain their wire
metadata in `customData` so math, code, sticky notes, provenance, bindings and
styles can round trip. Mermaid diagrams become grouped native shapes. Their
source is retained as one contract element. Math uses KaTeX MathML inside SVG;
code uses lowlight highlighting inside SVG. The adapter itself has no runtime
Excalidraw dependency, so its conversion tests run independently of the canvas.

A version buffer serializes operations, ignores duplicates, buffers reordered
versions and fetches an authoritative scene on gaps. Animated operations reveal
text, strokes and shapes progressively; reduced motion applies them instantly.
User edits are debounced for 350 ms. Each in-flight batch keeps its operation ID
across retries; conflicts fetch the latest version and replay local operations
per element. New edits queue behind the in-flight batch. Failed writes expose a
retry action and retain pending operations across panel changes and fullscreen remounts.

Fullscreen is portaled to `document.body` at z-index 1000. A right panel hosting
a board also uses z-index 1000, reserving this layer above voice-call overlays.
FE-A's call overlay must remain below 1000. `useWhiteboard` exposes `open`,
`layout` and `board` for any further voice layout coordination. The voice session
need not stop or remount when an opened event switches the selected panel.

The gateway currently accepts content-addressed `PUT /blobs/{sha256}` (and POST
to the same path), rather than an unaddressed `POST /blobs`. Snapshots therefore
reuse the existing hashing/upload helper and send the digest as `image_blob`.
`request_id` is included in `whiteboard.snapshot` to correlate the event, as
required by the contract's correlation note. The method's example parameter
list omits this field; the backend must accept this additive field.

Verification: `pnpm check`, `pnpm build` and
`EXCALIBUR_E2E_PORT=4193 pnpm exec playwright test e2e/whiteboard.spec.ts`.
The optional port avoids collisions between simultaneous worktree previews.
The e2e tests mock RPC, event delivery and blob uploads; they cover agent opening,
animated elements, highlighting, viewport focus, native rectangle drawing and
apply payloads, PNG snapshot correlation, fullscreen and 390 px layout.

On a heavily loaded shared machine, the optional verification settings
`EXCALIBUR_TEST_WORKERS=2 EXCALIBUR_TEST_TIMEOUT=30000 pnpm check`
limit unit-test concurrency and allow more scheduling time. Default test settings
are unchanged when these variables are absent.
