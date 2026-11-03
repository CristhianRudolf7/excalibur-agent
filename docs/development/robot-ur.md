# UR10e bridge and assistant integration

RB1 implements the robot bridge, daemon RPC/events and supervised `robot`
tool. The web page, URSim rig and physical-network setup belong to the other
RB workers. No physical-arm or URSim motion was performed as part of RB1's
verification. Relevant architecture extension seams are Sections 3.5, 13,
14.12, 19.2 and 23; the architecture specification stays external.

## Installation and configuration

From the repository root:

```sh
extensions/robot-ur/setup.sh
```

This idempotently creates `~/.local/share/excalibur/robot-venv`, installs
`ur_rtde==1.6.2`, and installs the five Python bridge modules under
`~/.local/share/excalibur/robot-bridge`. It does not connect to an arm or
start motion. Python must have `venv` support. Re-run setup after bridge
updates. Ordinary fake tests need only Python's standard library.

The daemon exposes the tool when layered configuration contains
`[robot] enabled = true` (the default). Set
`EXCALIBUR_ROBOT__ENABLED=false` or `[robot] enabled = false` and restart the
daemon to disable its RPC backend and remove the tool from both assistant
and daemon execution catalogs. This does not alter embedded exec or eval.

Environment overrides:

| Variable | Default / behavior |
| --- | --- |
| `EXCALIBUR_ROBOT_PYTHON` | `~/.local/share/excalibur/robot-venv/bin/python` |
| `EXCALIBUR_ROBOT_BRIDGE_PORT` | `7811`; bridge always binds `127.0.0.1` |
| `EXCALIBUR_ROBOT_BRIDGE_SCRIPT` | Installed `robot-bridge/server.py`; falls back to the repository's `extensions/robot-ur/bridge/server.py` during development |
| `EXCALIBUR_ROBOT_VENV` | Setup-only alternate virtual environment; set `EXCALIBUR_ROBOT_PYTHON` to its Python for the daemon |
| `EXCALIBUR_ROBOT_BRIDGE_DIR` | Setup-only alternate module destination; set `EXCALIBUR_ROBOT_BRIDGE_SCRIPT` correspondingly |

Bridge limits are loaded once per connection worker from
`~/.config/excalibur/robot.json`. A malformed file fails explicitly. Example
with the defaults:

```json
{
  "speed_max": 0.25,
  "accel_max": 0.5,
  "joint_speed_max": 0.6,
  "workspace": {"min": [-0.6, -0.6, 0.1], "max": [0.6, 0.6, 1.0]},
  "max_step_m": 0.15,
  "max_step_rad": 0.5,
  "gripper_do": 1,
  "gripper_close_value": true,
  "require_approval": true
}
```

Units are meters and radians, with Cartesian limits in the robot base
frame. Poses are `[x,y,z,rx,ry,rz]`; orientation is a rotation vector.
`speed_max` cannot exceed 0.5 m/s, `joint_speed_max` cannot exceed 0.6 rad/s,
and acceleration cannot exceed 0.5 in the corresponding motion units.
Relative translation uses the Euclidean norm, capped at 0.15 m; rotation
uses the rotation-vector norm, capped at 0.5 rad. Config can tighten these
limits. Requests above configured limits are rejected rather than clamped.
Both the starting TCP position and target/path must be inside the box;
configure a box that contains the actual initial position before use.

The gripper defaults to standard DO1, with true meaning close. DI1 alone
reports closed, DI2 alone reports open, and both/neither report unknown.
The gripper result reports sensors, not an invented successful position.
Named TCP poses persist atomically in
`~/.config/excalibur/robot-poses.json` and survive reconnect/restart.

## Bridge contract

The Python stdlib threaded HTTP server provides:

| Method and endpoint | Request / result |
| --- | --- |
| `GET /health` | `{ok:true, version}`; does not import native RTDE modules |
| `POST /connect` | `{host}` where host is a literal IPv4/IPv6 address; returns state plus PolyScope version and Remote Control status |
| `POST /disconnect` | Stops any activity, invalidates the session, returns `{connected:false}` |
| `GET /state` | Connection, modes, program bit, TCP pose, joints, digital IO arrays, speed scaling, gripper sensors and `updated_at` |
| `POST /move` | `{kind:'named'|'pose'|'relative'|'joints',name?,pose?,delta?,frame?,joints?,speed?,accel?,approved:true}`; returns final pose/joints |
| `POST /gripper` | `{action:'open'|'close',approved:true}`; returns sensor-derived gripper state |
| `POST /stop` | `{}`; always available without approval, including during another request |
| `POST /power` | `{action:'on'|'off'|'brake_release',approved?:true}`; enabling power/brakes requires approval and Remote Control |
| `GET /poses` | `{poses:{name:pose}}` |
| `POST /poses` | `{name}`; saves the actual TCP pose |
| `DELETE /poses/{name}` | Deletes a saved pose |
| `POST /freedrive` | `{on:true,approved:true}` to enter; `{on:false}` to exit |

