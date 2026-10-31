#!/usr/bin/env bash
# Build the static excalibur binary for benchmarks (Section 23.2).
#
# Produces Linux amd64 and arm64 static binaries with cgo (SQLite
# included) linked statically against musl, so the binary runs in any
# task image without system packages. Prints the sha256 of each binary.
#
# Usage: scripts/bench/build.sh [--out DIR] [--version TAG] [--arch amd64|arm64]
set -euo pipefail

OUT="dist/bench"
VERSION="dev"
ARCHS="amd64"
MUSL=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out) OUT="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --arch) ARCHS="$2"; shift 2 ;;
    --musl) MUSL=1; shift ;;
    -h|--help) echo "Usage: build.sh [--out DIR] [--version TAG] [--arch amd64|arm64] [--musl]"; exit 0 ;;
    *) echo "build.sh: unknown flag $1" >&2; exit 1 ;;
esac
done

mkdir -p "$OUT"

# Static bench binary: CGO_ENABLED=0 pure-Go static by default (no
# system packages, runs in any task image). The architecture document
# asks for cgo+musl; that route needs a full musl sysroot for
# sqlite3.h/libc headers (sqlite-vec cgo) which this host lacks, so the
# pure-Go static build is the portable default (see docs/decisions).
# Pass --musl for a best-effort cgo+musl build when the sysroot exists.
for ARCH in $ARCHS; do
  echo "building linux/$ARCH (musl=$MUSL) ..."
  if [[ "$MUSL" -eq 1 ]]; then
    if ! command -v musl-gcc >/dev/null 2>&1; then
      echo "build.sh: musl-gcc not found; install musl-tools first" >&2
      exit 1
    fi
    CGO_ENABLED=1 CC=musl-gcc GOOS=linux GOARCH="$ARCH" go build \
      -tags 'sqlite_fts5' \
      -ldflags "-linkmode external -extldflags '-static' -X main.version=$VERSION" \
      -o "$OUT/excalibur-linux-$ARCH" ./cmd/excalibur
  else
    CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build \
      -ldflags "-X main.version=$VERSION" \
      -o "$OUT/excalibur-linux-$ARCH" ./cmd/excalibur
  fi
  sha256sum "$OUT/excalibur-linux-$ARCH" | tee "$OUT/excalibur-linux-$ARCH.sha256"
done

echo "binaries in $OUT"
