# RB3 validation — 2026-11-03

Branch: `robot/rb3`, based on `a83dcbd`. Linux amd64, Docker Engine 29.1.3,
Python 3.12.3, pytest 8.3.5, ur_rtde 1.6.5. No physical robot, paid API,
browser camera, or production daemon was used.

## Simulator evidence

Image: `universalrobots/ursim_e-series:5.25.0`, digest
`sha256:e4e485bbf01ac4282ab1c037a4bbf5cf53d24759dd27538dd482d38dadd49d45`.
Dashboard firmware: `URSoftware 5.25.0.130258 (Dec 11 2025)`.
Environment: `ROBOT_MODEL=UR10` in the official e-Series image.
All seven published ports were verified as `127.0.0.1` bindings, all accepted
TCP connections, and `/vnc.html` returned HTTP 200 on port 6080.

The initial direct control attempt with ur_rtde 1.6.3 failed RTDE synchronization.
The final probe uses ur_rtde 1.6.5 at 500 Hz. Remote Control was enabled and
selected through the simulator pendant, and Dashboard confirmed `true` before
the final motion run. The simulator was explicitly powered off first so the
final probe exercised both power-on and brake-release transitions.

[Raw successful probe evidence](evidence/2026-11-03-direct.jsonl) records:

| Check | Observed result |
| --- | --- |
| Power-on / brake release | `Powering on`, `Brake releasing`, then RUNNING/NORMAL; Remote true |
| Joint home | All six joints reached `[-pi/2,-pi/2,-pi/2,-pi/2,pi/2,0]` |
| Home TCP | Approximately `[-0.17415,-0.6914,0.67685,0,pi,0]` |
| Relative move | Measured base-X delta `0.01999999999999974` m; other components unchanged within floating-point precision |
| Gripper output | DO1 close produced bitmask `2`; open produced `0` |
| STOP during asynchronous linear move | `stopL` and Dashboard `Stopped`; traveled approximately `0.000412` m toward a `0.10` m target, then remained stationary |
| Cleanup | Original DO1 restored; control script stopped |

The opt-in pytest run returned **1 passed, 1 skipped in 4.77 seconds**:
the direct test passed; the bridge test skipped because TCP 7811 refused the
connection. A required `run.sh bridge` invocation failed with connection
refused instead of silently falling back to a direct test.

Offline reference-client tests returned **12 passed**. They exercise URL
restrictions, successful health JSON, preserved HTTP 422 error JSON, rejection
of hidden `ok:false` successes, and rejection of non-object JSON. They are
client tests, not evidence of a working bridge.

Both live tests skip when opt-in is absent, and both also skip when URSim is
stopped even with opt-in set. The complete stopped-simulator pytest run
returned **12 passed, 2 skipped**. A running simulator in Local mode caused
the motion harness to fail before issuing power or move commands; state
remained POWER_OFF.

## Lifecycle checks

Repeated `up` reused the labeled container and kept all localhost bindings.
Repeated `down` succeeded. A cold restart exposed an early Dashboard greeting
while the controller was still DISCONNECTED / UNDEFINED safety; the launcher
now waits for an initialized robot mode and NORMAL safety as well. The fixed
cold-start check returned POWER_OFF / NORMAL.

This image's mock security backend reset the selected control mode to Local
on restart even though `RemoteControlEnabled=true` remained saved. Select
Remote again after each simulator restart before running the motion tests.
The guard rejects Local mode. The standalone simulator-only Dashboard helper
was also checked headlessly: POWER_OFF → IDLE → RUNNING, then `stop`.

Final container state: **exited**, retained for reuse, without a restart policy.

## Exact operating and test commands

All commands run from `/home/developer/projects/robot/rb3`:

