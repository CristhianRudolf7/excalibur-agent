---
name: tmux-persistent-sessions
description: "Keep long-running terminal work alive with tmux sessions, windows and logging."
metadata:
  excalibur:
    group: terminal
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Persistent sessions with tmux
      name_es: Sesiones persistentes con tmux
      desc_en: "Keep long-running terminal work alive with tmux sessions, windows and logging."
      desc_es: "Mantener trabajo largo vivo con sesiones, ventanas y logs de tmux."
---
# Persistent sessions with tmux

Use this skill for builds, test suites or servers that outlive a single command: start them in tmux, detach, and reattach later to read the scrollback.

## When to use

- A command runs longer than a few minutes.
- A server must stay up while you do other work.
- Output must survive disconnects and be reviewable later.

## Procedure

1. Create a named session: `tmux new -s work -d 'cd /srv/app && ./run-tests.sh 2>&1 | tee /tmp/tests.log'`; list with `tmux ls`.
2. Attach with `tmux attach -t work`, detach with `Ctrl-b d` (the process keeps running).
3. Read output without attaching: `tmux capture-pane -p -t work -S -200` (last 200 lines).
4. Send input to a session: `tmux send-keys -t work 'make smoke' Enter`.
5. Kill finished sessions with `tmux kill-session -t work`; enable logging per pane when the record matters.

## Pitfalls

- Forgetting the session name strands work; always use `-s` names tied to the task.
- Scrollback is finite: `tee` to a file for anything you must keep.
- Nested tmux (inside tmux) double-prefixes keys; prefer one level or adjust the prefix.

## Verification

- `tmux ls` shows the session alive; `capture-pane` shows recent expected output.
- After detach and reattach, the same session and history are present.
