---
name: go-language-notes
description: "Read and fix Go code: modules, error returns, goroutines, vet and gofmt."
metadata:
  excalibur:
    group: languages
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Go language notes
      name_es: Notas del lenguaje Go
      desc_en: "Read and fix Go code: modules, error returns, goroutines, vet and gofmt."
      desc_es: "Leer y corregir Go: módulos, errores, goroutines, vet y gofmt."
---
# Go language notes

Use this skill on Go code. The toolchain is opinionated: gofmt, vet and explicit error returns settle most style and bug debates.

## When to use

- Reading or fixing Go packages, tests or tools.
- Goroutine leaks, data races or ignored errors.
- Adding dependencies or restructuring modules.

## Procedure

1. Keep the loop tight: `gofmt -l .` (must print nothing), `go vet ./...`, `go test ./...` (see `go-modules-toolchain`).
2. Handle every error return explicitly; wrap with context: `fmt.Errorf("load config: %w", err)`; never `_ = f.Close()` on writes that matter.
3. Share by communicating where it clarifies, but plain mutexes beat clever channels for simple state; run races with `go test -race`.
4. Read unfamiliar code via `go doc <pkg>.<Sym>` and the test files (`*_test.go` show intended use).
5. Timeouts and cancellation travel via `context.Context` as the first parameter; respect `ctx.Done()` in loops and network code.

## Pitfalls

- Goroutines without an exit path leak; every `go` statement needs a done/quit story.
- Loop-variable capture is fixed in Go 1.22+, but older code still passes copies explicitly; check `go.mod`'s version.
- Interface nil vs typed-nil: a non-nil interface holding a nil pointer is not `== nil`.

## Verification

- `gofmt -l .` empty, `go vet ./...` clean, `go test -race ./...` green.
