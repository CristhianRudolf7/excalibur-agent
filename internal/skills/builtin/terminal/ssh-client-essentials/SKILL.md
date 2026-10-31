---
name: ssh-client-essentials
description: "Connect, copy and run remote commands over SSH with keys, config and multiplexing."
metadata:
  excalibur:
    group: terminal
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: SSH client essentials
      name_es: Esenciales del cliente SSH
      desc_en: "Connect, copy and run remote commands over SSH with keys, config and multiplexing."
      desc_es: "Conectar, copiar y ejecutar comandos remotos por SSH con claves y config."
---
# SSH client essentials

Use this skill for remote shells, file copies and one-shot remote commands. Key auth plus `~/.ssh/config` turns every host into a one-word command.

## When to use

- Logging into a remote machine or jumping through a bastion.
- Copying files to or from a remote host.
- Running a command remotely from a script.

## Procedure

1. Generate a key once: `ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519 -N ''`; install with `ssh-copy-id user@host`.
2. Pin hosts in `~/.ssh/config`:
   ```
   Host shortname
     HostName example.com
     User deploy
     IdentityFile ~/.ssh/id_ed25519
   ```
   then `ssh shortname` and `scp file shortname:/path/` just work.
3. One-shot commands: `ssh shortname 'cd /srv/app && ./deploy.sh'`; quote the remote command so local expansion cannot leak in.
4. Speed up repeats with multiplexing (`ControlMaster auto`, `ControlPath ~/.ssh/cm-%r@%h:%p`) or jump hosts (`ProxyJump bastion`).
5. Debug with `ssh -v shortname`; fix `UNPROTECTED PRIVATE KEY FILE` with `chmod 600` on the key (see `file-permissions-ownership`).

## Pitfalls

- Passwords in scripts are brittle; use keys with an agent (`ssh-add`) instead.
- `scp` needs the remote path quoted when it contains spaces; prefer `rsync -avz` for trees.
- Changing host keys trigger warnings: verify out-of-band before deleting the old entry with `ssh-keygen -R host`.

## Verification

- `ssh -o BatchMode=yes shortname true` exits 0 without prompting.
- `scp` round-trips a file and `diff` confirms identical contents.
