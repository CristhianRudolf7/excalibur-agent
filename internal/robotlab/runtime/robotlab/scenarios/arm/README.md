# Robot arm workcell

`ArmScenario` registers as `arm`. It provides `reach`, `pick_place` and `sort`
tasks on the same six-axis arm and two-finger gripper. All geometry is authored
from MuJoCo primitives. There are no meshes, downloaded models or grasp welds.

## Model and parameters

The arm uses standard DH transforms `Rz(q) Tz(d) Tx(a) Rx(alpha)`:

| Joint | a (m) | d (m) | alpha (rad) |
| --- | --- | --- | --- |
| shoulder_pan | 0 | 0.1625 | pi/2 |
| shoulder_lift | -0.425 | 0 | 0 |
| elbow | -0.3922 | 0 | 0 |
| wrist_1 | 0 | 0.1333 | pi/2 |
| wrist_2 | 0 | 0.0997 | -pi/2 |
| wrist_3 | 0 | 0.0996 | 0 |

The base is at `[-0.4, 0, 0.74]`, rotated pi around world z. Joint angles
are radians. The tool centre `tcp` is 0.115 m along the flange's local z;
its positive z points down during scripted manipulation. Position servos use
bounded forces, damping and gravity compensation. Arm links do not collide;
fingers, objects, table and bin walls do. This keeps arbitrary RL exploration
stable while preserving contact-based grasping and placement. A joint equality
couples the passive finger to the single `gripper` actuator. Its range is
`[0, 0.045]` metres per finger: zero closes, 0.045 opens a 90 mm gap.

Parameters are `task` (default `reach`), `objects` (integer 3–8, default 5),
`seed` (nonnegative integer, default 0) and `randomize` (boolean, default false).
The latter varies lighting and part colours on each reset. Reset seeds determine targets and
non-overlapping, stratified part layouts. Bolt, nut, cube, ball and washer parts
cycle through red, green, blue and yellow. Every fourth part and the first cube have a visible dark
defect, so even a three-part layout includes one. Parts retain stable mark names `part_1` through `part_N`. `marks(env)`
returns `id`, `name`, `label`, `body` and `world`; true labels are diagnostic
metadata and must be removed before model vision input. Bins `A`, `B` and
`reject` have blue, green and red walls, respectively, with raised white A/B/X
strokes. Fixed cameras are `top` and `side`.

## Environments and controls

`make_env(reward_weights=None, randomize=False, render_mode=None)` returns an
`ArmEnv`. Actions have seven normalised position targets in `[-1, 1]`, ordered
as the six table joints followed by `gripper`. Physics advances ten 2 ms steps
per action (50 Hz). Episodes truncate at 200 actions. Observations are float32:

- Reach: six joint positions, six velocities, target xyz, target minus TCP xyz.
- Pick/place and sort: six positions, six velocities, gripper opening, first
  part xyz, part minus TCP xyz, target-bin minus part xyz.

Reach samples targets in `[0.03, -0.22, 0.91]` to `[0.32, 0.22, 1.23]` metres.
It reports `distance` (negative metres), `effort` (negative mean squared action),
`smooth` (negative squared action change) and `success` (within 25 mm).
Manipulation reports `reach`, `grasp` (bilateral pad contact), `lift`, `place`,
`success` and `effort`. The learning objective moves `part_1` into `A`; sorting
uses the same shaping, while vision skills can move any mark into any bin.
Bin success requires a released part resting inside the walls. Interactive arm
sessions retain their layout after success or the training episode step limit;
use Reset to start a new layout. Policy playback and training keep automatic
episode resets. Reward weights
are exposed individually; other-task terms are unused for that episode.

Sliders address actuator names directly. Keyboard A/D moves shoulder pan,
S/W shoulder lift, F/R elbow. Keyboard events increment the current target;
unspecified joints retain their previous commands. `idle_action` holds all
commands instead of jumping to the middle of the joint limits.

## Skills

`skill(env, name, args)` is a bounded generator yielding once per control
step, returning a human-readable completion detail. Exhaust it to complete
an operation. Exceptions report invalid inputs, IK residuals, missing bilateral
contact, slipping or failed bin verification. Closing the generator stops
further skill steps. Skills manipulate actuator controls, never the live
arm/object positions or object constraints.

