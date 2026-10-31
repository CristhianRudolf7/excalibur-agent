---
name: shell-power-use
description: "Run effective Linux shell pipelines: quoting, redirection, pipes, exit codes and finding files fast."
metadata:
  excalibur:
    group: terminal
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Shell power use
      name_es: Uso avanzado del shell
      desc_en: "Run effective Linux shell pipelines: quoting, redirection, pipes, exit codes and finding files fast."
      desc_es: "Tuberías, redirecciones, quoting y códigos de salida eficaces en el shell."
---
# Shell power use

Use this skill whenever you run more than a trivial command. Correct quoting, explicit exit-code handling and streaming pipelines are the difference between a command that works once and one that works on every machine.

## When to use

- Chaining commands with pipes, `&&`, `||` or `;`.
- Redirecting output to files or feeding files to commands.
- Searching the tree for files or text before editing.
- Debugging a command that "works locally" but fails in scripts.

## Procedure

1. Quote every expansion: `"$var"`, `"$(cmd)"`. Single quotes for literals, double quotes for expansions, never bare `$var` with paths.
2. Prefer `cmd1 | cmd2` streaming over temp files; add `set -o pipefail` in scripts so a middle failure is not masked by the exit code of the last command.
3. Check exit codes explicitly: `if ! cmd; then ...; fi`. `cmd1 && cmd2` means "only if success"; `cmd1 || cmd2` means "only if failure"; `cmd1; cmd2` always runs both.
4. Find files with `find <dir> -name '<glob>' -type f` or the `glob` tool; search contents with `grep -rn --include='*.py' '<pattern>' <dir>`. Use `rg` when installed.
5. Redirect deliberately: `> file` truncates, `>> file` appends, `2>&1` merges stderr, `cmd < in.txt` feeds stdin. Silence with `>/dev/null 2>&1` only after the command is proven.

## Pitfalls

- Unquoted variables split on spaces and glob-expand: `rm -rf $dir` with an empty `$dir` is a disaster; `rm -rf "$dir"` fails safely.
- `grep pattern` without `-r` reads stdin and hangs waiting for input; always give it a path or pipe.
- `cd` in one `exec` call does not persist to the next; pass `workdir` or chain with `&&` in a single call.
- The `exec` tool takes a command string only: never invent a `target` parameter (no `container`, `host`, `/app`); run in the project directory instead.

## Verification

- Re-run the pipeline with `echo "exit=$?"` after it to confirm the reported status is the real one.
- Re-run with `set -x` (or prefix `bash -x script.sh`) and confirm every expansion resolves to the intended words.

## References

- `references/pipeline-recipes.md` — copy-paste pipeline, find and xargs recipes.
