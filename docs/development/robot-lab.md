# Robot Lab v3

This milestone integrates two simulated robots, low-level keyframe control,
markdown session skills and environments from chat text across the runtime, Go
service and browser. [Decision 0109](../decisions/0109-robot-lab-v3.md) records the
Go boundary; [decision 0110](../decisions/0110-robot-lab-v3-contract-integration.md)
records shared contract integration and structured replay state. Scripted physics
proofs establish feasibility; Go fixtures alone do not verify robot training.

## Backend contract

The daemon composes one `robotlab.Service`, supervised `ProcessSidecar` and
explicit installer. The deferred `robot_lab` tool shares the service's sidecar
and publisher. Default/delegate profiles and activated master turns can use it;
eval remains excluded. The benchmark path and real-arm bridge are outside this
milestone.

The runtime is embedded in the binary, extracted atomically under
`<data>/robotlab/src/<sha256>/`, and launched lazily:

```text
python -m robotlab.server --data <data>/robotlab --skills <skills-root>
```

JSON-lines requests preserve dotted method names, such as `session.start` and
`robot.act`. Go validates inputs against its embedded schema before forwarding;
the runtime owns geometry, robot-specific arguments, reachability and physics.
The sidecar parameter limit is 8 MiB; HTTP and WebSocket RPC envelopes use
the gateway's ordinary 4 MiB limit. Shutdown, bounded restart supervision, ready
deadlines and subscriptions retain their existing behavior. Long-lived Go
workers use `safego.Go` and join on shutdown.

The v3 boundary rejects the retired media ingestion/upload/read and skill
execution methods, and rejects `skill_write.code`. `VisionLoop`, `VisionAsker`
and `Service.VisionStep` preserve source compatibility for existing callers;
the hook is a no-op returning `(nil, nil)` without invoking the sidecar or asker.
`look` and `environment_preview` still return PNG data URLs in tool results.
`act(tool="look")` remains an alias. `session.image` still reads recorded PNG
observations and failure images with validated attempt-relative paths.

The tool has per-action schemas, a 95-second timeout and non-idempotent execution.
Mutations are sent once, except the bounded resume conflict recovery described
below. Session start and resume take their conversation from context and publish
`robotlab.focus` using the returned session robot/environment. Sidecar frame events
use the ephemeral bus; session/log events, focus and installer progress use the
retained bus. Replay frames remain available through `attempt.frames`.

When `robot_lab` is activated or the conversation owns a live session, a turn
gets **120 steps and at least 30 minutes** if `EXCALIBUR_MASTER_STEPS` is unset.
A longer existing timeout is preserved. An explicit steps setting preserves
the operator's normal limits. After a turn that used the lab drains, a supervised
worker binds its continuation to that session, then rechecks ownership, live
status and the runtime's current live ID before relaying the v3 training loop.
The receipt is `robotlab:<session>:<k>`, sender `robot-lab`, and the continuation
cap stays `3 * max_attempts`, restored from history after restart. User stop,
terminal session states, canceled/restarted turns and daemon shutdown prevent
continuation. Session stop events from either agent or user cancel pending work
immediately; a different live session also cancels it. The turn-budget entry hook
records the parent turn start, preserving it across drained inputs with the same
context. A newer non-relayed user message in that conversation suppresses the
continuation, including when the message arrived before it was queued. Skipped
continuations consume no receipt or continuation allowance.

## Resume from chat

An explicit retry/repeat request, including transcribed voice text, discovers
existing sessions through tool action `sessions` (`session.list`). Results are
newest first and contain ID, name, robot, environment, task, status, attempt count,
creation time and live flag. Optional filters combine: `robot` matches exactly;
`environment` is a case-insensitive ID substring; `query` is a case-insensitive
substring of name, task or environment. Broaden filters when necessary and select
the newest session matching the requested robot and environment.

