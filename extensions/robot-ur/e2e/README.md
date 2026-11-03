# UR10e simulator checks

These checks target only the labeled `excalibur-ursim` Docker container on
`127.0.0.1`. They never accept a physical robot IP. Direct RTDE checks and
HTTP bridge checks produce separate evidence. No paid APIs or models are used.

## Install and run

From the repository root:

```sh
extensions/robot-ur/ursim/ursim.sh up
# On first boot, enable and select Remote Control in the noVNC pendant.
# See ../ursim/README.md; the harness rejects Local mode.
python3 -m venv extensions/robot-ur/e2e/.venv
extensions/robot-ur/e2e/.venv/bin/python -m pip install -r extensions/robot-ur/e2e/requirements.txt
extensions/robot-ur/e2e/run.sh direct
EXCALIBUR_URSIM_TESTS=1 extensions/robot-ur/e2e/.venv/bin/python -m pytest extensions/robot-ur/e2e -m ursim -v -rs
extensions/robot-ur/ursim/ursim.sh down
extensions/robot-ur/ursim/ursim.sh status
```

The ignored venv is temporary local test tooling, separate from RB1's runtime
venv. `EXCALIBUR_ROBOT_TEST_PYTHON` can select an already provisioned Python.
No dependency or package is installed globally.

`run.sh direct` checks Dashboard power-on/brake-release, NORMAL/RUNNING state,
RTDE telemetry, a joint home at 0.3 rad/s, a base-X move of 0.02 m at 0.05 m/s,
DO1 close/open, and `stopL` plus Dashboard `stop` during a 0.02 m/s move. It
checks the measured pose delta (2 mm tolerance), output bits, stationary pose
after STOP, and that the full move target was not reached. Cleanup restores
the original DO1 and stops the control script. The direct simulator joint
home is `[-pi/2,-pi/2,-pi/2,-pi/2,pi/2,0]`; **never reuse it on hardware**.

The script has a 180-second process deadline plus a 10-second interrupt grace
period. If a native RTDE call hangs past that deadline, run the independent
`python3 extensions/robot-ur/ursim/dashboard.py stop` and stop the container.
The simulator cannot validate physical gripper sensors or collision safety.

## Bridge integration after RB1 merges

`contract_client.py` is a standard-library reference client for **Contract 1**.
It imports no bridge implementation. HTTP failures preserve status and error
JSON; `ok:false` over HTTP success fails the test. The bridge URL is restricted
to `http://127.0.0.1:<port>`; proxy environment settings are ignored.

With an already running bridge:

```sh
extensions/robot-ur/e2e/run.sh bridge
```

To let the harness start and clean up the bridge, set
`EXCALIBUR_ROBOT_BRIDGE_COMMAND` to its documented foreground launch command,
then run the same script. It starts that command in a new process group,
waits up to 30 seconds for `/health`, and terminates only that process group
on exit. `EXCALIBUR_ROBOT_BRIDGE_PORT` defaults to `7811`. Use a dedicated
simulator bridge with default DO1 polarity and a workspace containing the
test home's TCP (approximately `[-0.17415,-0.6914,0.67685]`) and its next
0.12 m in base X. Do not widen the physical robot's config for these tests.

**TODO(RB1 merge):** RB1's bridge directory and launch entry point were absent
in this branch during validation. Set the foreground command after reading
the merged bridge documentation, and rerun the HTTP test. Do not substitute
a fake bridge and report it as live bridge validation.

The HTTP check performs:

1. `/health`, `/connect {host:"127.0.0.1"}`, `/state` and all required state fields.
2. `/power {action:"on"}` and `{action:"brake_release"}`, polling the robot mode.
3. `/move {kind:"joints", joints:[...], approved:true}` to the simulator home.
4. `/move {kind:"relative", delta:{dx:0.02}, frame:"base", approved:true}`;
   verify measured TCP delta.
5. `/gripper` close/open; verify `digital_out[1]` rather than assuming the
   pneumatic sensors change in URSim. Approval is included for actuation.
6. An approved pose at `[100,100,100,...]`; require HTTP 4xx/5xx with
   `{ok:false,error:...}` identifying the workspace violation and no pose
   change. This runs while the robot is RUNNING, before STOP, so an unrelated
   stopped-program error cannot count as workspace enforcement.
7. Concurrent `/move` and `/stop`, without approval on STOP. Observe motion
   first, require STOP within 3 seconds while `/move` is pending, verify
   stable pose before the target, then STOP and `/disconnect` in cleanup.

Although the enumerated `/move` body in Contract 1 omits `approved`, the
common safety requirement explicitly requires it. The reference client
includes `approved:true` on motion requests. It does not bypass the bridge's
workspace, state, approval, or speed checks. If RB1's default workspace
excludes the simulator home, the test fails explicitly and needs a dedicated
simulator config, not a weakening of the safety check.

## Pytest skip and failure semantics

Both live tests carry `@pytest.mark.ursim` (through the module mark). They skip
unless `EXCALIBUR_URSIM_TESTS=1`, and skip when the container is absent/stopped.
With the container running, malformed container bindings, wrong model, bad
Dashboard state (including Local mode), broken HTTP responses, and motion assertions **fail**.
Only a refused bridge connection skips the bridge test; the direct test still
runs. `run.sh bridge` instead fails when the bridge is absent, making it
suitable for a required merge check. Pytest does not start a bridge itself.

```sh
# Safe default: offline client checks run; both live tests skip.
extensions/robot-ur/e2e/.venv/bin/python -m pytest extensions/robot-ur/e2e -v -rs
# Run only offline URL/error-handling checks.
extensions/robot-ur/e2e/.venv/bin/python -m pytest extensions/robot-ur/e2e -m 'not ursim' -v
# Explicitly required bridge run after integration (no fallback to direct).
extensions/robot-ur/e2e/run.sh bridge
```

Do not run these motion tests concurrently or with pytest-xdist: they share
one simulator and its RTDE input registers. See [validation.md](validation.md)
for actual commands, results, and outstanding merge checks.
