---
name: cobol-language-notes
description: "Read, compile and test COBOL programs with GnuCOBOL: divisions, PIC clauses and JCL-free runs."
metadata:
  excalibur:
    group: languages
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: COBOL language notes
      name_es: Notas del lenguaje COBOL
      desc_en: "Read, compile and test COBOL programs with GnuCOBOL: divisions, PIC clauses and JCL-free runs."
      desc_es: "Leer, compilar y probar COBOL con GnuCOBOL: divisiones y PIC."
---
# COBOL language notes

Use this skill when facing COBOL sources (maintenance, migration, verification). Read the divisions in order and compile with GnuCOBOL; the language is verbose but explicit.

## When to use

- Reading `.cbl`/`.cob` programs: what inputs, what files, what output.
- Compiling and running COBOL on Linux with `cobc`.
- Debugging fixed-format vs free-format and decimal arithmetic issues.

## Procedure

1. Read top-down: `IDENTIFICATION` (program id), `ENVIRONMENT` (files, system), `DATA` (all variables with `PIC` clauses), `PROCEDURE` (the logic). Data definitions answer most questions.
2. Respect `PIC` clauses: `9` digits, `X` alphanumeric, `V` implied decimal, `S` sign; `PIC 9(5)V99` is money-shaped, never float.
3. Compile with GnuCOBOL: `cobc -x -o prog prog.cbl` (free format needs `-free`); run `./prog`; test mode with `cobc -fsyntax-only` for a parse check.
4. Feed file-based programs their datasets first: check `SELECT`/`ASSIGN` in ENVIRONMENT and create matching sequential files.
5. Translate carefully: COBOL decimals are exact; verify every migrated calculation against the original with cent-level fixtures.

## Pitfalls

- Fixed-format columns (sequence, indicator, Area A/B) corrupt pasted code; confirm the format before editing.
- `PIC X` vs `PIC 9` moves and compares differently; alphanumeric-to-numeric moves need explicit handling.
- Paragraph fall-through (`PERFORM` ranges) executes the next paragraph unless control returns; trace the flow, do not assume block scope.

## Verification

- `cobc` compiles warning-free; the program runs on sample inputs and matches expected outputs field-for-field.
