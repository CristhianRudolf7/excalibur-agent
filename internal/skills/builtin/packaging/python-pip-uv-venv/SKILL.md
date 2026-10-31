---
name: python-pip-uv-venv
description: "Manage Python environments and installs with venv, pip and uv, including offline wheelhouses."
metadata:
  excalibur:
    group: packaging
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Python environments with pip and uv
      name_es: Entornos Python con pip y uv
      desc_en: "Manage Python environments and installs with venv, pip and uv, including offline wheelhouses."
      desc_es: "Entornos e instalaciones Python con venv, pip y uv, incluso sin red."
---
# Python environments with pip and uv

Use this skill for any Python task: create an isolated environment first, install pinned requirements, and keep an offline path with a wheelhouse.

## When to use

- Running Python tests, scripts or tools in a project.
- `ModuleNotFoundError` or conflicting package versions.
- No network: install from a local wheel directory.

## Procedure

1. Create and activate: `python3 -m venv .venv && source .venv/bin/activate`; confirm with `which python` pointing inside `.venv`.
2. Prefer `uv` when available (`uv venv && uv pip install -r requirements.txt`); it is faster and resolves better. Otherwise `pip install -r requirements.txt`.
3. Pin what you install: `pip freeze > requirements.txt` after a working install; prefer `==` pins in the committed file.
4. Offline: on a networked machine run `pip download -r requirements.txt -d ./wheelhouse`, copy the directory, then `pip install --no-index --find-links ./wheelhouse -r requirements.txt`.
5. Never `sudo pip install`; never install into the system Python. One project, one `.venv`.

## Pitfalls

- Forgetting to activate installs into the wrong interpreter; always check `which python` and `pip --version` first.
- `pip install <pkg>` without a venv pollutes the system and breaks apt-managed Python files.
- Wheels are platform-specific; a wheelhouse built on another OS or arch will not install.

## Verification

- `python -c 'import sys; print(sys.prefix)'` points at `.venv` and `pip check` reports no broken requirements.
- The project's smoke test (pytest, script run) passes inside the activated environment.

## References

- `references/offline-wheelhouse.md` — building and using a wheelhouse step by step.