`session_resume {session}` calls `session.resume {session, reason?, conversation?}`.
It resumes that ID using its saved `environment.json` and robot, clears terminal
outcome/time, appends attempt n+1, retains all earlier frames, scene and skills,
and sets the cumulative attempt ceiling to attempts made plus the current
settings allowance. The settings range remains 1–100; cumulative session ceilings
can exceed 100. The session's conversation becomes the caller's conversation.
A system log note "Session resumed" and a session event accompany the change.
The tool resolves live-session conflicts deterministically. After a conflict it
reads runtime status and verifies that the requested session exists before
stopping the other live session with `by:"agent"` and reason
`switched to <id> on user request`. It retries resume exactly once, reports the
stopped ID as `stopped_session`, and publishes focus after success. If that retry
fails, the error still identifies the stopped session; no further mutation is
attempted. If the requested session itself is live, the tool returns its current
session and publishes focus without stopping or resetting it. Succeeded sessions
remain resumable. Timeout and transport failures never trigger recovery. A retry
never uses `session_start`.

Focus navigates to `/lab?tab=trainings&session=<id>` and opens the agent column,
including from other pages while a voice call remains mounted. A session event
updates replay to the new live attempt without remounting chat. The attempt
selector includes the appended attempt and preserves previous replay choices
when training ends. Live frames are scoped to the attempt so delayed old frames
cannot overwrite the resumed world. Session lists refresh to show live status.
The agent reads the returned card and uses the session's markdown skills through
`skill_use` before continuing the normal training loop. Continuations follow the
new conversation owner and use the expanded cumulative budget.

## Low-level training loop

The built-in skill and tool description teach the same procedure:

1. Start a session, read the robot card (joints, limits, link lengths, reference
   pose, sensors and notes), and read/use relevant session skills.
2. Record a plan of small keyframes and check reachability and joint-limit
   margins with `ik`, which advances no physics or simulation time.
3. Prefer `sequence` calls with 4–12 keyframes over single moves. Each sequence
   accepts 1–16 keyframes with individual durations, at most 30 seconds total.
4. Inspect state, contacts, goal and attempt after every call. For the dog,
   inspect support, signed margin, margin direction and terrain ahead.
5. On each failed or abandoned attempt, reflect and write/improve a markdown
   skill with the lesson and exact keyframes that worked so far. Attach
   `failures:[N]`, then reset. If no useful lesson can be saved, give an explicit
   `no_skill_reason` of at most 2000 characters.
6. Continue until simulator goal truth or the attempt limit; never infer success
   from an image or a successful tool invocation. Summarize skill learning.

Angles are degrees, lengths meters, and durations simulation seconds. Motion
smoothly interpolates targets and holds them during a settling interval (default
0.2 seconds, allowed 0–2). Between calls, physics pauses and motors retain their
last targets. Out-of-reach or out-of-limit targets are rejected before motion.
Sequence stop conditions freeze targets and report the stopping keyframe and
reason. Both robots support unexpected-contact stops; dog also supports a
support-margin threshold. `wait` holds targets for 0.05–5 seconds.

| Robot | Body and tools | State and observations |
| --- | --- | --- |
| `dog` | Go2; `move_joints`, `move_feet`, `sequence`, `ik`, `wait` | Trunk pose/velocity, COM, support/margin, leg targets and joints, foot contacts/forces, terrain/edges and link contacts; head and rear cameras |
| `arm` | UR10e + Robotiq 2F85; `move_joints`, `move_gripper_to`, `gripper`, `sequence`, `ik`, `wait` | Joint angles, gripper pose/opening and finger contacts, held object and tracked object poses; wrist and overhead cameras |

For dog foot/trunk keyframes, unlisted feet keep their world target points and
omitted trunk fields keep their targets. Body-frame foot points are relative to
the desired trunk pose. Keep three feet planted and the COM in the support
triangle before lifting a foot. Arm grasp assist requires both finger pads
contacting the same dynamic object at motion end; opening beyond the object's
width releases it.

## Environments and goals

Environments are created and edited only from chat text. Use version 1 JSON,
`source:{type:"text",notes?}`, simple shapes, realistic dimensions/masses and
unique object IDs. Sizes are full dimensions in meters, positions centers, yaw
degrees. Omitted z rests on the floor/table. Preview before starting a session.
Legacy saved source metadata normalizes to text on runtime load; existing user
copies of environments remain available.

Exactly two environments are shipped, with director-owned geometry:

