---
name: robot-lab
description: Run physics and reinforcement-learning experiments in Robot Lab, compare policies, map webcam poses, and guide the robot arm with vision.
metadata:
  excalibur:
    group: ml
    version: 1.0.0
    platforms:
    - linux
    tools:
    - robot_lab
---
# Robot Lab

## When to use

Use Robot Lab for simulated robot control, reinforcement-learning comparisons,
webcam pose mapping, or vision-guided arm manipulation.

Discover `robot_lab` with `search_tools`. Its actions share one flat schema;
provide only fields relevant to the chosen action. Retain session, experiment
and run IDs. If unavailable, explain the Install card and prerequisites to the
user; the agent does not install Robot Lab automatically.

## Procedure

### Plan an experiment

Call `scenarios` before choosing parameters. Choose cartpole for balancing
(1–3 links), ballplate for continuous stabilization, arm for reach/pick_place/sort,
quadruped for locomotion, drone for flight, or rocket for controlled landing.
Use the returned parameter schema, reward terms and recommended algorithms.
Translate the user's goal into an observable outcome, select an algorithm,
set a bounded `total_steps` budget and choose `reward_weights` by returned term
names. Explain what each weight encourages and the tradeoffs it creates.

Create one experiment with named runs to compare algorithms side by side:

```json
{"action":"create_experiment","name":"Balance comparison","scenario":"cartpole","params":{"links":1},"total_steps":10000,"runs":[{"label":"PPO baseline","algorithm":"PPO"},{"label":"A2C baseline","algorithm":"A2C"}]}
```

Keep scenario, parameters, steps and reward weights the same across runs.
At most two runs train concurrently. Use `start` once, then `status` and
`metrics` with `experiment` and optional `run`/`since`; retain the returned
`next` cursor for incremental reads. `stop` interrupts the experiment.
Do not restart a run merely because a tool call timed out: inspect status first.

### Write the training script

`read_code` retrieves the current experiment script; `write_code` replaces it
and checks syntax. Every run executes that same script with its own context.
When code is omitted at creation, Robot Lab supplies an algorithm script.
A standard SDK script is:

```python
from robotlab import sdk
ctx = sdk.context()
env = sdk.make_env()
model = sdk.algorithm(ctx.algorithm, env, **ctx.hyperparams)
model.learn(ctx.total_steps, callback=sdk.callback())
sdk.save(model)
```

For a from-scratch algorithm, implement the policy and update rule explicitly
using NumPy or Torch. Follow Gymnasium reset/step semantics: reset returns
observation/info; step returns observation/reward/terminated/truncated/info.
Reset on either episode end. Keep training bounded by `ctx.total_steps`, log
completed episode returns with `sdk.log(step=..., episode_return=...)`, render
occasional evaluations with `sdk.preview(env, policy_fn)`, and save via
`sdk.save_policy(policy_fn_or_state)`. Scripts run without network access and
write only in the experiment directory; do not download packages or datasets.

### Read and explain results

Use the per-run steps, last return, best return and trend from `metrics`.
Return means accumulated reward per episode; a higher value reflects the chosen
reward, which may differ from the user's real goal. Best is one observed peak,
last is recent behavior, and trend compares the first and last available return
in the requested window. Missing returns mean insufficient observations.
Compare at similar step counts. Explain instability, early improvements and
plateaus in everyday terms, distinguishing observations from hypotheses.
One short run does not establish reliability; propose repeated seeded trials
when appropriate. Open a session, then `load_policy` with session, experiment,
run and `checkpoint:"best"` or `"last"` to inspect actual behavior.

### Webcam teleoperation

Open the scenario and use `set_teleop_mapping` with session and Python `code`
defining `map_pose(pose, state)`. It returns actuator inputs in [-1,1], using
actuator names returned by `scenarios`. The pose has 33 normalized landmarks
[x,y,z,visibility], optional world positions, hands, selected arm and calibration.
Map relative displacement from neutral calibration to bounded actuator values;
use visibility thresholds, dead zones and state for smoothing. Handle absent
hands/world/calibration and low visibility by returning neutral inputs. Do not
assume image coordinates are meters. Passing `code:null` restores the scenario
mapping. The browser sends landmarks; webcam video stays on the user's device.
Use `control` mode `pose` to enable mapping and `paused` to stop motion.

### Vision-guided manipulation

Open an arm sort/pick_place session and call `vision_step` with session and a
specific instruction, for example placing one red object in bin A. The arm
uses bin A (blue), bin B (green), and reject (red, marked X).
The loop takes a marked top-camera snapshot, asks the vision model for a strict
JSON decision, executes one pick_place, then takes another image to verify.
Each call performs at most one move. Inspect `verified` before claiming success;
false or absent verification is not a successful placement. A null action means
no move was selected. Model interpretations can be wrong; never infer classes
from hidden sidecar labels. `snapshot` returns only numbered mark boxes to the
agent; images and phase updates are shown in the vision panel. If vision is
unavailable, explain that an image-capable provider and vision model must be
configured. Do not substitute guessed object classifications.

## Pitfalls

Changing reward weights between runs makes return values incomparable. A single
best checkpoint can hide unstable training; inspect last return and playback too.
Keep webcam values normalized and bounded, and use bin IDs rather than colors
in vision decisions. A tool timeout does not prove that a training run stopped.

## Verification

Check script syntax results before starting, then confirm run status and advancing
step counts. Compare metrics at similar budgets and play back a saved checkpoint
to inspect the intended physical behavior. For teleoperation, check neutral pose,
low visibility and bounds before enabling movement. For vision, inspect the
returned verification flag and the second snapshot before reporting placement.
