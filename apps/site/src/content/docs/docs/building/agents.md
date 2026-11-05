---
title: 'Agents and subagents'
description: 'Create a personal agent and understand task delegation.'
---

Use personal agents to keep distinct conversations and personalities. Use subagents when a task needs delegated workers. These serve different purposes: a personal agent remains in your agent list, while a subagent works within a run.

## Create a personal agent

In agent settings, select the create-agent action. It opens the favorite agent's conversation with a prepared request. Review and send that request; opening the conversation alone does not create anything.

For example:

> Create a personal agent called Writing partner. It should help me revise technical explanations and ask about the audience before suggesting a rewrite.

You can then choose the agent from the selector. Settings let you update its name, title, description, and personality, choose a favorite, archive it, or restore an archived agent. Avatar requests also go through a conversation.

Each personal agent uses its own conversation history, including voice turns. Agents share user memory and resources. They are not separate user accounts or security boundaries. Archiving preserves history, removes the agent from active selectors, and cannot remove the last active agent.

## Delegate a task

For programming work, describe the goal, boundaries, and expected result. Excalibur's task system can run delegated work and subagents. A parent run coordinates its workers; you do not need to create a permanent personal agent for every file review.

From the terminal, start a bounded review:

```sh
excalibur task start "Review this project's README and report unclear setup instructions without editing files." --max-steps 20
```

The task returns an identifier. Inspect its status and result using the [CLI reference](/docs/reference/cli/). A background task may need an answer or stop at a limit. A running worker is not evidence that the requested outcome is finished.

If you want to separate chats, create a personal agent. If you want to divide one piece of work, ask for delegation. See [Coding](/docs/building/coding/) for reviewing commands and file changes.
