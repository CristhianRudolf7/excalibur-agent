# Quadcopter

A one-kilogram quadcopter with four rotors, contrasting front/rear colors,
landing skids and goal markers, built entirely from MuJoCo primitives.
Coordinates are meters with z up; control runs at 50 Hz over 200 Hz physics.
Mass scales inertia and the rotor thrust ranges.

`Drone(params).make_env(reward_weights=None, randomize=False)` returns a
Gymnasium environment. The default `action_mode="mixed"` means collective,
roll, pitch and yaw commands in `[-1, 1]`. The allocation is fixed and has no
feedback: the learned policy supplies all stabilization and position control.
For action `[c,r,p,y]`, the normalized motor commands are:

```text
[c + .015r - .015p + .05y, c - .015r - .015p - .05y,
 c - .015r + .015p + .05y, c + .015r + .015p - .05y]
```

Motor commands are clipped to `[-1,1]`, with -1 off, 0 hover thrust and 1
twice hover thrust. Alternating reaction torques provide yaw. Select
`action_mode="raw"` for four independent normalized motor actions. This
allocation lives in the scenario environment, so `sdk.make_env()` and saved
policy playback share exactly the same action convention.

Observation order is goal minus position (3), body quaternion wxyz (4),
world linear velocity (3), and body angular velocity (3). Rewards expose
`distance`, `upright`, `velocity`, `effort`, `waypoint`, and `crash` before
weighting in `info["terms"]`. Effort measures the allocated motor commands.
The default upright weight is 3, matching the successful training recipe.

Parameters: `task` is `hover`, `waypoints`, or `wind`; `mass` is 0.2–5 kg;
`wind_strength` is 0–10 N and applies only to wind. `waypoints` contains
1–32 xyz goals (x/y within ±4 m, z within 0.4–8 m); the default square is
at 1.5 m. Entering a 0.25 m goal radius advances the marker and awards a
bonus. Ground approach below 0.12 m, a flip beyond about 75 degrees, or
leaving x/y ±5 m and z 0–10 m terminates the episode. The horizon is 750
steps (15 s). `randomize=True` adds seeded position/velocity disturbances.

`manual_mode="assisted"` maps `motor1` to throttle (W/S), `motor2` to yaw
(D/A), `motor3` to pitch (up/down), and `motor4` to roll (right/left). Call
`scenario.manual_action(env, inputs)` per step. An attitude/velocity controller
mixes human commands into physical thrusts and retains an altitude target.
`manual_mode="raw"` gives direct per-motor sliders with the same four names,
independently of `action_mode`. Manual actions use the inverse allocation;
they can exceed the policy Box internally when stabilization needs more torque,
while physical motor commands remain clipped. Release the keys to stabilize.

`EXCALIBUR_ROBOTLAB_FLIGHT=off` rejects environment creation; it defaults ON.
See `docs/development/agent-quality.md` for rollback behavior.

## Training and bundled hover policy

The default budget is **1,048,576 steps** (`default_total_steps`). The
recommended PPO settings are the SDK's stock CPU defaults: one environment,
[64,64] network, 256 rollout steps, batch size 64, four epochs, learning rate
0.0003 and initial log standard deviation 0. Use seed 17 to reproduce the
measurement. Parameters, reward weights and mixed actions are defaults; the
standard `sdk.make_env()` / `sdk.algorithm("PPO", env)` script needs no action
wrapper or custom controller.

`pretrained/hover.zip` is the original 154,445-byte SB3 PPO checkpoint from
this run. `has_pretrained=True`; `pretrained_policy()` returns its path for
the mixed hover task and `None` for raw actions or other tasks. There is no
claim of pretrained waypoint or wind navigation. The model is training output,
not a downloaded artifact.

Training initially used 655,360 steps, then continued for 393,216 steps.
The first phase took 784.20 seconds and the continuation 538.50 seconds, under
`nice -n 19`, with two threads and hard limits of 1200 and 700 seconds.
This measurement used randomized starts during training and evaluation.
`Drone.training_randomize=True` opts the SDK into that verified reset
distribution. Other scenarios retain the SDK's existing fixed-start default;
direct `Drone().make_env()` still defaults to `randomize=False`.
The SDK change is an optional attribute lookup and does not alter the scenario
interface or other scenarios' behavior.
All evaluations below use deterministic actions and the final default rewards.
Mean error averages goal distance across all control steps.

