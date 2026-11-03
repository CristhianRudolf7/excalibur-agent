# Local UR10e simulator

Run from the repository root (Docker Engine, Bash, and Python 3 required):

```sh
extensions/robot-ur/ursim/ursim.sh up
extensions/robot-ur/ursim/ursim.sh status
python3 extensions/robot-ur/ursim/dashboard.py status
python3 extensions/robot-ur/ursim/dashboard.py ready
extensions/robot-ur/ursim/ursim.sh down
```

`up` starts the official `universalrobots/ursim_e-series:5.25.0` image,
pinned to digest
`sha256:e4e485bbf01ac4282ab1c037a4bbf5cf53d24759dd27538dd482d38dadd49d45`.
The image selects the **UR10e** with `ROBOT_MODEL=UR10` (the e-Series image
does not use `UR10e` as the environment value). See the
[official image documentation](https://hub.docker.com/r/universalrobots/ursim_e-series).

The container is named `excalibur-ursim`, has a 2 GiB memory / 2 CPU limit,
and has no restart policy. All published ports use `127.0.0.1`:

| Host / container TCP port | Interface |
| --- | --- |
| 29999 | Dashboard |
| 30001 | Primary URScript |
| 30002 | Secondary URScript |
| 30003 | Real-time |
| 30004 | RTDE |
| 5900 | VNC |
| 6080 | noVNC web UI |

Open <http://127.0.0.1:6080/vnc.html?autoconnect=true>. From another machine,
forward only the web port with `ssh -L 6080:127.0.0.1:6080 <linux-host>` and
open that local URL. Do not change the bindings to `0.0.0.0`.

`up` waits up to 120 seconds for Dashboard, the firmware version, an initialized
controller (POWER_OFF/IDLE/RUNNING), and NORMAL safety. It does not power on or
move the robot. A Dashboard greeting alone can arrive while the controller is
still DISCONNECTED with UNDEFINED safety during boot. `down` stops and retains the container,
including taught programs and simulator settings. Subsequent `up` reuses it.
An unrelated container with the same name is rejected. To reset **only this
simulator**, stop it and explicitly run `docker rm excalibur-ursim`, then `up`.
An optional `EXCALIBUR_URSIM_IMAGE` selects another official tagged image when
creating a fresh container; it does not change an existing container's image.

## Remote mode and services

On first boot, the simulator may show a confirmation of its generated safety
configuration. Review and confirm it **only in the simulator**. Then:

1. Open the noVNC pendant's hamburger menu → Settings → Security → Services.
   Enable Dashboard, Primary, Secondary, Real-Time Client Interface, and RTDE
   where applicable. The tested image's mock security backend exposes these
   services already; port readiness and actual RTDE reads are checked below.
2. Settings → System → Remote Control → Enable. Exit Settings and select
   Remote in the top-right control mode selector. Enabling the feature alone
   does not select Remote. Reconnect clients after changing modes.
3. Check `python3 extensions/robot-ur/ursim/dashboard.py status`. It prints
   firmware, `is in remote control`, `robotmode`, and `safetystatus`.
   Recheck Remote after restarting; select it again if the mock backend reset it.

Unlike a real e-Series arm, this simulator's mock backend may permit Dashboard
power and script commands even when `is in remote control` returns `false`.
That behavior does not validate the real arm's Remote Control prerequisite.
The physical setup must follow the
[official controller setup instructions](https://github.com/UniversalRobots/Universal_Robots_Client_Library/blob/master/doc/setup/robot_setup.rst).

## Power on and release brakes without the pendant

`dashboard.py ready` opens TCP 29999, consumes the server greeting, sends
`power on\n`, polls `robotmode\n` until `IDLE`, sends `brake release\n`, and
waits for `RUNNING`. If already IDLE/RUNNING it skips completed transitions.
It does not unlock protective stops or reset safety faults. `status` should
report `Safetystatus: NORMAL`; investigate any other mode before proceeding.

Interactive raw alternative, against **URSim only**:

```sh
nc 127.0.0.1 29999
# Wait for: Connected: Universal Robots Dashboard Server
# Type each command, followed by Enter; wait for each response:
power on
robotmode
# Repeat robotmode until IDLE, then:
brake release
robotmode
# Repeat robotmode until RUNNING.
safetystatus
stop
```

Dashboard is newline-delimited, and its acknowledgement of `power on` is not
proof that the transition finished. These commands are documented in the
[official Dashboard reference](https://www.universal-robots.com/articles/ur/dashboard-server-e-series-port-29999/).

## Validation and diagnostics

Use [the e2e harness](../e2e/README.md) for measured motion, DO1, and STOP checks.
It verifies the named Docker container, model, running state, official image,
and every loopback binding before sending commands. It accepts no physical
robot host parameter.

```sh
docker logs --tail 60 excalibur-ursim
docker exec excalibur-ursim tail -n 60 /ursim/URControl.log
docker exec excalibur-ursim tail -n 60 /ursim/polyscope.log
docker port excalibur-ursim
```

An initial boot may briefly log missing generated safety settings while
PolyScope initializes; only proceed once actual state is NORMAL/RUNNING.
The probe uses `ur_rtde==1.6.5` and 500 Hz. During bring-up, 1.6.3 failed
control synchronization; the 1.6.5 / 500 Hz combination passed. The vendor
documents excess RTDE packets below 500 Hz in this simulator release in its
[PolyScope 5.25 release notes](https://www.universal-robots.com/articles/ur/release-notes/release-note-software-version-525x/).

Stop URSim when finished, even when the bridge tests are skipped:

```sh
extensions/robot-ur/ursim/ursim.sh down
extensions/robot-ur/ursim/ursim.sh status
```
