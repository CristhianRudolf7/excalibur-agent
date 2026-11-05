---
title: 'Coding'
description: 'Give a project task and inspect the commands, changes, and result.'
---

Excalibur can inspect files, search a project, edit code, and run terminal commands. Start from a project directory you want it to work on and connect a model provider. Describe the problem and the expected behavior, along with any files or commands it should avoid.

## Give a bounded request

For example:

```sh
excalibur exec "Find the cause of the failing parser test. Make the smallest fix and run the relevant tests. Do not commit changes."
```

This is an actionable request: it allows file edits and test execution. If you only want a review, say so explicitly. Non-interactive `exec` does not stop to ask follow-up questions, so include important constraints at the start.

## Read the activity in chat

During programming work, the conversation can show run and subagent activity, plan steps, file reads, commands, edits, and test output. Expand entries to inspect the output or diff when available. Truncated and binary results have their own limited presentations.

The file panel can open workspace files and show changed lines, including removed lines and navigation between changes. Some fallback content in the current editor is sample data. Do not treat a sample project or a generic status label as evidence about your repository.

The current browser editor also keeps manual edits in local interface state. Do not assume typing there has written changes back to disk. Ask the agent to apply an edit, then verify the actual file or diff.

## Check the delivery

Read the final result and the command output. A successful test command supports only the checks that actually ran. If work failed or stopped, inspect its state before starting another request that depends on it.

Use [background tasks](/docs/getting-started/quick-start/) for work you want to inspect later. For a website you want to open on another device, ask for a [public preview](/docs/building/public-preview/) after it has been built.
