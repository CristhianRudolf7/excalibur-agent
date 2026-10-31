---
name: python-language-notes
description: "Read, run and fix Python code: versions, imports, virtualenvs, pytest and common errors."
metadata:
  excalibur:
    group: languages
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Python language notes
      name_es: Notas del lenguaje Python
      desc_en: "Read, run and fix Python code: versions, imports, virtualenvs, pytest and common errors."
      desc_es: "Leer, ejecutar y corregir Python: versiones, imports, venvs y pytest."
---
# Python language notes

Use this skill on any Python codebase. Pin the interpreter, isolate the environment, and let pytest plus tracebacks guide the fix.

## When to use

- Running or fixing Python scripts, tests or packages.
- `ModuleNotFoundError`, `ImportError` or version-dependent syntax errors.
- Choosing between stdlib and third-party approaches.

## Procedure

1. Confirm interpreter and env: `python3 --version`, `which python`, active `.venv` (see `python-pip-uv-venv`).
2. Run module-style from the project root: `python -m pytest tests/ -x -q` or `python -m package.module`, never bare-script-path from random directories (imports resolve from `sys.path[0]`).
3. Read the traceback bottom-up: last line is the error, the frames above show the call path; reproduce with the smallest snippet before editing.
4. Lint and format: `python -m compileall -q .` for syntax, `ruff`/`flake8` when configured, `black`/`ruff format` for style.
5. Version traps: `python2` vs `python3` print/division, f-strings need 3.6+, `match` needs 3.10+, `tomllib` needs 3.11+.

## Pitfalls

- Running `pytest` from a subdirectory silently changes import roots; run from the project root with config (`pytest.ini`/`pyproject.toml`).
- Mutable default arguments, late-binding closures in loops, and `except:` bare clauses are the classic trio.
- Circular imports surface as half-initialized modules; move the import into the function or restructure.

## Verification

- `python -m pytest` green on the touched area plus `python -m compileall -q` clean; the reproducer now exits 0.
