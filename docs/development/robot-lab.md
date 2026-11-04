# Robot Lab v2

Robot Lab trains four simulated robot bodies through deterministic classical
controllers. The conversational agent observes, plans and improves text/Python
skills within a training session. Physics advances during actions and skill
execution; observations and pauses between calls leave the world frozen.
Decision [0097](../decisions/0097-robot-lab-v2.md) records the Go boundary change.

## Architecture and contract

The daemon composes one `robotlab.Service`, supervised `ProcessSidecar` and
explicit installer. The deferred `robot_lab` tool shares that service's sidecar
and publisher. It is available to default/delegate profiles and enabled master
turns, and remains excluded from eval. Protected engine, provider, embedded
wiring, command and benchmark implementations are unchanged.

The runtime source is embedded in the Go binary, extracted atomically into a
content-addressed directory, and launched lazily from that directory:

```text
python -m robotlab.server --data <data>/robotlab --skills <skills-root>
```

Requests use the existing multiplexed JSON-lines transport, with an 8 MiB line
limit. Sidecar error codes become stable API errors. Process-group shutdown,
bounded restart supervision, ready deadlines and subscriptions remain in place.
Long-lived workers use `safego.Go` and join on shutdown. The previous experiment,
teleoperation and separate vision-step RPC/tool paths are removed. `VisionLoop`,
`VisionAsker` and the compatibility hook remain so existing composition callers
compile; the hook has no side effects and returns guidance to use `look`.

`packages/contracts/robotlab.json` is the v2 wire schema. All sidecar methods are
forwarded with their dots unchanged, for example `robotlab.env.list`,
`robotlab.session.start`, `robotlab.robot.act` and `robotlab.skills.write`.
`Service.Op` validates parameters against the complete schemas, including
recursive goals, before forwarding. Runtime validation owns geometric bounds,
unique object IDs, spawn collisions, robot-specific tool arguments and physics.
The Go-specific operations are status merging, explicit installation and
`robotlab.media.upload`. Uploads decode at most 32 MiB, sniff image/video MIME,
write a private random upload filename, and call `media.ingest`. A failed ingest
removes its upload. The uploaded name is display metadata, never a file path.
HTTP and WebSocket RPC envelopes allow up to 48 MiB only for a single
`robotlab.media.upload`, accommodating the base64-encoded media. Every other
method and HTTP batch retains the 4 MiB total body limit. Oversized HTTP bodies
and method-specific rejections return the existing JSON-RPC parse error;
WebSocket messages over 48 MiB close with message-too-big status.

`robotlab.session.image {session, path}` forwards log-image reads and returns
`{png}`. Its schema permits only PNGs directly under an attempt's `looks/`
directory or its `failure/image.png`; runtime lookup enforces session containment.

The generator `python3 scripts/gen-robotlab-contracts.py` updates only the Robot
Lab entries in API method/event registries and JSON exports, generated Go types,
embedded validation schema and marked TypeScript sections. Run the web formatter
after generation:

```sh
python3 scripts/gen-robotlab-contracts.py
cd apps/web
pnpm exec prettier --write src/lib/rpc/contract.ts
```

The tool has per-action argument schemas, a 95 second timeout, and non-idempotent
execution. It sends each mutation once. `look`, `environment_preview` and `media`
return PNG data URLs in `tools.Result.Images`; text retains visible objects,
state, user specification, goal and attempt progress. Session start takes the
conversation from `whiteboard.Conversation(ctx)` and publishes
`robotlab.focus {session, robot, environment}`. Skills are per session: reuse
requires `copy_skills_from` at start or `skills_copy`.

Sidecar `frame` events become ephemeral `robotlab.frame`: no telemetry sink,
retained replay or outbox. Subscribe explicitly for live viewing; reliable 3D
replay uses `attempt.frames`. Sidecar `session` and `log`, Go `focus`, and installer
progress use the retained event bus.

When `robot_lab` is activated or the conversation owns a live session, and
`EXCALIBUR_MASTER_STEPS` is unset, a master turn gets 48 steps and at least 15
minutes. After a turn that performed a lab call drains, a supervised worker
rechecks ownership and live status and delivers the prescribed continuation via
`master.ReceiveRelay`, sender `robot-lab`, receipt `robotlab:<session>:<k>`.
The bound is `3 * max_attempts` per session; persisted relay history restores the
count after restart. Stopped, successful, failed and exhausted sessions do not
continue, and canceled/restarted turns and daemon shutdown do not trigger relays.
An explicit steps setting preserves the operator's normal turn limits.

## Data and environment

