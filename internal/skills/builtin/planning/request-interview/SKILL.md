---
name: request-interview
description: Interview the user to clarify requirements, doubts, ambiguities and missing data, grouping
  questions with their default option and recording assumptions under rule 6.4.
metadata:
  excalibur:
    group: planning
    version: 1.0.0
    platforms:
    - linux
    tools:
    - ask_user
    - task_db
    display:
      name_en: Request interview
      name_es: Entrevista de requisitos
      desc_en: Interview the user to clarify requirements, doubts, ambiguities and missing data, grouping
        questions with their default option and recording assumptions under rule 6.4.
      desc_es: Entrevistar al usuario para aclarar requisitos, dudas, ambigüedades y datos faltantes.
---
# Request interview

Use this skill when a request is ambiguous, incomplete, or carries irreversible consequences. A short structured interview up front is cheaper than rework later: it pins down what the user wants, what they assume, and what may be decided without asking.

## When to use

- The request admits more than one reasonable interpretation.
- The task is irreversible or expensive (sends messages, spends money, deletes data).
- Mandatory data is missing and no safe default exists.

## Procedure

1. Read the request file and list every ambiguity, missing datum, and candidate assumption.
2. Group related questions into at most 3 messages; each question states its recommended default and why.
3. Ask via ask_user and record answers literally in the request file.
4. Record every applied default as an assumption with its reason, per rule 6.4.
5. Convert clarified points into requirements R# with task_db before planning.

## Output

An updated request file: resolved questions with literal answers, assumptions for applied defaults, and new R# requirements ready for spec-writing.

## Pitfalls

- Never ask more than 3 questions per message; group and prioritize.
- Never present the user's agreement as validation of facts; answers are input, not evidence.
