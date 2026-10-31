#!/usr/bin/env bash
# Strict-mode script starter for `shell-scripting-robust`.
# Copy this file, rename, and fill in main().
set -euo pipefail
IFS=$'\n\t'

log() { printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }

usage() {
  cat <<EOF
Usage: $(basename "$0") [--out DIR] [--jobs N] INPUT...
EOF
  exit 0
}

OUT=./out
JOBS="$(nproc)"
ARGS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage ;;
    --out) OUT="${2:?--out needs a value}"; shift 2 ;;
    --jobs) JOBS="${2:?--jobs needs a value}"; shift 2 ;;
    --) shift; ARGS+=("$@"); break ;;
    -*) die "unknown flag: $1" ;;
    *) ARGS+=("$1"); shift ;;
  esac
done
[[ ${#ARGS[@]} -gt 0 ]] || die "need at least one INPUT"

TMPDIR_WORK=$(mktemp -d)
trap 'rm -rf "$TMPDIR_WORK"' EXIT
mkdir -p "$OUT"

main() {
  local input
  for input in "${ARGS[@]}"; do
    [[ -r "$input" ]] || die "cannot read: $input"
    log "processing $input (jobs=$JOBS)"
    # ... work here, writing results under "$OUT" ...
  done
  log "done: results in $OUT"
}

main "$@"
