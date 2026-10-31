---
name: process-job-control
description: "Inspect and control Linux processes: ps, kill signals, background jobs and runaway tasks."
metadata:
  excalibur:
    group: terminal
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Processes and job control
      name_es: Procesos y control de trabajos
      desc_en: "Inspect and control Linux processes: ps, kill signals, background jobs and runaway tasks."
      desc_es: "Inspeccionar y controlar procesos Linux: ps, señales, trabajos y fugas."
---
# Processes and job control

Use this skill when a command hangs, a port is busy, or a runaway process eats CPU or memory. Observe first, signal gently, escalate only if needed.

## When to use

- A previous command is still running or left a server holding a port.
- CPU or memory spikes with no obvious owner.
- Background work must keep running after the command returns.

## Procedure

1. List with `ps aux | grep -v grep | grep <name>` or `pgrep -af <pattern>`; see the tree with `ps -ef --forest`.
2. Check resources with `top -b -n1 | head -30` (or `htop` interactively); confirm the PID owns the symptom before touching it.
3. Signal in order: `kill <pid>` (TERM, lets it clean up), wait, then `kill -QUIT` for a dump, and only then `kill -9 <pid>` (KILL, no cleanup).
4. Never `kill -9` databases or build tools on first resort; prefer their stop command (`systemctl stop`, `docker stop`).
5. For durable background work use `nohup cmd &`, `tmux` (see `tmux-persistent-sessions`) or a systemd unit (see `systemd-units-services`); record the PID and a log file.

## Pitfalls

- `pkill <pattern>` matches its own command line and siblings; prefer `pkill -f '^exact'` or kill by PID.
- `kill -9` on a parent orphans children holding ports; kill the process group or the launcher instead.
- A "busy port" is a symptom: find the owner with `ss -ltnp` before killing anything (see `network-ports-debugging`).

## Verification

- `ps -p <pid>` reports no such process and `ss -ltnp` shows the port free.
- The log shows a clean shutdown line when TERM was used.
