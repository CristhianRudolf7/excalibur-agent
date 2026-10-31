# History rewrite checklist

For `git-history-surgery`. Do every step in order; stop at the first red item.

## Before

- [ ] `git status` is clean (stash or commit everything first).
- [ ] Backup branch created: `git branch backup-$(date +%Y%m%d)`.
- [ ] Base identified: `git merge-base HEAD main` (or the real upstream).
- [ ] The branch is yours alone: `git log --format='%ae' <base>..HEAD | sort -u` shows only you.
- [ ] Secrets rotated if any commit ever contained one (rewriting alone is not enough).

## During

- [ ] Rebase todo reviewed line by line before `:wq`.
- [ ] Each rewritten commit builds and its tests pass (or is marked fixup to a commit that does).
- [ ] Commit messages state why, not just what.

## After

- [ ] `git log --graph --oneline <base>..HEAD` shows the intended shape.
- [ ] `git push --force-with-lease` only (never `--force` on shared remotes).
- [ ] CI green on the rewritten branch.
- [ ] Purged paths gone: `git rev-list --objects --all | grep <path>` prints nothing.
- [ ] Backup branch deleted only after a week of calm.
