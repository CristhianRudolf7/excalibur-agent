#!/usr/bin/env bash
# Flags bare `go` statements launching goroutines outside internal/safego.
# Long-lived goroutines must go through safego.Go so a panic marks the
# owner as failed instead of killing the daemon (Sections 3.2, 5.3).
#
# Usage: scripts/check-no-bare-go.sh
# Exit 0 when clean, 1 listing offenders.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# Matches goroutine launches: `go func...` or `go name(...)`, at statement
# starts or after `{`, `}` or `;`. Full-line // comments are excluded;
# obvious launches inside block comments or strings are still the
# reviewer's job.
PATTERN='(^|[[:space:]{};])go[[:space:]]+(func[[:space:](]|[_a-zA-Z][_a-zA-Z0-9.]*[[:space:]]*\()'

offenders=0
while IFS= read -r file; do
  # The single sanctioned launch site lives in safego.go itself.
  case "$file" in
    */internal/safego/safego.go) continue ;;
  esac
  hits="$(grep -vE '^[[:space:]]*//' "$file" | grep -nE "$PATTERN" || true)"
  if [ -n "$hits" ]; then
    echo "bare go statement in $file:"
    echo "$hits" | sed 's/^/  /'
    offenders=1
  fi
done < <(find . -path ./node_modules -prune -o -name '*.go' -print | grep -v '_test\.go' | sort)

if [ "$offenders" -ne 0 ]; then
  echo "FAIL: launch long-lived goroutines via safego.Go (internal/safego)."
  exit 1
fi
echo "OK: no bare go statements outside internal/safego."
