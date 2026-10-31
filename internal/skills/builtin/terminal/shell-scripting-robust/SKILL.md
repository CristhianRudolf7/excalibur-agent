---
name: shell-scripting-robust
description: "Write robust bash scripts with strict mode, traps, argument parsing and safe temp files."
metadata:
  excalibur:
    group: terminal
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - write_file
    display:
      name_en: Robust shell scripting
      name_es: Scripts shell robustos
      desc_en: "Write robust bash scripts with strict mode, traps, argument parsing and safe temp files."
      desc_es: "Scripts bash robustos con modo estricto, traps y parseo de argumentos."
---
# Robust shell scripting

Use this skill when a task needs a `.sh` file instead of one-liners: setup scripts, reproducers, glue between tools. A strict-mode header plus cleanup traps eliminates the most common script bugs.

## When to use

- Writing or fixing any `.sh` script longer than a few lines.
- A script fails silently, halfway, or only on some machines.
- Parsing flags, handling temp files, or cleaning up on failure.

## Procedure

1. Start every script with the strict header:
   ```bash
   #!/usr/bin/env bash
   set -euo pipefail
   IFS=$'\n\t'
   ```
2. Add a cleanup trap for temp state: `tmpdir=$(mktemp -d); trap 'rm -rf "$tmpdir"' EXIT`.
3. Parse arguments with a `while [[ $# -gt 0 ]]; do case "$1" in ... esac; shift; done` loop; default with `${VAR:-default}` and require with `${VAR:?message}`.
4. Prefer functions with `local` variables; quote all call sites; return non-zero on error and let `set -e` propagate unless you handle it with `|| return`.
5. Make executable with `chmod +x` and test with `bash -n script.sh` (syntax) then `bash -x script.sh` (trace).

## Pitfalls

- `set -e` ignores failures inside `if` conditions and `||` lists by design; check those explicitly.
- `source script.sh` runs in your shell; prefer executing (`./script.sh`) to avoid leaking variables.
- Never `curl ... | bash` from untrusted sources; download, read, then run.

## Verification

- `bash -n` passes, `shellcheck script.sh` is clean when shellcheck is installed, and the script exits 0 on the happy path and non-zero on a forced failure.
- Re-run twice; the second run must be idempotent or fail with a clear "already done" message.

## References

- `references/strict-mode-template.sh` — starter script with header, traps and arg parsing.