| ID | Kind/robot | Goal |
| --- | --- | --- |
| `escalera-estrecha` | Room / dog | `{"type":"robot_on","target":{"object":"descanso"},"feet":4,"stable_s":1}` |
| `estante-pequeno` | Table / arm | `{"type":"object_on","object":"caja","target":{"object":"estante"},"stable_s":1}` |

The stair has 0.12 m rises, 0.30 m treads and 0.45 m width without railings. The
shelf task places a 7 cm box on a 5 × 5 cm top 0.40 m above the table. These tasks
require careful support and placement; the Go layer does not synthesize gaits or
grasp routines.

`robot_on` requires 1–4 feet contacting the target, no fall and continuous
stability for `stable_s`. `object_on` requires a resting, released object with no
robot contact, bottom within 1 cm of the target top, COM over the surface under
the existing surface/container rules, and speed below 0.02 m/s continuously for
`stable_s`. Both default to 1 second. Existing near, held and composite goals
remain. Settings default to 40 attempts and allow 1–100.

## Skills, storage and installation

Skills are markdown only: `SKILL.md` with frontmatter and a body of lessons,
mistakes and exact working keyframes. `skills.write` takes `session`, `name`,
`description`, `instructions`, and optional `failures`, returning
`{skill, accepted:true}`. Every write creates an accepted version. Versions,
`fallos/` evidence, uses and success rates remain; skills are copied explicitly
with `copy_skills_from` or `skills_copy`. After failure/abandonment, the reset
learning gate requires a skill write/improvement or `no_skill_reason`.

| Path | Purpose |
| --- | --- |
| `<data>/robotlab/src/<sha256>/` | Extracted runtime |
| `<data>/robotlab/venv/` | Managed interpreter and pinned requirements |
| `<data>/robotlab/menagerie/` | Downloaded robot assets/licenses |
| `<data>/robotlab/meshes/<key>.bin` | Viewer mesh cache |
| Session `attempts/<n>/frames.jsonl` | Recorded simulation frames |
| Session `attempts/<n>/failure/` | Failure image and state evidence |
| `<skills-root>/<session>/<skill>/` | SKILL.md, versions and fallos |

The skills root is `EXCALIBUR_ROBOT_SKILLS_DIR` or
`~/.local/share/excalibur/robot-skills`. `EXCALIBUR_ROBOTLAB_MENAGERIE` overrides
assets, and `EXCALIBUR_ROBOTLAB_PYTHON` selects a development interpreter. Runtime
source still comes from the binary. Installation targets the managed venv,
installs runtime-owned requirements, fetches pinned assets, verifies imports
and writes its completion marker only on success. Status/startup never install.
The v3 asset set consists of `unitree_go2`, `universal_robots_ur10e` and
`robotiq_2f85`. Assets are downloaded, never committed.

`EXCALIBUR_ROBOTLAB=off` disables operations while leaving status readable. The
Linux child defaults `MUJOCO_GL` to `osmesa`, runs unbuffered without bytecode,
and caps numeric-library threads to two. Its environment excludes credentials
and host Python/loader injection variables; install alone receives pip/proxy/CA
settings. OSMesa is a system prerequisite. Existing platform lifecycle and
sandbox hooks are preserved by this Go change.

## Verification commands and limits

Run from the repository root on the shared machine. These checks use fakes and
never call paid APIs:

```sh
systemd-run --user --scope -q -p MemoryMax=6G nice -n 19 /usr/local/go/bin/go vet -p 2 ./internal/robotlab ./internal/daemon ./internal/api ./internal/gateway ./internal/wireengine ./internal/cli ./internal/skills ./cmd/excalibur
systemd-run --user --scope -q -p MemoryMax=6G nice -n 19 /usr/local/go/bin/go test -race -p 2 ./internal/robotlab ./internal/daemon ./internal/api ./internal/gateway ./internal/wireengine ./internal/cli ./internal/skills ./cmd/excalibur
systemd-run --user --scope -q -p MemoryMax=6G nice -n 19 /usr/local/go/bin/go test -p 2 ./internal/skills -run RobotLab
```