```sh
docker pull universalrobots/ursim_e-series:5.25.0
extensions/robot-ur/ursim/ursim.sh up
extensions/robot-ur/ursim/ursim.sh status
python3 extensions/robot-ur/ursim/dashboard.py status
# In noVNC: confirm simulator safety settings, enable Remote Control, exit,
# select Remote, then confirm Dashboard reports true.
python3 -m venv extensions/robot-ur/e2e/.venv
extensions/robot-ur/e2e/.venv/bin/python -m pip install -r extensions/robot-ur/e2e/requirements.txt
extensions/robot-ur/e2e/run.sh direct
EXCALIBUR_URSIM_TESTS=1 timeout --signal=INT --kill-after=10s 180s extensions/robot-ur/e2e/.venv/bin/python -m pytest extensions/robot-ur/e2e -m ursim -v -rs
extensions/robot-ur/e2e/run.sh bridge
extensions/robot-ur/e2e/.venv/bin/python -m pytest extensions/robot-ur/e2e -m 'not ursim' -v
python3 extensions/robot-ur/ursim/dashboard.py ready
python3 extensions/robot-ur/ursim/dashboard.py stop
extensions/robot-ur/ursim/ursim.sh down
extensions/robot-ur/ursim/ursim.sh down
extensions/robot-ur/ursim/ursim.sh status
EXCALIBUR_URSIM_TESTS=1 extensions/robot-ur/e2e/.venv/bin/python -m pytest extensions/robot-ur/e2e -v -rs
bash -n extensions/robot-ur/ursim/ursim.sh extensions/robot-ur/e2e/run.sh
extensions/robot-ur/e2e/.venv/bin/shellcheck extensions/robot-ur/ursim/ursim.sh extensions/robot-ur/e2e/run.sh
python3 -m compileall -q extensions/robot-ur/ursim/dashboard.py extensions/robot-ur/e2e/check.py extensions/robot-ur/e2e/contract_client.py extensions/robot-ur/e2e/test_ursim.py extensions/robot-ur/e2e/test_contract_client.py
systemd-run --user --scope -q -p MemoryMax=6G env GOFLAGS=-p=2 make lint cross
systemd-run --user --scope -q -p MemoryMax=6G env GOMAXPROCS=2 go test -race -p 2 ./...
```

ShellCheck 0.11.0, shell syntax, and Python compilation passed. ShellCheck and
vncdotool (used only to operate the simulated pendant) were installed in the
ignored temporary venv, not added as runtime requirements. `make lint cross`
passed: gofmt, go vet, staticcheck, no-bare-go, Darwin build, and Windows build.
The full repository Go race suite also **passed (exit 0)**, including
`tests`, `tests/agentquality`, and `tests/memory`, under the 6 GiB memory cap.
Two earlier foreground runs ended with signal 143 without a test failure
report. The successful final invocation ran in a detached process group,
retaining the same memory cap, with `GOMAXPROCS=2` and package parallelism 2.

## Merge notes and remaining checks

- No Go contracts, eval profile, web files, or RB1-owned implementation files
  changed. Cherry-pick this commit with RB1/RB2; the owned paths are `ursim/`,
  `e2e/`, and `docs/development/robot-ur-setup.md`.
- **TODO(RB1 merge):** launch the real bridge with
  `EXCALIBUR_ROBOT_BRIDGE_COMMAND` or start it separately, then require
  `run.sh bridge` to pass. Confirm the dedicated simulator workspace contains
  the home TCP and the relative-move path. Default DO1 polarity is assumed.
- Workspace rejection, bridge approval/speed enforcement, HTTP STOP concurrency,
  daemon RPC/events, model image input, and the Robot web page have **not** been
  validated live in this branch. The direct probe does not implement or claim
  these bridge protections. No fake bridge was used to claim success.
- The bridge test checks out-of-box rejection before STOP while RUNNING so an
  unrelated stopped-program error cannot produce a false safety pass.
- The move body includes `approved:true` per the common safety rule even
  though that field is missing from Contract 1's enumerated move body.
- The owner's guide is Spanish because RB3 explicitly requested that exception
  to the repository's English-only Markdown rule; other added files are English.