| Path | Purpose |
| --- | --- |
| `<data>/robotlab/src/<sha256>/` | Extracted embedded Python source |
| `<data>/robotlab/venv/` | Managed interpreter and pinned packages |
| `<data>/robotlab/venv/.robotlab-installed` | Written only after all install stages succeed |
| `<data>/robotlab/menagerie/` | Downloaded robot assets and licenses |
| `<data>/robotlab/uploads/` | Original successful uploads |
| `<data>/robotlab/media/<id>/` | Imported media and representative PNG frames |
| `<data>/robotlab/meshes/<key>.bin` | Decimated viewer mesh cache |
| Session `attempts/<n>/frames.jsonl` | Recorded 20 Hz simulation frames |
| Session `attempts/<n>/failure/` | Failure image, state and executed code |
| `<skills-root>/<session>/<skill>/` | Instructions, optional code, versions and failures |

The skills root is `EXCALIBUR_ROBOT_SKILLS_DIR` or
`~/.local/share/excalibur/robot-skills`. It lives outside the repository.
`EXCALIBUR_ROBOTLAB_MENAGERIE` overrides asset discovery.
`EXCALIBUR_ROBOTLAB_PYTHON` selects an existing interpreter for development;
it still runs the binary's extracted source. Installation always targets the
managed venv. `EXCALIBUR_ROBOTLAB=off` disables operations while leaving status
readable. Non-Linux processes report unsupported and never launch Python.

On Linux, `MUJOCO_GL` defaults to `osmesa` only when unset. The child inherits
explicit rendering selection, lab switches, the skills override and basic OS
paths/locale. It sets unbuffered Python, disables bytecode, and caps OMP/MKL to
two threads. Daemon credentials and host Python/loader injection variables are
excluded. Installation alone additionally receives pip, proxy and CA settings.
OSMesa remains a system prerequisite, reported by status without loading it.

## Installation and asset decision

The Go installer checks Python >= 3.10, creates a venv, installs the embedded
runtime-owned `requirements.lock`, calls
`python -m robotlab.assets fetch --dest <data>/robotlab/menagerie`, verifies
MuJoCo, NumPy, OpenCV and Pillow imports, then writes the completion marker.
Concurrent explicit requests coalesce. Status and startup never install anything.
For an existing v1 data directory, request installation once to populate the v2
asset directory.
The v2 runtime lock removes the previous training frameworks; it is supplied by
the Python runtime work, independently of the Go installer.

Robot assets are approximately 120 MB and are downloaded, never versioned in
this repository. The runtime-owned manifest pins Menagerie commit
`98a70c569c4c1b6da467a20012f766ee1f8dc521` and individual file SHA-256 hashes.
Only these six folders are required: `universal_robots_ur10e`, `robotiq_2f85`,
`unitree_go2`, `unitree_z1`, `skydio_x2` and `unitree_g1`. Downloads use the pinned
raw file URLs. Retain upstream license files and `ATTRIBUTION.md` with the assets;
asset licenses are separate from the harness license. The Python world worker
owns the manifest/downloader and its integrity/license tests. The Go installer
tests mock this fetch command and check that a fetch failure cannot mark an
installation complete; no asset download was performed for those tests.

## Tests and integration limits

Ordinary Go tests use fake sidecars, fake relay masters, a tiny Go transport
helper and fake installer commands. They do not need Python, robot assets,
network access or paid calls. From the repository root:

```sh
systemd-run --user --scope -q -p MemoryMax=6G nice -n 19 /usr/local/go/bin/go build -p 2 ./...
systemd-run --user --scope -q -p MemoryMax=6G nice -n 19 /usr/local/go/bin/go vet -p 2 ./...
systemd-run --user --scope -q -p MemoryMax=6G nice -n 19 /usr/local/go/bin/go test -race -p 2 ./internal/robotlab/... ./internal/daemon/... ./internal/api/... ./internal/gateway/... ./internal/telemetry/... ./internal/wireengine/... ./internal/cli/... ./internal/skills/...
```

Contract tests cover method/event parity, embedded schema/source parity, every
forwarded sidecar method, invalid parameters and upload boundaries. Tool tests
cover all actions, images, focus, conversation attribution, timeout and one-send
mutations. Daemon tests cover master-role access, extended budgets, end-of-turn
relay timing, session ownership/status, persisted continuation caps and shutdown.
The TypeScript RPC contract has a targeted Vitest check:

```sh
cd apps/web
systemd-run --user --scope -q -p MemoryMax=6G nice -n 19 pnpm exec vitest run src/lib/rpc/contract.test.ts
```