Go tests cover forwarding, validation, rejection of retired methods/code,
image results, inert compatibility hooks, the look alias, deferred discovery,
eval exclusion, session ownership, continuation limits, and the larger turn
budget through an assistant turn exceeding the old 48-step limit. The schema
consistency tests must also agree with the integrated source and registries.
Do not count a fixture run as evidence of live physics or goal completion.

The contract generator reads `packages/contracts/robotlab.json` and updates
Robot Lab registries, Go types, embedded schema and marked TypeScript sections:

```sh
python3 scripts/gen-robotlab-contracts.py
export PATH=$HOME/.local/opt/node-v22.23.4-linux-x64/bin:$PATH
pnpm install --frozen-lockfile
pnpm --filter @excalibur/web exec prettier --write src/lib/rpc/contract.ts
cd apps/web
systemd-run --user --scope -q -p MemoryMax=6G nice -n 19 pnpm exec vitest run src/lib/rpc/contract.test.ts
```

The shared source, registries and generated consumers are integrated. Runtime
checks run in `internal/robotlab/runtime`:

```sh
MUJOCO_GL=osmesa EXCALIBUR_ROBOTLAB_MENAGERIE=/home/developer/excalibur-sim/rl2/menagerie systemd-run --user --scope -q -p MemoryMax=6G nice -n 19 /home/developer/excalibur-sim/venv/bin/python -m pytest -q tests/
```

For a live integrated check, pick random free daemon ports in 78xx and web ports
in 51xx; never use shared ports 7797, 7798, 4173 or 5182. Verify the two cards and
built-ins, text creation/editing, preview/look, keyframe actions and state,
failed-attempt skill learning, reset, replay, copying, continuation and user stop.
Require simulator truth for the stair/shelf outcome. Record observations
separately; this procedure is not a claim that live v3 training has passed.

## Integrated dispatch coverage

`tests/test_core_integration.py` drives real sessions through the server dispatcher
and JSON-lines serialization with installed robot assets and unchanged built-in
worlds. The dog test exercises both cameras, cached robot state, the abandoned
attempt learning gate, a markdown skill with failure evidence, reset and usage
accounting, then reuses the public keyframe stair planner. It stops immediately
when simulator goal truth succeeds. The arm test reuses the shelf acceptance
plan through `robot.act` and verifies stable, unassisted placement.

Both tests assert terminal session/attempt success, simulator goal truth,
20 Hz recorded frame counts, scene/body pose consistency and structured action
state in persisted replay logs. State reads occur during live sessions; final
state comes from the terminal action response. These scripted tests do not
establish the assistant's ability to discover a successful motion plan.

## Merged verification

Verified on the integrated branch with local fakes and installed simulation
assets. Every test ran in a user systemd scope with `MemoryMax=6G` and
`nice -n 19`; Go commands used `-p 2`.

| Check | Result |
| --- | --- |
| Full Python runtime suite, including real dog/arm dispatch | 306 passed, 1 native Windows check skipped |
| Race tests for robotlab, daemon, api, gateway, wireengine, cli, skills and command | All eight packages passed; skills rerun after correcting its template headings |
| Go vet for the same eight packages | Passed |
| macOS and Windows builds for the same eight packages | Passed |
| Whole web `pnpm check` | Typecheck, lint, formatting and 129 Vitest files / 714 tests passed |
| Final generated-contract TypeScript check | Passed |
| Lab Playwright, including production build | 10 tests passed |
| Repository formatting and whitespace checks | Passed |

Existing web lint and bundle-size warnings remain non-failing. The native
Windows runtime check requires a Windows host; retained helper tests run on
Linux. The nonprotected retired-feature scan matches only negative Go tests
that verify removed operations stay rejected.

## Resume verification

Resume coverage includes every terminal status, live-session conflicts, repeated
attempt numbering, preserved recordings and scene, saved-world reconstruction,
skill reuse, conversation transfer and cumulative attempt allowances. Go tool
and daemon tests cover filtered discovery, schema validation, focus publication,
bounded conflict recovery, no transport-error retries, and continuation ownership
after budgets exceed 100.

Web tests cover focus while replaying, the appended attempt selector, rejection
of delayed frames from older attempts, both requested retry texts and active
voice-call controls after navigation. The lab hosts the shared call sheet so it
can be closed and reopened without ending the call. Browser agent turns are
local fakes; these tests verify UI integration, while backend tests verify the
session mutation and skill persistence.