Failures return `{ok:false,error:'reason'}` and HTTP 400, 409, 502 or 504.
No endpoint clears protective stops or changes controller safety settings.
Connection creates receive/IO interfaces first; control script upload is
lazy so Dashboard power-on/brake release remains possible from an idle arm.
The program-running bit comes from receive `getRobotStatus() & 2`.

Before effects, the bridge checks explicit approval (unless its local
`require_approval` config is false), NORMAL safety mode, RUNNING robot mode
and Remote Control. The Go tool independently requires approval even if the
bridge's local approval switch is disabled. Joint targets and sampled joint
paths undergo FK workspace checks; Cartesian targets undergo reachability
and controller safety checks. Rotation in the tool frame uses rigid-body
composition, not addition of Euler angles.

Every command is serialized inside an isolated native worker process.
Moves use asynchronous RTDE commands with state/mode/workspace monitoring.
Progress state is cached for concurrent HTTP state reads. Native work is
bounded at 24 seconds, leaving time for process termination and independent
STOP within the 30-second move-request limit; the motion polling loop uses
an earlier 22-second deadline. An interrupted or unsuccessful move is an
error. HTTP requests never share an RTDE control object across threads.

STOP terminates the native worker before sending a secondary-interface
URScript containing `stopl` and `stopj`, and a Dashboard `stop` on separate
sockets. It invalidates the session; reconnect before issuing new motion.
The sockets have short deadlines, and failure of either stop transport is
reported. Reconnect does not resume an earlier motion automatically.
Freedrive entry requires approval and a valid initial state; its 50 Hz
watchdog stops on unsafe modes, box violations or excessive measured TCP/
joint speed. A watchdog fault remains visible until reconnect. Manual
freedrive and sampled joint-path checks are not a continuous collision
planner or controller-certified safety limit; configure controller safety
planes/reduced limits for the actual installation.

## Daemon and camera contract

The five additive RPCs use the gateway passthrough seam:

| RPC | Parameters | Result |
| --- | --- | --- |
| `robot.status` | `{}` | `{bridge:'running'|'stopped'|'error',state?,camera?,error?}` |
| `robot.connect` | `{host}` | Same status shape |
| `robot.disconnect` | `{}` | Same status shape |
| `robot.stop` | `{}` | `{ok:true}` or a surfaced error |
| `robot.frame` | `{blob,width,height}` | `{ok:true}` |

`robot.status` observes an idle service without starting Python. Connect or
a bridge-backed tool action starts it on demand. The daemon supervisor
health-checks and restarts a failed bridge with exponential backoff capped
at 30 seconds; it never reconnects an arm or replays an effect after restart.
The bridge belongs to the daemon lifecycle and uses the platform process
group implementation, with parent-death signaling and group cleanup.
All long-lived Go goroutines use `safego.Go`. Shutdown sends STOP before
terminating/joining the process. A healthy manually started bridge at the
configured port can be used; shutdown stops the robot but does not claim
ownership of that external process.

State events have type `robot.state` and payload `{state:<bridge state>}`.
They publish after action/state changes and approximately once per second
while the demand loop runs. State includes `connected:false` after STOP;
failed bridge state reads mark the cached state disconnected and expose the
reason in status. Event consumers should inspect modes and timestamps,
including an older timestamp if a native state read is temporarily blocked.

Upload camera bytes through existing `POST /blobs`; its `blob_digest` field
is the `blob` argument to `robot.frame`. The daemon verifies that the digest
exists and decodes as PNG/JPEG with the declared dimensions. Limits are
4 MiB, 4096 pixels on either axis and 12 million total pixels. Only the
latest ref/metadata are held by the robot service; upload persistence and
retention remain owned by the existing blob store. `robot.status.camera`
contains `updated_at`, `width` and `height`, never base64 bytes.

