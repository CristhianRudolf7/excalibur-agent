---
name: file-permissions-ownership
description: "Diagnose and fix Linux file permissions, ownership and executable bits safely."
metadata:
  excalibur:
    group: terminal
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: File permissions and ownership
      name_es: Permisos y propiedad de ficheros
      desc_en: "Diagnose and fix Linux file permissions, ownership and executable bits safely."
      desc_es: "Diagnosticar y corregir permisos, propiedad y bits de ejecución en Linux."
---
# File permissions and ownership

Use this skill for "permission denied", scripts that will not execute, or files owned by the wrong user after container or sudo work.

## When to use

- `Permission denied` or `EACCES` errors on read, write or execute.
- A script exists but `./script.sh` says it cannot be executed.
- Files created by root or a container need to belong to the normal user.

## Procedure

1. Inspect with `ls -l <path>` and `namei -l <path>` (shows every path component; a missing `x` on any parent directory blocks access).
2. Read the bits as `rwxrwxrwx` = owner/group/other; directories need `x` to enter and `r` to list, files need `x` to execute.
3. Fix minimally: `chmod +x script.sh` for execution, `chmod 644 file` / `chmod 755 dir` for standard sharing, `chmod -R` only when the whole tree truly shares one policy.
4. Fix ownership with `chown user:group file` (or `sudo chown -R $(id -u):$(id -g) dir` for trees polluted by root); never `chmod 777` as a fix.
5. For SSH keys and secrets: private keys must be `600`, the `.ssh` directory `700`, otherwise clients refuse them.

## Pitfalls

- `chmod 777` masks the real problem and creates a security hole; undo it with the least privilege that works.
- Mounts (`noexec`, read-only) and ACLs (`getfacl`) override plain bits; check them when chmod "does nothing".
- Running the agent or tools with `sudo` to dodge permissions creates root-owned files that break the next run.

## Verification

- Re-run the failing command as the normal user and confirm success.
- `ls -l` shows the intended bits and `namei -l` shows no blocked component.
