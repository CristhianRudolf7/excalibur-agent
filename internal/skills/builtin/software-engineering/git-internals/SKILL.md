---
name: git-internals
description: "Read git history precisely: log, blame and bisect over objects, refs and reflog recovery."
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Git internals
      name_es: Interioridades de git
      desc_en: "Read git history precisely: objects, refs, blame, bisect and reflog recovery."
      desc_es: "Leer el historial git con precisión: objetos, refs, blame y bisect."
---
# Git internals

Use this skill when `git log` is not enough: finding when a line changed, which commit broke a test, or recovering "lost" work.

## When to use

- Blaming a line, finding a regression commit, or understanding a tangled merge.
- Recovering deleted branches, dropped stashes or bad resets.
- Reading packed history efficiently in large repos.

## Procedure

1. Blame precisely: `git blame -L <start>,<end> -- <file>` for the lines, `git log -S '<string>' --oneline -- <path>` (pickaxe) for when text appeared or vanished.
2. Bisect regressions: `git bisect start`, mark `git bisect bad` (now) and `git bisect good <old>`, then test each step and answer `good/bad`; finish with `git bisect reset`. Automate with `git bisect run <cmd>`.
3. Recover with the reflog: `git reflog` lists where HEAD was; restore with `git reset --hard <sha>` or `git branch rescue <sha>` — nothing is lost until gc runs.
4. Read structure: `git show <sha> --stat`, `git log --graph --oneline -20`, `git ls-tree -r HEAD <dir>`; inspect objects with `git cat-file -p <sha>` when plumbing matters.
5. Keep it fast: `git log --oneline -- <path>` scopes history; `--since/--until` and `-n` bound scans in huge repos.

## Pitfalls

- `git reset --hard` destroys uncommitted work; `git stash` first when unsure.
- Bisect lies when the test is flaky or the breakage is environmental; stabilize the test command before starting.
- Force-pushing shared branches rewrites teammates' history; prefer reverts on shared lines.

## Verification

- The blamed commit actually introduces the line (`git show` confirms); bisect converges to one commit whose parent passes and which fails.