`robot` actions are `status`, `look`, `move_named`, `move_relative`,
`move_pose`, `move_joints`, `gripper`, `home`, `stop`, `list_poses`,
`save_pose`, and `freedrive`. Gripper uses the `gripper:'open'|'close'`
argument. `home` resolves the named TCP pose `home`. `look` returns state
plus an image input for the existing model. No extra model or vision
extraction call is involved. The engine preserves image inputs in its
history and compiler; OpenAI Responses sends text/image parts in the
matching `function_call_output`. Chat Completions explicitly rejects this
image path rather than silently dropping it. Other provider transports
have not gained tool-image support in this change.

The tool description requires looking first, describing the observation,
planning a small step, asking approval for that exact direction/distance/
speed, and setting `approved:true` only after explicit approval. STOP needs
no approval or motion lease and bypasses bridge health/restart locks. It
also runs with a fresh bounded context if a canceled operation requests it.

## Extension seam and profile isolation

`extensions/robot-ur/excalibur-extension.yaml` is a local `builtin` extension
manifest for the same tool. `Loader.ProvideBuiltin("robot-ur", ...Tool...)`
followed by `LoadDir`/`Activate` registers it as deferred; a test executes the
activated tool through the normal executor. A local `excalibur ext install`
can stage this directory. The daemon's built-in wiring is authoritative;
it does not dynamically activate a second installed copy. There is no new
remote store backend or publication flow.

Daemon assembly adds this tool only when supplied an enabled robot service.
The conversational allowlist also explicitly includes it, including on a
short approval reply. Embedded exec, delegate/eval configuration, fixed
prefix hashes and frozen eval tool/profile tables are untouched. Rollback:
disable `robot.enabled`, restart the daemon, and remove the local extension
if it was separately staged.

## Backend verification

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s extensions/robot-ur/bridge -v
bash -n extensions/robot-ur/setup.sh
systemd-run --user --scope -q -p MemoryMax=6G \
  go test -race -p 2 ./internal/robot ./internal/daemon ./internal/api \
  ./internal/gateway ./internal/config ./internal/providers/openai ./internal/tools
```

Python tests cover caps, malformed/finite input, workspace and relative
norms, mode/approval gates, base/tool rotations, every motion kind, gripper
polarity/sensors, durable poses, remote mode, power, disconnect/reconnect,
protective stop and timeout during motion, freedrive monitoring, HTTP
validation, busy state and STOP during an operation. Optional native-binding
introspection checks the pinned wheel's API without opening a connection.

Go tests use fake HTTP bridges for RPCs, events/heartbeats, status and frame
validation, every action, denied approval with zero bridge effects, model
images, error propagation, canceled motion and unconditional STOP. Process
tests start the actual stdlib Python server without native/hardware access,
kill/restart it, verify backoff and join it on shutdown. A real gateway test
uploads a frame, dispatches RPCs and verifies an assistant `look` survives
the executor, engine history and compiler into the next model request.

Transport references:
[ur_rtde API](https://sdurobotics.gitlab.io/ur_rtde/pages/reference/api.html),
[asynchronous motion example](https://sdurobotics.gitlab.io/ur_rtde/pages/examples/basic_motion/move_async_example.html),
[OpenAI function output format](https://developers.openai.com/api/docs/guides/function-calling).

## Verification record: 2026-11-03

| Check | Result |
| --- | --- |
| `systemd-run --user --scope -q -p MemoryMax=6G nice -n 19 go test -race -p 2 ./...` | Passed the complete repository suite |
| Focused robot/daemon race tests after the owner-cancellation fix | Passed, including assistant image transport, shutdown and unchanged eval/catalog pins |
| Python unittest suite with the pinned native wheel installed | 17 passed; fake operations and offline binding introspection only |
| `go vet -p 2 ./...` | Passed |
| `GOMAXPROCS=2 staticcheck ./...` | Passed |
| Linux `go build -p 2 ./...` | Passed |
| Darwin and Windows `go build -p 2 ./...` | Passed |
| `gofmt -l .`, `git diff --check` and `scripts/check-no-bare-go.sh` | Passed |
| Setup executed twice against repository-local test destinations | Passed; existing dependency and modules were reused/reinstalled idempotently |

The first broad test attempt encountered timing failures in the unchanged
exec-signal and automation-script tests while other worktrees were running
checks. The final complete run passed both without modifications to those
tests. No model API, URSim motion or physical-arm connection was used for
these results. Live simulator/hardware validation remains separate work.
