# 0111: Daemon coding activity events

Interactive daemon runners publish the existing `tool.called`, `tool.completed`,
`file.edited`, and `run.progress` event names. Additive optional fields associate
activity with a conversation, root turn, parent run, and tool call. Existing event
consumers remain compatible; no tool definitions, model requests, or evaluation
profiles change.

Tool previews expose selected path/command fields, never raw arguments. The
existing vault redactor runs before UTF-8-safe clipping. Outputs and per-edit
diffs are limited to 8 KiB, commands to 2 KiB, paths to 512 bytes, and names and
call/session identifiers to 128 bytes. Up to 50 files are observed per call and
20 completion steps per run. Output previews keep the tail; diffs keep the head.
Truncation is explicit. Exit codes remain optional while a process is alive.

Diffs come from the daemon's shared version history, sampled across the tool
invocation and filtered by executing run. This preserves individual edits,
including edits that restore earlier contents. It avoids attributing unrelated
working-tree modifications to a turn. Shell-created changes without a recorded
file version are visible through command output, not synthesized as edit events.

Delegation progress retains its parent and root turn through nested runs. Steps
are the delegation card's completion criteria; they are completed together only
when the run succeeds. They are not inferred from tool names. Existing final
message changes and activity state remain available.
