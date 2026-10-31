---
name: code-review
description: Review code quality and classify findings by severity, with file and line reference.
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - code_graph
    - lsp
    - github
    display:
      name_en: Code review
      name_es: Revisión de código
      desc_en: Review code quality and classify findings by severity, with file and line reference.
      desc_es: Revisar calidad y clasificar hallazgos por gravedad.
---
# Code review

Use this skill to review changes before they land. Severity-classified findings with exact references let authors fix what matters and dispute the rest on facts.

## When to use

- Reviewing a PR, branch, or worktree result.
- Acting as a second pair of eyes on critical code.
- Quality debates need a shared severity scale.

## Procedure

1. Read the diff completely; check the spec and criteria it claims to satisfy.
2. Verify behavior claims by running the touched tests, not by reading alone.
3. Classify each finding: blocker, should-fix, nit — with file and line.
4. Separate facts (fails, leaks, breaks) from taste (style, naming).
5. Approve only when blockers are resolved or explicitly accepted with reason.

## Output

A review with severity-classified findings, references, and a verdict.

## Pitfalls

- Taste is never a blocker; say so and move on.
- Unrun code is unreviewed; execute the claim.
