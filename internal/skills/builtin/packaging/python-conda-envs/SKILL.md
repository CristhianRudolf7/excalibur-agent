---
name: python-conda-envs
description: "Manage Python environments with conda or mamba, including offline and prefix installs."
metadata:
  excalibur:
    group: packaging
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Conda environments
      name_es: Entornos conda
      desc_en: "Manage Python environments with conda or mamba, including offline and prefix installs."
      desc_es: "Entornos Python con conda o mamba, incluso sin red."
---
# Conda environments

Use this skill when the project ships `environment.yml` or needs non-Python binaries (CUDA, MKL, GDAL) that pip cannot provide.

## When to use

- An `environment.yml` or conda-only dependency (e.g. `cudatoolkit`, `gdal`) is present.
- pip installs fail compiling native extensions.
- Reproducing a scientific or ML environment exactly.

## Procedure

1. Prefer `mamba` (faster solver, same commands) when installed; otherwise `conda`.
2. Create from file: `conda env create -f environment.yml -p ./conda-env` (prefix keeps it inside the project) or `conda create -n myenv python=3.11 <pkgs>`.
3. Activate with `conda activate ./conda-env` (prefix) or `conda activate myenv`; verify `which python`.
4. Export a lock: `conda env export --no-builds > environment.lock.yml` after a working solve.
5. Offline: `conda list --explicit > spec.txt` on a connected machine, then `conda create --offline -p ./conda-env --file spec.txt` with the package cache copied over.

## Pitfalls

- The base environment is not a project environment; never install project packages into it.
- Mixing pip and conda in one env works only if pip runs after the conda solve and versions are then frozen.
- `conda activate` needs shell hooks; in scripts use `source "$(conda info --base)/etc/profile.d/conda.sh"` first.

## Verification

- `conda env list` shows the env, `python -c 'import <keydep>'` succeeds, and the project smoke test passes.
