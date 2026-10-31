#!/usr/bin/env bash
# Launch Harbor trials for Excalibur via the native bench command
# (Section 23.2, Appendix F).
#
# Thin wrapper: `excalibur bench tb21 run` owns the frozen conditions
# (pinned dataset digest, -k 5 default, no timeout/resource changes, no
# upload). This script exists for the documented `scripts/bench/run.sh
# tb21` entry point and forwards to the binary.
#
# Usage: scripts/bench/run.sh <tb21|tb40> <provider/model> [k] [--upload] [-- extra bench flags]
# Example: scripts/bench/run.sh tb21 openai/gpt-6-luna 1 -- --credentials-file ~/.config/excalibur/credentials/openai.json --tasks terminal-bench/write-compressor
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "Usage: run.sh <tb21|tb40> <provider/model> [k] [--upload] [-- extra bench flags]" >&2
  exit 1
fi

DATASET="$1"; shift
MODEL="$1"; shift
K=5
UPLOAD=0

if [[ $# -gt 0 && "$1" =~ ^[0-9]+$ ]]; then
  K="$1"; shift
fi
if [[ $# -gt 0 && "$1" == "--upload" ]]; then
  UPLOAD=1; shift
fi
if [[ $# -gt 0 && "$1" == "--" ]]; then
  shift
fi

PROVIDER="${MODEL%%/*}"
MODEL_ID="${MODEL#*/}"
if [[ "$PROVIDER" == "$MODEL" ]]; then
  PROVIDER="openai"
  MODEL_ID="$MODEL"
fi

UPLOAD_FLAG=()
if [[ "$UPLOAD" -eq 1 ]]; then
  UPLOAD_FLAG=(--upload)
fi

BIN="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.. && pwd)/dist/bench/excalibur-linux-amd64"
if [[ ! -x "$BIN" ]]; then
  echo "run.sh: building static binary first (scripts/bench/build.sh)" >&2
  "$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/scripts/bench/build.sh" --arch amd64
fi

exec go run ./cmd/excalibur bench "$DATASET" run \
  --provider "$PROVIDER" --model "$MODEL_ID" \
  --k "$K" "${UPLOAD_FLAG[@]}" \
  "$@"
