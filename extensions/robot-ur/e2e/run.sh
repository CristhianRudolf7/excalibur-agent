#!/usr/bin/env bash
set -euo pipefail

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
python=${EXCALIBUR_ROBOT_TEST_PYTHON:-$here/.venv/bin/python}
mode=${1:-bridge}
port=${EXCALIBUR_ROBOT_BRIDGE_PORT:-7811}
bridge_pid=
simulator_verified=

if [[ "$mode" != bridge && "$mode" != direct ]]; then
    echo "Usage: $0 bridge|direct" >&2
    exit 2
fi
if [[ ! -x "$python" ]]; then
    echo "Create $here/.venv and install $here/requirements.txt first (see README.md)." >&2
    exit 2
fi
if [[ ! "$port" =~ ^[0-9]+$ ]] || ((port < 1 || port > 65535)); then
    echo 'EXCALIBUR_ROBOT_BRIDGE_PORT must be an integer from 1 to 65535.' >&2
    exit 2
fi

cleanup() {
    if [[ -n "$simulator_verified" ]]; then
        python3 "$here/../ursim/dashboard.py" stop >&2 || true
    fi
    if [[ -n "$bridge_pid" ]]; then
        kill -- -"$bridge_pid" 2>/dev/null || true
        if ! timeout 10s tail --pid="$bridge_pid" -f /dev/null; then
            kill -KILL -- -"$bridge_pid" 2>/dev/null || true
        fi
        wait "$bridge_pid" 2>/dev/null || true
    fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Refuse an absent/mismatched simulator before launching even a local bridge.
"$python" -c 'import sys; sys.path.insert(0, sys.argv[1]); from check import require_simulator; require_simulator()' "$here"
simulator_verified=1

if [[ "$mode" == bridge && -n "${EXCALIBUR_ROBOT_BRIDGE_COMMAND:-}" ]]; then
    "$python" - "$port" <<'PY'
import socket
import sys
try:
    connection = socket.create_connection(("127.0.0.1", int(sys.argv[1])), timeout=1)
except ConnectionRefusedError:
    pass
else:
    connection.close()
    raise SystemExit("Bridge port is occupied; omit EXCALIBUR_ROBOT_BRIDGE_COMMAND to use an existing bridge")
PY
    # TODO(RB1 merge): set this to RB1's documented bridge entry point. No private imports.
    setsid bash -c "$EXCALIBUR_ROBOT_BRIDGE_COMMAND" &
    bridge_pid=$!
    "$python" - "$here" "$port" <<'PY'
import sys
import time
sys.path.insert(0, sys.argv[1])
from contract_client import BridgeClient
client = BridgeClient(f"http://127.0.0.1:{sys.argv[2]}")
deadline = time.monotonic() + 30
while True:
    try:
        assert client.get("/health", timeout=1)["ok"] is True
        break
    except Exception:
        if time.monotonic() >= deadline:
            raise
        time.sleep(0.2)
PY
fi

if [[ "$mode" == bridge && ! -d "$here/../bridge" && -z "${EXCALIBUR_ROBOT_BRIDGE_COMMAND:-}" ]]; then
    echo 'TODO(RB1 merge): bridge source is absent; checking an already running Contract 1 service.' >&2
fi
timeout --signal=INT --kill-after=10s 180s \
    "$python" -u "$here/check.py" "$mode" --bridge-url "http://127.0.0.1:$port"
