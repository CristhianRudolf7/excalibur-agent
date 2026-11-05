---
title: 'Quick start'
description: 'Send a first request and inspect a background task.'
---

Use this guide after [installing the binary](/docs/getting-started/install/) and [connecting a model provider](/docs/getting-started/providers/). The examples assume `excalibur` is on your `PATH`. Run project requests from a directory that Excalibur may read and modify.

## Send a first request

For a single non-interactive request:

```sh
excalibur exec "List the top-level files in this directory and explain their purpose."
```

`exec` runs the request and prints a final summary. It can use tools and change files when the request calls for it. It does not pause to ask follow-up questions, so include your constraints in the request. If no provider is configured, it fails rather than supplying a simulated answer.

For continuing conversations, start the daemon in a separate terminal:

```sh
excalibur daemon start
```

Then send a message from another terminal:

```sh
excalibur chat "Help me plan a small documentation update."
```

The conversation command needs a reachable daemon. Starting the daemon does not automatically open a browser or the web application.

## Start work in the background

A background task returns an identifier immediately:

```sh
excalibur task start "Review the README for broken relative links. Report findings without editing files."
```

Copy the returned task identifier. Use it in `excalibur task status TASK_ID`, `excalibur task wait TASK_ID`, and `excalibur task result TASK_ID`. Replace `TASK_ID` with the actual identifier; it is a placeholder, not a special keyword.

A task can stop for a question, become blocked, or fail. Read its status and result before assuming the request is complete. The [CLI reference](/docs/reference/cli/) covers replies, cancellation, and exit codes.

If a request cannot connect to a model, check the provider name, model identifier, and credential source first. Credentials must be available to the process that runs the request, including the daemon when you use chat.