- `home`, `open_gripper`, `close_gripper`: no arguments.
- `move_to`: `{"xyz": [x,y,z], "yaw": 0}`; yaw is optional, in radians.
- `pick_place`: `{"object": "part_1", "target": "A"}`; an integer mark id or
  its numeric string is also accepted. Targets are `A`, `B`, `reject`.

IK uses the MuJoCo translational and rotational site Jacobians, damped least
squares, bounded joint updates and two initialisations. The tool faces downward;
yaw rotates its horizontal jaw axis. Each solution is followed with smoothstep
joint interpolation and a settling interval. Unreachable or unsafe targets
raise a clear error. `pick_place` opens, approaches above the object, descends,
closes, checks contact, lifts and checks height, transfers above the bin,
lowers, releases, retreats, returns home and checks the final part position.

## Default webcam mapping

`pose_mapping()` returns `map_pose(pose, state) -> {actuator_name: target}`.
`pose_mapping_source()` returns `DEFAULT_MAPPING_SOURCE`, a standalone script
that imports only NumPy and defines the identical mapping with its joint constants.
The runtime exposes this script through `teleop.get_mapping` for editing.
The caller must retain a distinct mutable `state` dictionary per session.
Outputs are normalised actuator positions, ready for `manual_action`.

The mapping uses world landmarks when supplied, otherwise normalised pose
landmarks. Right shoulder/elbow/wrist are 12/14/16; `arm: "left"` selects
11/13/15. MediaPipe y points down. Let `u = elbow - shoulder` and
`v = wrist - elbow`. The six raw angles are:

1. Shoulder yaw: `atan2(u.z, u.x)` (x mirrored for the left arm).
2. Shoulder pitch: `atan2(-u.y, hypot(u.x, u.z))`.
3. Elbow flexion: `acos(dot(u,v) / (norm(u)*norm(v)))`.
4. Wrist pitch: forearm elevation minus upper-arm elevation.
5. Wrist yaw: `atan2(v.z, hypot(v.x, v.y))`.
6. Wrist roll: angle of the hand's index-to-little-finger knuckle vector in xy,
   when the selected hand is present; otherwise zero.

A valid `pose.calibration` subtracts neutral anatomical angles before applying
per-joint gains and offsets. Defaults are `gains=[1,-0.8,1,1,1,1]` and
`offsets=HOME`. Override those six-element arrays in `state` to write a variant.
Hand thumb-tip 4 to index-tip 8 distance is divided by wrist 0 to middle-knuckle
9 distance. Ratios at/below 0.15 close the gripper; at/above 0.8 open it.
Missing hands preserve opening. Missing, degenerate, nonfinite or low-visibility
(<0.5) pose input holds the previous command.

Targets are clamped to physical joint limits. A 0.025 rad dead zone (1 mm for
the gripper) removes jitter, then exponential smoothing uses alpha 0.22 per
sample. Set `state.dead_zone` and `state.smoothing` to customise them;
`state.targets` stores the last smoothed physical positions. Do not share state
between sessions. Webcam retargeting is an illustrative joint mapping, not
human-to-robot Cartesian scale matching.

## Verification

Run from `internal/robotlab/runtime`:

```sh
MUJOCO_GL=osmesa OMP_NUM_THREADS=2 MKL_NUM_THREADS=2 \
  OPENBLAS_NUM_THREADS=2 LP_NUM_THREADS=2 nice -n 15 \
  /home/cristhian/excalibur-sim/venv/bin/python -m pytest -q \
  tests/test_scen_arm.py tests/test_contract.py
```

The arm can be disabled by `EXCALIBUR_ROBOTLAB_ARM=off` (default on); see
`docs/development/agent-quality.md` for switch semantics.

Larger checks are opt-in and do not run in pytest. From the runtime directory,
use a hard wall-clock limit and scratch output outside the checkout:

```sh
MUJOCO_GL=osmesa OMP_NUM_THREADS=2 MKL_NUM_THREADS=2 \
  OPENBLAS_NUM_THREADS=2 LP_NUM_THREADS=2 timeout 1800 nice -n 19 \
  /home/cristhian/excalibur-sim/venv/bin/python -m robotlab.scenarios.arm.verify \
  skills --layouts 40
MUJOCO_GL=osmesa OMP_NUM_THREADS=2 MKL_NUM_THREADS=2 \
  OPENBLAS_NUM_THREADS=2 LP_NUM_THREADS=2 timeout 1800 nice -n 19 \
  /home/cristhian/excalibur-sim/venv/bin/python -m robotlab.scenarios.arm.verify \
  learning --algorithm PPO --steps 300000 --output /home/cristhian/excalibur-sim/scratch/scen-arm
```

