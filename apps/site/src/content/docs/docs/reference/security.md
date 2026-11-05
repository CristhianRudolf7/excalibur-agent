---
title: 'Security and permissions'
description: 'Handle credentials and understand the limits of confirmation controls.'
---

Excalibur can read files, run commands, change local data, and act through connected services. Give it access only to projects and accounts you intend to use. A conversation instruction helps define the task, but it is not a substitute for operating-system permissions or an isolated environment.

## Keep credentials out of messages

Use the credential forms in the conversation for website logins and one-time codes. Use provider environment variables or vault references for API credentials. Normal chat messages are not secret-entry forms.

The vault stores encrypted secret values and supports locking, unlocking, scoped entries, allowed tools, and website domains. Listings expose metadata rather than values. To inspect the available CLI controls:

```sh
excalibur vault --help
```

The `set` command accepts a value through hidden terminal input or `--stdin`, not as a value argument. `get` deliberately reveals a value, so do not use it in shared terminal recordings. See [Browser and password vault](/docs/everyday/browser-passwords/) for the web forms.

## Review external actions

Gmail sending, trash, and deletion require explicit approval in chat. Drive sharing, trash, and deletion and Google Docs text replacement also have confirmation gates. Other operations, including some writes, have different rules. Do not assume that every external change waits for an approval dialog.

A request for a public preview authorizes publication. Anyone with its link can view the published content. Review the files before requesting that link and stop the preview when it is no longer needed.

## Understand what is shared

Personal agents have separate conversations but share memory and user resources. They do not provide account isolation. A terminal request can edit files and execute commands, and non-interactive `exec` does not stop for follow-up questions.

Some platform protections differ between operating systems; Windows and macOS support is still in progress. These controls do not make arbitrary extensions or shell commands safe. Review extension requirements and the actual command or file changes when the result matters. Consult [Configuration](/docs/reference/configuration/) for settings and [Google tools](/docs/everyday/google/) for account-specific actions.