Verified on the resume branch with the required user systemd scopes, 6G memory
caps, nice 19 and Go parallelism 2:

| Check | Result |
| --- | --- |
| Full Python runtime suite | 400 passed, 1 native Windows check skipped |
| Expanded resume status/filter tests | 7 passed, including the additional failed-status case |
| Whole web `pnpm check` | Typecheck, lint, formatting and 129 files / 716 tests passed |
| Lab Playwright with production build | 13 passed, including chat resume, replay-to-live and an active voice call |
| Go vet for robotlab, daemon, api and gateway | Passed |
| Go race tests for the same four packages with a 30-minute timeout | All four passed |

The initial daemon race run reached the default ten-minute package timeout
without an assertion failure; its remaining wiring test was setting up SQLite.
The successful rerun used `go test -race -p 2 -timeout 30m` for the same four
packages; the daemon package completed in 446.612 seconds.

## Deterministic retry switching verification

Tool tests cover conflict → stop → one resume retry → focus, the exact stop
reason and `stopped_session` result, returning the same live session, resuming a
succeeded session, missing saved targets and bounded recovery errors. The tool
description remains within its enforced 3500-character budget; the built-in skill
documents automatic recovery without requiring the agent to handle the conflict.

Continuation tests cover newer user input before and after queueing, drained
inputs under one parent turn, later valid turns, stopped/noncurrent sessions,
ownership changes, and cancellation through agent-tool and user-RPC stop paths.
Stopped work cannot reappear merely because the same session ID resumes.

Verified with local fakes under the required user systemd scopes, `MemoryMax=6G`,
`nice -n 19` and Go parallelism 2:

```sh
systemd-run --user --scope -q -p MemoryMax=6G nice -n 19 /usr/local/go/bin/go vet -p 2 ./internal/robotlab ./internal/daemon ./internal/skills
systemd-run --user --scope -q -p MemoryMax=6G nice -n 19 /usr/local/go/bin/go test -race -p 2 -timeout 30m ./internal/robotlab ./internal/daemon ./internal/skills
```

Both commands passed; the full daemon race suite completed in 400.628 seconds.
The final targeted daemon RobotLab race checks and shortened-skill package checks
also passed. These tests establish Go behavior without a live training run.

## Live verification with the real agent (2026-11-04)

Run against a local daemon and web preview with the real chat agent (no fakes).

- Arm, `estante-pequeno`: succeeded on attempt 1 with low-level `ik` + `sequence` +
  `gripper` calls (box released centered on the 5 cm shelf top, `object_on` stable).
- Arm retry from chat ("Vuelve a intentar la simulación del brazo robótico en la sesión
  de la caja y el estante"): the agent listed sessions, resumed the same session, the web
  navigated from the agent home page to `/lab?tab=trainings&session=<id>`, attempt 2 failed
  (speed safety stop while carrying the box), the agent wrote `grasp-and-place-slow`, and
  attempts 3 and 4 succeeded using it.
- Dog retry from chat ("Ahora vuelve a intentarlo con el perro, en la sesión de la
  escalera"): resumed the existing stairs session (attempt 26) and the web navigated to it;
  the first action applied the saved skill `aproximacion-escalon-segura`.
- Dog, `escalera-estrecha`: across several sessions (skills copied forward) the agent learned
  weight transfer before lifting a foot, foot planting height, short crawl steps and calf
  clearance at the step edge, but did not reach the landing within the observed attempts.
  The scripted feasibility test proves the climb is physically possible with the same tools.

Operational lessons from these runs:
- A model account without credits makes provider calls retry until the 30-minute lab turn
  cap; probe the credentials when a lab turn stalls with no tool calls.
- The web preview must proxy to the lab daemon (`EXCALIBUR_DAEMON_URL`); otherwise the
  page talks to a different daemon and never receives `robotlab.*` events.
- Replayed `robotlab.focus` events older than 15 s are ignored so opening the app does not
  jump to an old session.
- Retry requests rely on tool-level guarantees (resume works for finished sessions; a live
  session is stopped and the requested one resumed; stale continuations are dropped) rather
  than on prompt wording alone.
