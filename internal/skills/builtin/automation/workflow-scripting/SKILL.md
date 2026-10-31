---
name: workflow-scripting
description: Write the script steps of a workflow in any language, including vision models, with typed
  inputs and outputs.
metadata:
  excalibur:
    group: automation
    version: 1.0.0
    platforms:
    - linux
    tools:
    - workflow
    - exec
    display:
      name_en: Workflow scripting
      name_es: Scripts de workflow
      desc_en: Write the script steps of a workflow in any language, including vision models, with typed
        inputs and outputs.
      desc_es: Escribir pasos de script con entradas y salidas tipadas.
---
# Workflow scripting

Use this skill for the code inside workflow nodes. Typed inputs and outputs plus hermetic steps keep scripts composable and testable outside the workflow.

## When to use

- Implementing workflow step logic.
- Steps need vision models or other languages.
- Debugging a failing workflow step.

## Procedure

1. Fix the step contract: typed inputs, typed outputs, and failure modes.
2. Keep steps hermetic: no hidden state, no ambient credentials beyond declared inputs.
3. Test the step standalone with fixtures before wiring it in.
4. Handle the declared failures with retries or compensations, not silence.
5. Log structured progress so the workflow run is debuggable.

## Output

Tested hermetic steps with typed contracts.

## Pitfalls

- Hidden state in steps breaks resume and retry.
- Undeclared failure modes become stuck instances.
