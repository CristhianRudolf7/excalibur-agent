---
name: interpretability
description: Analyze the internal functioning of models through activations and interventions.
metadata:
  excalibur:
    group: science
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Interpretability
      name_es: Interpretabilidad
      desc_en: Analyze the internal functioning of models through activations and interventions.
      desc_es: Analizar el funcionamiento interno con activaciones.
---
# Interpretability

Use this skill to look inside models. Activation analysis and causal interventions test whether an explanation holds or merely sounds plausible.

## When to use

- Understanding a model behavior mechanistically.
- Testing whether a feature causally matters.
- Auditing what a model represents.

## Procedure

1. Formulate the mechanistic hypothesis precisely enough to falsify.
2. Analyze activations for correlates, then intervene to test causality.
3. Require the intervention to move behavior as predicted.
4. Check generality: does the mechanism hold across inputs.
5. Report correlates distinguished from causes.

## Output

Mechanistic findings with interventional evidence.

## Pitfalls

- Correlates are not causes; intervene or qualify.
- Single-example mechanisms rarely generalize; test broadly.
