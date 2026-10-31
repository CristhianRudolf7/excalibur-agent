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
ARCHS="amd64 arm64"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out) OUT="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --arch) ARCHS="$2"; shift 2 ;;
    -h|--help) echo "Usage: build.sh [--out DIR] [--version TAG] [--arch amd64|arm64]"; exit 0 ;;
    *) echo "build.sh: unknown flag $1" >&2; exit 1 ;;
esac
done

if ! command -v musl-gcc >/dev/null 2>&1; then
  echo "build.sh: musl-gcc not found; install musl-tools first" >&2
  exit 1
fi

mkdir -p "$OUT"
export CGO_ENABLED=1
export CC=musl-gcc

# Cross-arch static builds need a matching assembler: musl-gcc on an
# x86_64 host assembles amd64 only. Build one arch locally with
# --arch and the other on its native host; Harbor trials run the
# arch of their task image.
for ARCH in $ARCHS; do
  echo "building linux/$ARCH ..."
  GOOS=linux GOARCH="$ARCH" go build \
    -tags 'sqlite_fts5' \
    -ldflags "-linkmode external -extldflags '-static' -X main.version=$VERSION" \
    -o "$OUT/excalibur-linux-$ARCH" ./cmd/excalibur
  sha256sum "$OUT/excalibur-linux-$ARCH" | tee "$OUT/excalibur-linux-$ARCH.sha256"
done

echo "binaries in $OUT"
