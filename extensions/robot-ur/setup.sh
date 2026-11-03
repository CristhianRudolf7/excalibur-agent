#!/usr/bin/env bash
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
venv="${EXCALIBUR_ROBOT_VENV:-${HOME}/.local/share/excalibur/robot-venv}"
python3 -m venv "$venv"
"$venv/bin/python" -m pip install --requirement "$root/requirements.txt"
bridge_dir="${EXCALIBUR_ROBOT_BRIDGE_DIR:-${HOME}/.local/share/excalibur/robot-bridge}"
mkdir -p "$bridge_dir"
for module in server controller dashboard safety transforms; do
  install -m 0644 "$root/bridge/$module.py" "$bridge_dir/$module.py"
done
printf 'Robot bridge: %s/server.py\n' "$bridge_dir"
printf 'Robot Python: %s/bin/python\n' "$venv"
