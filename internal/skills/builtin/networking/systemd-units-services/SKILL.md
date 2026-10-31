---
name: systemd-units-services
description: "Run work as systemd units: service files, enablement, logs and timers."
metadata:
  excalibur:
    group: networking
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: systemd units and services
      name_es: Unidades y servicios systemd
      desc_en: "Run work as systemd units: service files, enablement, logs and timers."
      desc_es: "Trabajo como unidades systemd: ficheros, enable, logs y timers."
---
# systemd units and services

Use this skill to keep a service or scheduled job running across reboots. Units beat nohup and cron for anything that must restart, log and be inspectable.

## When to use

- A server or worker must survive reboots and crashes.
- Replacing a fragile `nohup ... &` or a cron line with logging.
- Reading why a service failed to start.

## Procedure

1. Write `/etc/systemd/system/<name>.service` with `ExecStart` (absolute paths), `User=`, `WorkingDirectory=`, `Restart=on-failure`, and `Environment=` or `EnvironmentFile=` for config.
2. Activate: `systemctl daemon-reload && systemctl enable --now <name>`; check with `systemctl status <name>`.
3. Read logs with `journalctl -u <name> -e` (add `-f` to follow, `--since '10 min ago'` to scope).
4. For schedules prefer `.timer` units over cron: `OnCalendar=daily`, same logging and status story.
5. User-level work without root: `systemctl --user` units in `~/.config/systemd/user/`.

## Pitfalls

- Relative paths and unexported env vars are the top failure: unit environments are minimal by design.
- Forgetting `daemon-reload` after editing the file silently runs the OLD definition.
- `Restart=always` on a fundamentally broken command burns CPU; fix the start first, then decide the policy.

## Verification

- `systemctl is-active <name>` prints `active`, `journalctl -u <name>` shows clean startup, and a `systemctl restart` plus reboot-cycle (when feasible) recovers automatically.
