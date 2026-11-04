#!/usr/bin/env bash
set -euo pipefail
export PATH="/usr/local/go/bin:$HOME/go/bin:$PATH"
repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
if [[ "$(uname -s)" == "Darwin" ]]; then
  export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
  state="$(mktemp -d "${TMPDIR:-/tmp}/excalibur-web-daemon.XXXXXX")"
else
  state="$(mktemp -d -t excalibur-web-daemon.XXXXXX)"
fi
child=""
cleanup() {
  if [[ -n "$child" ]]; then kill "$child" 2>/dev/null || true; wait "$child" 2>/dev/null || true; fi
  rm -rf "$state"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
nice -n 19 go -C "$repo" build -p 2 -o "$state/excalibur" ./cmd/excalibur
"$state/excalibur" daemon start --listen "${EXCALIBUR_DEV_LISTEN:-127.0.0.1:7787}" --data-dir "$state/data" &
child=$!
wait "$child"