The layout check cycles through all eight mark ids, five part classes and three
bins, with domain randomisation and independently seeded layouts. Its process
exits unsuccessfully below 95% verified placements. The learning check compares
the initial and trained deterministic policies on twenty held-out target
seeds (100–119), logging success count, nearest distance, return, CPU and wall
time. PPO is recommended first with a 300,000-step budget. Checkpoints remain
scratch artifacts; no pretrained policy is advertised.

Measured on 2026-11-02 using the shared development interpreter:

- The complete scenario and contract suite passed **35 tests**. The IK check
  reached **20/20** seeded workspace points with less than **10 mm** error,
  and a separate opposite-yaw check passed.
- The larger contact-based skill harness placed **40/40** parts successfully
  over layout seeds 0–39, cycling eight object ids and all three bins. Each
  operation took 700 control frames; the full run took **746.55 s wall time**
  on the heavily shared host at nice 19. This figure covers the documented
  stratified reset layouts. Verification checks bilateral finger contact,
  lift height, released gripper and settled position inside the requested bin.
- Repository checks passed: Go formatting, vet, staticcheck, Linux/Darwin/
  Windows builds, the bare-goroutine check, and the memory-capped race check
  for `internal/robotlab` (which currently contains no Go test files).
- An additional eight-object sorting session completed **8/8 sequential
  placements** into A/B/reject, including bins already holding other parts.

PPO demonstrated reaching on **18/20 held-out goals (90%)** at **221,184 steps**,
with a 25 mm success radius and **21.61 mm** mean nearest distance. It used seed
17, normalised observations, separate 64-by-64 actor/value networks, 1,024-step
rollouts, five update epochs, batch 64, learning rate 0.0003, entropy coefficient
0.001 and initial log standard deviation -2.3. The initial actor bias commands
the known home pose; target-dependent behaviour is learned through rewards.

| Cumulative PPO steps | Goals reached / 20 | Mean nearest distance | Mean episode return |
| --- | --- | --- | --- |
| 0 | 0 | 0.52464 m | -420.16 |
| 100,352 | 3 | 0.08652 m | -78.84 |
| 200,704 | 12 | 0.03331 m | -18.68 |
| 221,184 | 18 | 0.02161 m | -4.08 |

The three measured training/evaluation phases reported **565.92 s CPU** and
**627.38 s wall time** in total, excluding interpreter/model-loading time and
pauses between phases. Each process ran at nice 19 with two Torch/BLAS threads.
The measured run used cumulative budgets 100,000, 200,000 and 300,000; PPO rounds
them to complete rollouts. The first two phases reached their budgets and the
last stopped early after exceeding the 16/20 learning smoke criterion.

PPO saves both `reach-ppo.zip` and `reach-ppo-normalization.pkl`; apply the saved
observation statistics during replay. Use `--resume --algorithm PPO` with the
same output directory and a larger cumulative `--steps` budget to continue a
checkpoint. Both optimiser and observation statistics are restored. The helper
accepts at most 500,000 requested steps and exits unsuccessfully if its learning
criterion is not reached within the budget. Use the documented wall-clock limit
as well as the step budget.

The initial SAC configuration was not verified as successful: at 50,000 steps
it reached 0/20 goals, with mean nearest distance 0.14814 m, before its
1,800-second wall limit. Its 128-by-128 network, learning rate 0.0003, batch 128,
100,000-transition replay buffer and one update per four environment steps used
default automatic entropy tuning. PPO is the verified recipe. SAC continuation
would require replay-buffer handling beyond this small verification helper.


Round-two runtime verification on 2026-11-02 exercises the real sidecar process
with an eight-part `sort` session: scenario discovery, manual gripper control,
marked `top` snapshot, a 700-frame pick/place into A, placement persistence
after resumed simulation, editable default mapping retrieval and six pose-mode
control/frame exchanges. The saved 640x480 PNG was inspected visually: all eight
parts have blue outlines and legible numbered badges above their silhouettes.
The integration check runs as `tests/test_scen_arm_sidecar.py`; images and
sidecar data are generated in pytest scratch directories, never committed.
Mapping tests also compare the exported script and in-process mapping with
right/left poses, world points, calibration, hands, missing/invalid data and
custom gains/smoothing. Both built-in sessions and mapping workers retain
caller-owned smoothing state between samples.
