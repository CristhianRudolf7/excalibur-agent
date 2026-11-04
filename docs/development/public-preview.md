# Public previews from chat

The daemon exposes `public_preview` as a deferred tool in profile `default`.
It is available to conversational turns, including requests for a public page
that can be opened from a phone. It is not registered in embedded or eval
harnesses and does not change their tool lists or cached core prefixes.

## Publishing

For a small static site, the assistant supplies files directly:

```json
{
  "action": "publish",
  "files": {
    "index.html": "<!doctype html><html><head><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"></head><body><h1>Hello</h1></body></html>",
    "style.css": "body { font-family: system-ui; }"
  },
  "ttl_seconds": 7200
}
```

Files must use relative paths without traversal, with at most 256 files and
2 MiB of combined path/content bytes. They are written into a fresh private
`previews/site-*` directory under the daemon data directory. The directory
is removed when the preview stops, including failed startup.

For an existing static project, use `{"action":"publish","path":"/absolute/project/dist"}`.
The path must be a directory inside the daemon's `previews` or `workspaces`
directories, a registered project root, or a trusted workspace root supplied
by the daemon. Relative paths resolve against those roots. Arbitrary local
directories are rejected. Build projects before publishing their static output;
this tool does not run builds or application commands. Escaping symlinks and
special files are rejected. File serving also enforces containment at open time,
so adding an escaping symlink after publication does not expose its target.

To expose an already running HTTP app, use
`{"action":"publish","port":5305,"confirmed":true}` only after the user
confirms sharing that local service. The origin is always `127.0.0.1`; no host
or arbitrary origin URL can be supplied. The app remains running when its
preview is stopped.

Static previews bind only to `127.0.0.1`, selecting a free port in 5300–5399.
They serve `index.html` for directory requests, use Go's MIME handling, and
return 404 instead of directory listings. A successful publish returns:

```json
{
  "id": "preview_<run-id>",
  "url": "https://example-preview.trycloudflare.com",
  "local_url": "http://127.0.0.1:5300",
  "expires_at": "2026-11-04T12:00:00Z"
}
```

## Tunnel lifecycle and installation

Install `cloudflared` using the [official downloads](https://developers.cloudflare.com/tunnel/downloads/)
and put it on the daemon's `PATH`. Alternatively set `EXCALIBUR_CLOUDFLARED`
to its executable path before starting the daemon. No user home path is compiled
into the tool. Missing executables produce installation guidance.

Each publish starts exactly this command, with a distinct owned process group:

```sh
cloudflared tunnel --no-autoupdate --url http://127.0.0.1:<port>
```

The daemon extracts an HTTPS `trycloudflare.com` URL from either output stream,
waiting up to 40 seconds. It then retries public HTTP GET verification for up
to 15 seconds; only a 2xx response succeeds. Verification does not follow
redirects. If the local resolver still negatively caches a fresh
`trycloudflare.com` hostname, verification retries its address lookup using
[Cloudflare DNS over HTTPS](https://developers.cloudflare.com/1.1.1.1/encryption/dns-over-https/make-api-requests/dns-json/).
This fallback applies only to missing tunnel hostnames, preserves the original
HTTP Host and TLS certificate checks, and does not change system DNS settings.
Failed startup, verification failure, interruption, and early process
exit close owned resources. Raw tunnel output is neither logged nor returned.
No preview URL is claimed on failure.

`{"action":"list"}` returns active, verified previews.
`{"action":"stop","id":"preview_<run-id>"}` stops one preview.
The default lifetime is two hours, with a maximum of 24 hours (86400 seconds).
Expiry and daemon shutdown stop owned tunnel processes and static servers.
Caller-owned app servers and project directories are preserved. Previews do
not resume after daemon restart.

[Cloudflare Quick Tunnels](https://developers.cloudflare.com/tunnel/get-started/quick-tunnels/)
create temporary public URLs. Anyone with the link can view the published
content; the hostname changes on each publish and has no uptime guarantee.

## Chat display

Successful previews are attached at the final assistant-message boundary using
the trusted executing run identity. Other runs cannot contribute preview links
to that reply. The persisted representation is ordinary Markdown with a title
carrying the expiry:

```markdown
[Public preview](https://example-preview.trycloudflare.com "public-preview:2026-11-04T12:00:00Z")
```

The chat renders these explicit links as compact cards with a globe icon,
English/Spanish title, URL opening in a new tab, clipboard button, and localized
expiry. The expiry label changes when its time elapses. The card uses semantic
theme colors and wrapping URLs for narrow screens, including 390 px widths.
History replay uses the same representation; other clients retain a clickable
Markdown link. Stopping a preview early does not rewrite earlier messages.

## Verification

`internal/daemon/public_preview_test.go` uses a temporary executable that emits
a fake hostname and stays alive until stopped. Public verification uses an
injected HTTP transport: tests make no Cloudflare or paid model calls. Coverage
includes publish/list/stop, TTL, shutdown, owned Windows job cleanup after process
exit, path containment (including symlinks
added after publishing), MIME types, index files, directory listings, existing
port ownership, input limits, missing executables, process exit, timeouts,
verification retries, cancellation, chat selection/execution, and final-message
link attachment, including a simulated model turn that publishes files and persists
the resulting preview link. Offline DNS tests cover the negative-cache fallback
and bounded public DNS responses.

`PublicPreviewCard.test.tsx` covers English/Spanish labels, safe external links,
expiry and elapsed lifetime, clipboard success/failure, persisted-message
rendering, duplicate links, and invalid metadata. The browser test in
`e2e/public-preview.spec.ts` checks history replay, copying, and card/link bounds
at 390 px in both languages and both themes.

The daemon embedded/eval hash tests and parity tests must continue to pass.


An optional real-tunnel smoke is available behind the live build tag. It
publishes only a tiny test page and makes no model calls:

```sh
EXCALIBUR_LIVE=1 go test -tags live ./internal/daemon -run '^TestPublicPreviewLive$'
```

Keep `cloudflared` on `PATH` (or set `EXCALIBUR_CLOUDFLARED`) for this check.
The test closes the public tunnel and local server before returning.

On 2026-11-04, a real Quick Tunnel smoke served the generated HTML over HTTPS
and confirmed that stopping the preview also stopped its owned local server.
The live check exposed a local negative-DNS-cache delay; the public DNS fallback
and 15-second verification window cover that startup behavior.