Gateway tests exercise both transports at 4 MiB and 48 MiB, a full-size base64
upload envelope, unchanged backend parameters, rejection without dispatch and
WebSocket reuse after a method-specific rejection. HTTP tests additionally
cover unknown Content-Length, whitespace, nested method names and batch limits.
Python runtime, assets and UI changes must be integrated before the live demo.
The Go fixture checks are not evidence of live physics or successful training.

## Live demo procedure

1. Integrate the v2 Python runtime, asset fetcher, robot plugins and web client.
   Configure the assistant normally. Choose a random unused loopback port in
   78xx for the daemon and 51xx for a development web server. Check both are free;
   never use shared ports 7797, 7798, 4173 or 5182.
2. Build with the memory-capped command above. Start the daemon with a separate
   data directory and `daemon start --listen 127.0.0.1:<port> --data-dir <data>`.
   Enable the lab; install using the lab's explicit install operation, or use a
   prepared interpreter and `EXCALIBUR_ROBOTLAB_MENAGERIE`. Keep credentials out
   of command arguments and logs.
3. Open `/lab`, check the four robot cards and default environments, and preview
   `mi-cuarto` and `mesa-figuras`. Ask the assistant to train the dog in `mi-cuarto`
   to bring the ball back to its starting point. Confirm session focus opens,
   observer/camera frames appear during actions, and physics freezes between
   tool calls.
4. Inspect plan/action/look log entries, a reflected retry and a session skill.
   Confirm simulator goal truth determines the displayed outcome. Verify that
   bounded continuation keeps an unfinished live session moving across turns.
5. Stop a session from the UI during an action. Confirm the session stops and no
   continuation restarts it. Select an old attempt and inspect its replay.
6. Start another session with `copy_skills_from`, then verify copied skills.
   Upload a small image/video, provide dimensions, inspect media frames, ask
   about ambiguous measures, create a simple-shape environment and preview it.
   Record observed results separately; these steps are a procedure, not a live
   verification claim.

## Verification (2026-11-03)

Automated, on the integrated `robotlab-v2` tree:

- Python runtime: `pytest -q tests/` passes (390 tests, real Menagerie models,
  JSON-lines server round trips, real dog/arm/drone/humanoid sessions, failure
  evidence and skill regression).
- Go: `go vet` and `go test -p 2` for robotlab, daemon, api, gateway, telemetry,
  wireengine, cli, skills and cmd pass. The eval/Terminal-Bench paths and the
  real-arm page/bridge are unchanged.
- Web: `tsc -p tsconfig.app.json`, Vitest and the lab Playwright suite
  (`e2e/robotlab.spec.ts`, 7 tests, fake backend) pass.

Live, with an own daemon and the configured assistant model:

- Environment created from text ("Sala de juegos"), then edited by text (ball
  size and mass). The dog with arm brought the ball back to its start point;
  the simulator verified the goal and the session ended `succeeded`, using a
  skill copied from the previous session that was written after a failure.
- Humanoid sessions in `pasillo`: the robot falls in early attempts (walls,
  obstacle), the agent writes and improves skills after each failure
  (versions v1…v6), copies them into a new session and walks further with them.
- Training continues across assistant turns through the continuation relay.
- Arm in `mesa-figuras`: the first attempt closed the gripper too high; the agent
  wrote a skill from that failure and placed the red cube inside the bin on the
  second attempt (container-aware `object_on`).
- Drone in `pasillo`: took off, flew over the obstacle and reached the mat on the
  first attempt. Note: `robot_near` is horizontal, so it is met while flying over
  the target; a goal that also requires landing is not available yet.
- Environment created from an uploaded image plus a specification: the agent
  reconstructed the main furniture with approximate placement.

## Operational notes learned live

- Use the default service tier for the lab. With the flex tier, a single model
  call with the lab tools active took minutes, which makes training impractical.
- A failure must explain itself: fall/safety reasons include the environment
  geoms the robot touched shortly before (`touched wall/2 with
  robot/right_shoulder_pitch_link`). Without this the agent learned wrong lessons.
- A timed-out action (60 s simulation limit) ends only that action; the attempt
  continues. Falls, crashes and safety stops end the attempt.
- `attempt_reset` after a failed or abandoned attempt requires a skill event in
  that attempt or an explicit `no_skill_reason`; this keeps skills central.
- Objects created from text need realistic sizes and masses. The dog's gripper
  holds objects up to about 0.12 m wide and 0.5 kg; walking into loose objects
  kicks them.
- Read-only methods (scene, frames, state, logs, meshes) stay available while an
  action runs, so the live 3D view and the robot camera update during training.
