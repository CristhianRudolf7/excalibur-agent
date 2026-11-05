---
title: 'Public preview with Cloudflare'
description: 'Share a temporary link to a built website or a running local server.'
---

Ask for a public preview when you want to open a website on your phone or share it with someone else. Excalibur uses Cloudflare Quick Tunnels. The machine running the daemon needs the `cloudflared` executable on `PATH`, or its path configured through `EXCALIBUR_CLOUDFLARED`.

## Request a public link

After building your website, say:

> Publish a temporary public preview of this site so I can open it on my phone.

An explicit request for a public or Cloudflare link authorizes publication. Excalibur does not need another confirmation for that request. Without an explicit publication request, the preview tool rejects publishing.

The tool can serve a built static directory inside an allowed project or workspace root. It can also expose an existing HTTP server on loopback. A source directory that needs a build is not automatically a finished static website. Ask Excalibur to build it first.

## Check the returned link

A successful preview returns an actual public URL and an expiry time. The conversation can show these in a preview card. Do not assume a link exists just because the assistant said it was starting a tunnel. If publication fails, resolve the reported problem and retry.

Anyone with the link can view the published content. Choose the output directory or local server carefully and avoid publishing private documents or development pages containing credentials. This is a public preview, not an authenticated review room.

The default lifetime is two hours, with a maximum requested lifetime of 24 hours. Previews also stop when the daemon shuts down. You can ask Excalibur to list active previews and stop a particular one before its expiry.

Stopping the preview closes the tunnel and any preview server owned by Excalibur. It does not stop a local HTTP server that you started separately. For the work that creates the website, see [Coding](/docs/building/coding/).
