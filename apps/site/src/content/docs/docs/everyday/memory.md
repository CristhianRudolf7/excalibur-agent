---
title: 'Memory'
description: 'Save useful preferences and retrieve information from earlier work.'
---

Use memory for information that should remain useful beyond the current message: a preference, a recurring constraint, or a fact you want Excalibur to recall. Memory is separate from the document library and from the full conversation history.

## Make a preference explicit

For example, tell your agent:

> Remember that I prefer dates written as year-month-day in reports.

Excalibur has tools to store facts and preferences and to search stored memories. It also has a separate history search for earlier messages and actions. Ask what it remembers about a subject when you want to check the information being used.

Memories can have a scope and validity dates. A general preference and a fact about one project need not apply in the same places. When a fact changes, explain what changed and when. Stored information can be refined or replaced; do not assume that a new statement silently erases every older mention in chat history.

## Query from the terminal

With the daemon running, you can query general memory:

```sh
excalibur memory "report date format"
```

The default scope is `general`. The command also accepts `--scopes` for a comma-separated scope set. It returns the daemon's memory query result; it does not start a new conversational reply.

Personal agents share user memory even though their conversations are separate. Changing agents therefore does not create a private memory partition. See [Agents and subagents](/docs/building/agents/) before using multiple personas to organize work.

Retrieval is a way to find relevant material, not a guarantee that every earlier statement will appear in every answer. Check important facts in their original source. Use [Documents and citations](/docs/everyday/documents/) for passages from files and [the password vault](/docs/everyday/browser-passwords/) for credentials rather than storing passwords as ordinary remembered facts.
