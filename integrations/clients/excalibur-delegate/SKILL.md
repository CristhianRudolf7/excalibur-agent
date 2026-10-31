---
name: excalibur-delegate
description: Delegate a self-contained task to the Excalibur agent harness via its CLI or MCP server, then follow progress and collect the delivery report.
license: Excalibur Source-Available License
---

# Excalibur delegation

Use this skill when a task is self-contained and verifiable (implement a
function with its tests, fix a reproducible bug, investigate a repo,
migrate a module) or when long work should leave your own context free.
Do not delegate one-line changes or decisions that depend on the current
conversation.

## Delegate over the CLI

Create the task and return immediately:

    excalibur task start "<request>" --project DIR [--profile delegate] [--worktree] [--budget-usd X] [--max-steps N] [--timeout S]

Output is JSON: {"task_id": "T45", "status": "in_progress"}.

Follow progress and collect the result:

    excalibur task status T45
    excalibur task wait T45 --timeout 300
    excalibur task result T45 [--format md]

If the task asks a question, answer it or add a directive:

    excalibur task reply T45 "<text>"

Control it with pause, resume, cancel; inspect with list and logs:

    excalibur task pause T45
    excalibur task resume T45
    excalibur task cancel T45
    excalibur task list [--project DIR]
    excalibur task logs T45 [--follow]

Exit codes match the run state: 0 completed, 2 completed with pending
items, 3 blocked, 4 paused, 5 failed, 130 canceled, 1 harness error.

With --never-ask the task assumes instead of asking and records every
assumption in the delivery report.

## Delegate over MCP

When the client supports MCP, configure the excalibur server with:

    excalibur mcp install --client <codex|claude-code|opencode|muse-code> [--print]

Then call excalibur_delegate (set wait:true for the final result),
excalibur_status, excalibur_wait, excalibur_result, excalibur_reply,
excalibur_cancel and excalibur_list. Progress arrives as MCP progress
notifications during wait.

## Results

Results name the branch and diff when a worktree was used; nothing in
the delegating tree changes until you integrate it. The report carries
the summary, files changed, criteria with evidence, assumptions and
anything still pending.
