---
name: commit-comparison
description: Look up local and GitHub commit SHAs and compare their differences to know which changes
  were made and which are published.
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - github
    - exec
    display:
      name_en: Commit comparison
      name_es: Comparación de commits
      desc_en: Look up local and GitHub commit SHAs and compare their differences to know which changes
        were made and which are published.
      desc_es: Comparar SHAs locales y de GitHub.
---
# Commit comparison

Use this skill to answer 'what changed and what is published'. Comparing local and remote SHAs tells you exactly which work is safe, which is pending, and which diverged.

## When to use

- Before pushing, merging, or integrating.
- After a fetch, to see what moved upstream.
- Debugging 'it works here but not there'.

## Procedure

1. Resolve the local SHA and the remote SHA for each relevant ref.
2. Compare the ranges: list commits present on each side only.
3. Review the diff of the unpushed or unmerged side for surprises.
4. Confirm the working tree is clean before any integrating action.
5. Record the SHAs compared so the decision is auditable.

## Output

The comparison: SHAs, exclusive commits per side, diff summary, and the resulting action.

## Pitfalls

- Never push or merge with a dirty tree or an unread diff.
- Diverged histories need an explicit strategy, not a forced push.
