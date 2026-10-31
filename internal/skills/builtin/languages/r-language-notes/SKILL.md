---
name: r-language-notes
description: "Run and fix R scripts and packages: vectors, data frames, installs and reproducible runs."
metadata:
  excalibur:
    group: languages
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: R language notes
      name_es: Notas del lenguaje R
      desc_en: "Run and fix R scripts and packages: vectors, data frames, installs and reproducible runs."
      desc_es: "Ejecutar y corregir R: vectores, data frames e instalaciones."
---
# R language notes

Use this skill on R scripts and packages. Think in vectors, guard the library path, and run non-interactively for reproducibility.

## When to use

- Running or fixing `.R` scripts, Rmarkdown renders or package code.
- `there is no package called ...`, factor/NA surprises, or plotting failures headless.
- Making an analysis rerunnable on another machine.

## Procedure

1. Run scripts non-interactively: `Rscript script.R`; render docs with `Rscript -e 'rmarkdown::render("doc.Rmd")'`; check packages with `R CMD check` when developing one.
2. Fix installs explicitly: `.libPaths()` shows library dirs; install with `install.packages("pkg", repos="https://cloud.r-project.org")` into a project library (see `python-conda-envs` for conda-managed R).
3. Vectorize: prefer vector ops and `apply` family over loops; remember recycling rules and that `NA` propagates through comparisons (`x == NA` is `NA`, use `is.na(x)`).
4. Treat factors deliberately: convert with explicit levels (`factor(x, levels=...)`); strings become factors only when asked in modern R.
5. Snapshot the session: `sessionInfo()` versions in the report; `set.seed()` before any randomness.

## Pitfalls

- Installing into the system library without permission fails; use a user or project library and `.libPaths()`.
- Partial matching of argument names (`$` on data frames, function args) silently binds the wrong thing; use full names.
- Headless plots need a device: `png()/pdf()` around plot code or `ggsave()`; interactive devices do not exist in batch runs.

## Verification

- `Rscript` run exits 0 twice with identical outputs given the same seed; `sessionInfo()` recorded alongside results.
