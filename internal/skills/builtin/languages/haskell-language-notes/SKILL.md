---
name: haskell-language-notes
description: "Read and fix Haskell code: GHC errors, purity, common monads and cabal or stack builds."
metadata:
  excalibur:
    group: languages
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Haskell language notes
      name_es: Notas del lenguaje Haskell
      desc_en: "Read and fix Haskell code: GHC errors, purity, common monads and cabal or stack builds."
      desc_es: "Leer y corregir Haskell: errores GHC, pureza y builds cabal/stack."
---
# Haskell language notes

Use this skill on Haskell code. Types are the design: follow GHC's errors, keep IO at the edges, and let the build tool manage packages.

## When to use

- Reading or fixing Haskell modules and type errors.
- Choosing `Maybe`/`Either`/`IO` handling or debugging laziness issues.
- Building with cabal or stack.

## Procedure

1. Build with the project tool: `cabal build` / `stack build`, test with `cabal test` / `stack test`; match the resolver or `cabal.project` freeze file.
2. Read GHC errors inside-out: the "couldn't match expected vs actual" pair names the mismatch; add type holes (`_`) to ask GHC for the expected type.
3. Push effects outward: pure functions in the core, `IO` only at `main` and boundaries; represent failure with `Maybe`/`Either`, not `error` except for impossible cases.
4. Debug laziness with `print`/`Debug.Trace` sparingly; space leaks usually mean an accumulating thunk — force with `seq`/`deepseq` or strict fields (`!`).
5. Format with `ormolu`/`fourmolu` when configured; enable `-Wall` and fix all warnings.

## Pitfalls

- Partial functions (`head`, `!!`, `fromJust`) throw at runtime; total pattern matches are the safe default.
- String vs Text vs ByteString: pick `Text` for text and `ByteString` for binary; converting repeatedly is slow.
- Floating laziness plus `IORef`/`MVar` misuse deadlocks; prefer `STM` or plain data flow first.

## Verification

- `-Wall`-clean build, full test suite green, and no new partial-function uses in changed code.
