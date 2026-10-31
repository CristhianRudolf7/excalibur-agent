---
name: finding-verification
description: Verify each finding with an independent agent that tries to reproduce or refute it.
metadata:
  excalibur:
    group: security
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Finding verification
      name_es: Verificación de hallazgos
      desc_en: Verify each finding with an independent agent that tries to reproduce or refute it.
      desc_es: Verificar cada hallazgo con un agente independiente.
---
# Finding verification

Use this skill to separate real vulnerabilities from plausible stories. An independent verifier reproduces or refutes each candidate, so the report holds only what survives contact with the system.

## When to use

- Candidate vulnerabilities need confirmation.
- A report must distinguish proven from suspected.
- False positives burned credibility before.

## Procedure

1. Hand the verifier the candidate with its claimed impact and reproduction sketch.
2. Require reproduction against the real configuration, not a toy setup.
3. Classify: confirmed, pending validation, or refuted — with the evidence for each.
4. Re-test fixes against the same reproduction before closing.
5. Keep refuted candidates on record so they are not re-reported.

## Output

Per-finding verdicts with reproduction evidence.

## Pitfalls

- Unreproduced findings are suspicions, not report entries.
- The verifier must be independent of the hunter.
