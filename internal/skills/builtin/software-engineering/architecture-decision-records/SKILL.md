---
name: architecture-decision-records
description: Record architecture decisions with their motive and alternatives in decisions.
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - task_db
    - doc_edit
    display:
      name_en: Architecture decision records
      name_es: Registros de decisiones de arquitectura
      desc_en: Record architecture decisions with their motive and alternatives in decisions.
      desc_es: Registrar decisiones con motivo y alternativas.
---
# Architecture decision records

Use this skill so future readers know why the system looks this way. A short record per decision — context, choice, alternatives, consequences — prevents relitigation and accidental reversal.

## When to use

- Making a decision that shapes structure or constraints.
- Choosing between architectures, patterns, or platforms.
- Future maintainers will ask 'why is this like this'.

## Procedure

1. Write the context in two lines: what forced the decision now.
2. State the choice and the concrete reasons it won.
3. Record the alternatives considered and why each lost.
4. Note consequences: what becomes easier, what becomes harder.
5. File the record in decisions with a stable numbered name.

## Output

An ADR filed with context, choice, alternatives, and consequences.

## Pitfalls

- No ADR without the losing alternatives.
- File it the same day; archaeology is not documentation.
