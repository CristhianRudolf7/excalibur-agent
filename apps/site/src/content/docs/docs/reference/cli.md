---
title: 'CLI reference'
description: 'Find commands, control a delegated task, and interpret its exit state.'
---

Run `excalibur` without arguments to list commands. In this version, `excalibur --help` shows only the global flag help. Command families such as `excalibur task --help` list their subcommands. Some subcommand help requests print help and also exit with an error, so do not use their exit status as a daemon health check.

## Common commands

| Command                    | Purpose                            |
| -------------------------- | ---------------------------------- |
| `excalibur daemon start`   | Run the daemon                     |
| `excalibur daemon stop`    | Request daemon shutdown            |
| `excalibur chat "MESSAGE"` | Send a conversation message        |
| `excalibur exec "REQUEST"` | Run a non-interactive request      |
| `excalibur status`         | List run status through the daemon |
| `excalibur tasks`          | List tasks through the daemon      |
| `excalibur projects`       | List registered projects           |
| `excalibur memory "QUERY"` | Query memory                       |
| `excalibur skills "QUERY"` | Search skills                      |
| `excalibur ext --help`     | List extension subcommands         |
| `excalibur vault --help`   | List vault subcommands             |

Quoted uppercase text and identifiers below are placeholders. Replace them with your request or the actual returned identifier. Chat, status, tasks, projects, memory, and skills require a reachable daemon. `exec` can run without one and still needs a configured provider.

## Follow a background task

```sh
excalibur task start "Review the README without editing files."
excalibur task status TASK_ID
excalibur task wait TASK_ID --timeout 300
excalibur task result TASK_ID --format md
```

`task start` returns immediately with an identifier. If a question is pending, answer with `excalibur task reply TASK_ID "ANSWER"`. The `task` family also includes `pause`, `resume`, `cancel`, `list`, and `logs`. Use the same `--project` directory for subsequent commands when the task is stored in project state.

The global `--json` flag goes before the command, as in `excalibur --json status`. `exec` also accepts `--json` to stream machine-readable events and a final summary.

## Interpret completion

For `exec` and terminal task states returned by `task wait`:

| Code | Meaning                                  |
| ---- | ---------------------------------------- |
| 0    | Completed                                |
| 1    | Command, configuration, or runtime error |
| 2    | Completed with pending items             |
| 3    | Blocked                                  |
| 4    | Paused or execution timeout              |
| 5    | Failed                                   |
| 130  | Canceled                                 |

A bounded wait can return while work is still active. Read the reported state, not only the exit code. See [Quick start](/docs/getting-started/quick-start/) for prerequisites and [Configuration](/docs/reference/configuration/) for provider settings.
