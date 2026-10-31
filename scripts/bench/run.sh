#!/usr/bin/env bash
# Launch Harbor trials for Excalibur (Section 23.2, Appendix F).
#
# Wraps `harbor run` with the fixed comparison conditions: pinned
# dataset digest, no timeout/resource changes, -k 5 by default, results
# never uploaded unless the user passes --upload.
#
# Usage: scripts/bench/run.sh <tb21|tb40> <provider/model> [k] [--upload] [-- extra harbor args]
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "Usage: run.sh <tb21|tb40> <provider/model> [k] [--upload] [-- extra harbor args]" >&2
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

case "$DATASET" in
  tb21) SET="terminal-bench/terminal-bench-2-1" ;;
  tb40) SET="terminal-bench/terminal-bench@4.0.0" ;;
  *) echo "run.sh: dataset must be tb21 or tb40" >&2; exit 1 ;;
esac

AGENT_PATH="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../integrations/harbor" && pwd)/excalibur_agent.py"

UPLOAD_FLAGS=()
if [[ "$UPLOAD" -eq 1 ]]; then
  # Publish only on explicit request (Section 23.1).
  UPLOAD_FLAGS=(--upload --public)
fi

echo "dataset=$SET model=$MODEL k=$K agent=$AGENT_PATH"
exec harbor run \
  -d "$SET" \
  -a "$AGENT_PATH" \
  -m "$MODEL" \
  -k "$K" \
  "${UPLOAD_FLAGS[@]}" \
  "$@"
