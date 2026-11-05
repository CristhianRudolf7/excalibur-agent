---
title: 'Gmail, Drive, and Google Docs'
description: 'Connect Google and review external changes before approving them.'
---

Connect your Google account in Settings → Connections before asking Excalibur to work with Gmail, Drive, or Google Docs. The installation needs a configured Google OAuth client. If the connection card says the service is not configured, account sign-in alone cannot fix the missing installation setup.

## Connect and check access

Select the Google connection action, complete authorization in the browser, and return to Settings. The card reports the connected account and missing permissions when available. Follow its reconnect action if access has expired or required permissions are missing.

The implemented Google tools cover Gmail search, reading and drafts; Drive file search and file operations; and reading and editing Google Docs. They do not imply that every Google product has its own direct integration. For example, do not assume Calendar is connected just because Gmail works.

## Draft first, then approve sending

A useful request is:

> Draft a reply to this Gmail thread thanking the sender. Show me the recipients and subject before sending.

Identify the actual thread in your request. Excalibur can prepare a draft without sending it. Sending requires explicit approval in chat and a confirmed tool call. Review recipients, subject, body, and attachments before approving. A draft result is not a sent message.

Confirmation also applies to Gmail trash and permanent deletion, Drive sharing, trash and deletion, and Google Docs text replacement. These are specific gates; not every write operation has the same confirmation step. Appending text and creating a document can change your account without that replacement gate.

Disconnecting stops Excalibur's Google connection. It does not delete messages or files already created in your account. For website passwords, use the [browser credential forms](/docs/everyday/browser-passwords/), not the Google connection card. See [Security and permissions](/docs/reference/security/) before approving changes to externally shared material.