| Policy | Reset seeds | Mean return | Mean steps | Mean goal error | Full flights |
| --- | --- | --- | --- | --- | --- |
| Untrained stock PPO | 100–109 | 413.55 | 497.8 | 0.925 m | 0/10 |
| Stock PPO, 655,360 steps | 100–109 | 920.05 | 647 | 0.672 m | 4/10 |
| Stock PPO, 917,504 steps | 100–109 | 1752.43 | 750 | 0.327 m | 10/10 |
| Bundled stock PPO, 1,048,576 steps | 100–109 | 2128.10 | 750 | 0.079 m | 10/10 |
| Bundled policy, fresh seeds | 200–209 | 2141.75 | 750 | 0.069 m | 10/10 |

All twenty final-checkpoint flights completed the 15-second horizon. On fresh
seeds, maximum goal error was 0.1385 m and final heights were 1.490–1.518 m.
The 917,504-step checkpoint also completed the SDK's fixed-start horizon with
0.292 m mean error; fixed-start seed changes do not produce independent starts.
The bundled model uses the longer budget because the 655,360-step stock run
was not yet a reliable hover controller. A separate fixed-start training check
reached 524,288 steps without a full-horizon flight (mean 417 steps and
1.097 m error). It was stopped once the SDK was aligned with the verified
randomized distribution; it is not evidence of fixed-start learnability.

Reproduce from `internal/robotlab/runtime`, keeping training outside tests:

```sh
PYTHONPATH=. MUJOCO_GL=osmesa OMP_NUM_THREADS=2 MKL_NUM_THREADS=2 \
  OPENBLAS_NUM_THREADS=2 OMP_WAIT_POLICY=PASSIVE \
  nice -n 19 timeout 1800 /home/cristhian/excalibur-sim/venv/bin/python \
  tests/test_scen_flight_learning.py drone --steps 1048576 --eval-every 131072 \
  --envs 1 --width 64 --n-steps 256 --batch-size 64 --epochs 4 \
  --output-dir /home/cristhian/excalibur-sim/scratch/scen-flight/drone-reproduce
```

The measured two-phase run can be reproduced with `--steps 655360`, then
`--steps 393216 --resume /path/to/drone-last.zip`. For read-only verification,
add `--evaluate-only --resume robotlab/scenarios/drone/pretrained/hover.zip`;
`--eval-seed 200` selects fresh evaluation seeds. `--fixed-start` matches the
direct scenario factory's `randomize=False` reset behavior. Checkpoints are stored at each
evaluation interval. `--motor-mix` remains an alias selecting
`action_mode=mixed`; it does not wrap or allocate twice.

An earlier eight-environment PPO recipe used [64,64], 256 rollout steps per
environment, batch size 256, ten epochs, initial log standard deviation -1
and the same default reward weights. At 655,360 steps it completed 10/10
horizons with 0.181 m mean error. That checkpoint is retained in external
scratch storage; the more accurate stock-PPO checkpoint is bundled.
Previous independent raw-motor training at 501,760 steps ended at mean 70.8
steps and 0/10 full flights despite improved returns; it did not learn hover.
Use `--params '{"action_mode":"raw"}'` explicitly for such experiments.

## Verification

From the runtime directory, use the shared interpreter with
`MUJOCO_GL=osmesa OMP_NUM_THREADS=2 MKL_NUM_THREADS=2 nice -n 15`:
`python -m pytest -q tests/test_scen_flight.py tests/test_scen_flight_sidecar.py tests/test_contract.py`.
Coverage includes both action modes, all task/manual variants, mass extremes,
2,000 random steps, assisted hover, wind, goal progression, crashes, reward
weighting, switch-off, 1,024-step PPO smoke runs, seeded bundled-policy hover,
and real JSON-lines sidecar discovery/manual input/rendering/pretrained playback.
