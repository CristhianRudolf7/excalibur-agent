---
name: go-modules-toolchain
description: "Build, test and pin Go projects with modules, including offline module caches."
metadata:
  excalibur:
    group: packaging
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Go modules and toolchain
      name_es: Módulos y toolchain de Go
      desc_en: "Build, test and pin Go projects with modules, including offline module caches."
      desc_es: "Compilar, probar y fijar proyectos Go con módulos, incluso sin red."
---
# Go modules and toolchain

Use this skill for any Go checkout: verify with vet, keep `go.mod`/`go.sum` consistent, and vendor or cache modules for offline builds.

## When to use

- Building, testing or vetting a Go module.
- `go: missing go.sum entry` or toolchain download errors.
- No network: use the module cache or a vendor directory.

## Procedure

1. Check `go version`; honor `toolchain` directives in `go.mod` (`GOTOOLCHAIN=local` only to debug version issues).
2. Standard loop: `go vet ./...`, `gofmt -l .` (empty output), `go test ./...`, then `go build ./...`.
3. Keep sums consistent: `go mod tidy` after changing imports; commit both `go.mod` and `go.sum`.
4. Offline: `go mod download` on a connected machine and copy `$(go env GOMODCACHE)`, or `go mod vendor` and build with `-mod=vendor`. Set `GOFLAGS=-mod=mod` only deliberately, and `GOPROXY=off` to prove no network is touched.
5. Cross-compile with `GOOS=darwin go build ./...` / `GOOS=windows go build ./...` (see `cross-compilation`).

## Pitfalls

- Editing `go.mod` by hand without `go mod tidy` leaves phantom requirements; tidy and re-test.
- `GOPATH` mode is legacy; every project here uses modules (`GO111MODULE` defaults to on).
- cgo needs gcc and headers; pure-Go builds stay portable (see `gcc-clang-toolchain`).

## Verification

- `go vet ./...`, `gofmt -l .` and `go test ./...` pass; `GOPROXY=off go build ./...` succeeds for offline claims.
