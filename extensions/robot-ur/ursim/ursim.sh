#!/usr/bin/env bash
set -euo pipefail

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
name=excalibur-ursim
image=${EXCALIBUR_URSIM_IMAGE:-universalrobots/ursim_e-series:5.25.0@sha256:e4e485bbf01ac4282ab1c037a4bbf5cf53d24759dd27538dd482d38dadd49d45}
label=excalibur.robot.ursim

# Distinguish an absent container from an unavailable Docker daemon.
docker info --format '{{.ServerVersion}}' >/dev/null

owned() {
    local owner
    owner=$(docker inspect --format '{{index .Config.Labels "excalibur.robot.ursim"}}' "$name" 2>/dev/null) || return 1
    if [[ "$owner" != true ]]; then
        echo "Refusing to manage unrelated container $name" >&2
        exit 1
    fi
}

case "${1:-}" in
    up)
        if owned; then
            docker start "$name" >/dev/null
        else
            docker run --detach --name "$name" --label "$label=true" \
                --memory 2g --cpus 2 --env ROBOT_MODEL=UR10 \
                --publish 127.0.0.1:29999:29999 \
                --publish 127.0.0.1:30001:30001 \
                --publish 127.0.0.1:30002:30002 \
                --publish 127.0.0.1:30003:30003 \
                --publish 127.0.0.1:30004:30004 \
                --publish 127.0.0.1:5900:5900 \
                --publish 127.0.0.1:6080:6080 "$image" >/dev/null
        fi
        if ! python3 "$here/dashboard.py" wait; then
            docker logs --tail 60 "$name" >&2
            exit 1
        fi
        echo 'UR10e simulator ready; noVNC: http://127.0.0.1:6080/vnc.html?autoconnect=true'
        ;;
    down)
        if owned; then
            docker stop --time 15 "$name" >/dev/null
        fi
        echo 'URSim stopped (container retained; no automatic restart).'
        ;;
    status)
        if owned; then
            docker inspect --format '{{.Name}}: {{.State.Status}}; image={{.Config.Image}}' "$name"
            docker port "$name"
        else
            echo 'URSim not created.'
        fi
        ;;
    *)
        echo "Usage: $0 up|down|status" >&2
        exit 2
        ;;
esac
