---
name: git-history-surgery
description: "Rewrite git history safely: rebase, fixups and splits, log search, filter-repo purges and signed pushes."
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Git history surgery
      name_es: Cirugía del historial git
      desc_en: "Rewrite git history safely: rebase, fixups, splits, filter-repo and signed pushes."
      desc_es: "Reescribir el historial con seguridad: rebase, fixups y filter-repo."
---
# Git history surgery

Use this skill to clean up local history before sharing: squash fixups, split tangled commits, drop secrets or giant files. Only rewrite history you own.

## When to use

- Polishing a private branch (squash, reword, reorder).
- Removing a committed secret or huge file from history.
- Splitting one commit into logical pieces or extracting a file's history.

## Procedure

1. Guard first: `git status` clean, note the base (`git merge-base HEAD main`), and tag a backup (`git branch backup-<date>`).
2. Interactive rebase for local polish: `git rebase -i <base>` with squash/fixup/reword; `git commit --fixup <sha>` + `git rebase -i --autosquash` for review-driven fixes.
3. Split a commit: `git reset HEAD~1` (keeps work), stage logical hunks (`git add -p`), commit each piece with its own message.
4. Purge files/secrets repo-wide with `git filter-repo --path <file> --invert-paths` (install it; do NOT use `filter-branch`); every clone must re-clone afterwards.
5. Push deliberately: `git push --force-with-lease` on your own branch only; never force-push shared branches — revert there instead.

## Pitfalls

- Rewriting public history strands every collaborator; the rule is absolute for shared branches.
- `filter-branch` is deprecated and dangerous; `filter-repo` is the supported tool.
- Dropping a secret from git without rotating it is theater; rotate first (see `secrets-hygiene`).

## Verification

- `git log --graph --oneline` shows the intended shape; `git show` per commit is coherent; CI passes on the rewritten branch; the purged file is absent from `git rev-list --objects --all`.

## References

- `references/history-rewrite-checklist.md` — pre-rewrite backup and post-rewrite checks.
