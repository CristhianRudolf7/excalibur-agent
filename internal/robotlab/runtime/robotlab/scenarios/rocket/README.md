# Rocket landing

A silver and orange vertical lander with four capsule legs, a two-axis hinged
main engine and attitude jets. The dark launch field contains a circular pad
with a mint cross. All geometry uses primitives. The model has 1.5 kg total
mass, a 200 Hz physics timestep and 50 Hz control.

`Rocket(params).make_env(reward_weights=None, randomize=False)` creates a
Gymnasium environment. Five normalized actions control `thrust`, `gimbal_x`,
`gimbal_y`, `attitude_x`, and `attitude_y`. Main throttle maps [-1, 1] to
[0, maximum thrust]; gimbal commands map to ±0.25 radians; attitude jets map
to ±0.35 Nm. Maximum thrust is 2.5 times local weight, with a 10 N minimum
for low gravity. W/S control throttle, arrows control the gimbal, Q/E control
roll jets and A/D control pitch jets. The engine gimbal is physical: hinged
bodies redirect the thrust site rather than applying an artificial force.

Parameters are `start_height` (1.5–20 m), `wind` (0–10 N gust amplitude),
`fuel` (0–1,000 N s impulse budget), and `gravity` (`earth`, `moon`, `mars`).
Reset always samples small seeded lateral offsets, velocities and tilts;
`randomize=True` increases their ranges. Observations contain pad-relative
position with a 0.905 m touchdown height (3), quaternion wxyz (4), linear and
angular velocities (6), gimbal angles (2), gimbal velocities (2), and fuel
fraction (1). Named unweighted reward terms are `distance_to_pad`, `speed`,
`tilt`, `fuel`, `landed`, and `crash`.

Engine impulse and equivalent attitude-jet impulse consume fuel at every
physics substep. Partial final thrust spends exactly the remaining budget.
Touchdown stops the episode on its first ground contact. A landing is soft
only inside a 0.65 m center radius with upright cosine ≥0.97, vertical speed
≤0.8 m/s, lateral speed ≤0.5 m/s, and angular speed ≤0.5 rad/s. Pre-impact
velocity prevents the contact solver from disguising a hard landing. All
other contacts are crashes. Fuel exhaustion above 1.5 m and flight outside
x/y ±15 m or above 30 m also terminate with a penalty. The horizon is 750
steps. `info` includes `outcome` and `fuel_remaining`.

`EXCALIBUR_ROBOTLAB_FLIGHT=off` rejects environment creation; it defaults on.
See `docs/development/agent-quality.md` for switch and rollback details.

## Verification

The shared flight test suite covers all gravity modes and parameter extremes,
2,000 finite random steps, seeded resets, real soft/hard/off-pad/tilted contact,
exact fuel depletion, reward weighting, switch-off, and a 1,024-step PPO smoke
run. Run the command in the drone README. Separate bounded learning results
are recorded below after measurement; the smoke test is not a convergence
claim.

## Learning measurement (2026-11-02)

CPU PPO used seed 17, one environment, a [64, 64] policy, 1,024 rollout
steps, batch size 128, five epochs and learning rate 0.0003. Ten deterministic
evaluation episodes used randomized starts with reset seeds 100–109 and the
default Earth, 5 m, 180 N s scenario. Speed is the total linear speed at the
end of each episode, averaged across those ten episodes.

| Training steps | Mean return | Mean episode steps | Mean pad error | Final speed | Soft landings |
| --- | --- | --- | --- | --- | --- |
| 0 | -753.69 | 224.1 | 12.927 m | 12.36 m/s | 0/10 |
| 32,768 | -241.52 | 116.5 | 3.464 m | 7.82 m/s | 0/10 |
| 49,152 | -207.58 | 95.5 | 3.064 m | 6.31 m/s | 0/10 |
| 100,352 | -382.59 | 118.1 | 4.607 m | 9.75 m/s | 0/10 |

This establishes improvement in the approach, including substantially lower
pad error and impact speed. **Soft landing is not established.** The initial
policy flew out of bounds; all ten episodes at 49,152 steps reached the ground
but were classified as crashes. Later training regressed, so the final policy
must not be described as a successful lander. The 1,024-step ordinary test is
only a compatibility smoke test. No pretrained policy is advertised.

A separate run with eight environments, a [64, 64] policy, 256 rollout steps,
batch size 256, three epochs and initial log standard deviation -2 reached
153,600 evaluated steps: mean return -600.03, mean pad error 7.732 m, final
speed 23.44 m/s, and 0/10 soft landings. It was stopped after preserving a
checkpoint because this configuration did not improve landing. Initial
20-minute attempts under host load above 80 timed out before a checkpoint;
the measurements above came from subsequent completed training intervals.

Reproduce outside ordinary tests from the runtime directory:

```sh
PYTHONPATH=. MUJOCO_GL=osmesa OMP_NUM_THREADS=2 MKL_NUM_THREADS=2 \
  OPENBLAS_NUM_THREADS=2 OMP_WAIT_POLICY=PASSIVE \
  nice -n 19 timeout 1200 /home/cristhian/excalibur-sim/venv/bin/python \
  tests/test_scen_flight_learning.py rocket --steps 100000 \
  --envs 1 --width 64 --n-steps 1024 --batch-size 128 --epochs 5 \
  --output-dir /home/cristhian/excalibur-sim/scratch/scen-flight/rocket
```

PPO rounds step counts up to complete rollouts. The runner evaluates every
8,192 steps by default and saves recovery checkpoints at that cadence. Pass
`--resume /path/to/rocket-last.zip` to continue, `--params '{"start_height":1.5}'`
for an explicit curriculum stage, or `--reward-weights '{"crash":200}'` for an
explicit reward experiment. Training and evaluation use identical parameters
and reward weights. All checkpoints and logs belong outside the repository.
The original training preceded the 2 mm ground/pad visual separation. A
read-only reevaluation of the 100,352-step checkpoint on final geometry returned
-382.59 with mean pad error 4.607 m, final speed 9.74 m/s and 0/10 soft landings,
confirming the improvement persists. The final geometry also passed the full
physics and touchdown classification suite. Use `--evaluate-only --envs 1
--resume /path/to/rocket-last.zip` to verify a saved checkpoint without training.

## Bounded landing curriculum (review round 2, 2026-11-02)

One additional PPO attempt started at 1.5 m, continued at 2.5 m, then raised
start height to the default 5 m. Training used seed 17, eight environments,
a [64,64] policy, 256 rollout steps, batch size 256, ten epochs, learning rate
0.0003 and initial log standard deviation -1. Starts were randomized at every
stage. Reward weights were `speed=2`, `tilt=10`, `crash=200`, `landed=500`;
other terms retained their defaults. Only the experiment reward weights and
start height changed; the scenario's default rewards and dynamics did not.

Each evaluation used ten deterministic episodes with reset seeds 100–109.
Mean pad error is the mean per-step distance to the landing configuration;
final speed includes the post-contact solver response and is not used to
classify touchdown (classification uses pre-impact speed).

| Policy / stage | Total steps | Training seconds | Mean return | Mean pad error | Final speed | Soft landings |
| --- | --- | --- | --- | --- | --- | --- |
| Untrained at 5 m | 0 | — | -3380.61 | 12.927 m | 12.36 m/s | 0/10 |
| 1.5 m curriculum | 200,704 | 363.51 | -433.06 | 0.733 m | 2.41 m/s | 0/10 |
| 2.5 m curriculum | 301,056 | 180.90 | -439.13 | 1.168 m | 1.74 m/s | 0/10 |
| 5 m curriculum | 401,408 | 160.48 | -1742.41 | 4.401 m | 9.55 m/s | 0/10 |

The complete attempt took 14 minutes 38 seconds wall time, including stage
transitions, and 704.89 seconds of measured training. Every process used
`nice -n 19` and two CPU threads. The first stage had a 3500-second hard
limit; after it finished in 364 seconds, each continuation had a 600-second
hard limit, keeping the total below the 60-minute budget.

**This did not learn soft landing.** At 5 m the shaped return improved over
the untrained baseline, but all ten episodes crashed. The 2.5 m improvement
did not transfer reliably to default height. Read-only evaluation of the final
checkpoint with the scenario's default reward weights returned -345.57,
mean episode length 130.4, mean pad error 4.401 m and 0/10 soft landings.
The required 5/10 threshold was not met, so no `pretrained/land.zip` is bundled
and `has_pretrained` remains false. The sidecar correctly reports `not_found`
for pretrained rocket playback; manual control and snapshots pass.

Reproduce the attempt from the runtime directory. Use the shared interpreter,
`PYTHONPATH=. MUJOCO_GL=osmesa OMP_NUM_THREADS=2 MKL_NUM_THREADS=2
OPENBLAS_NUM_THREADS=2 OMP_WAIT_POLICY=PASSIVE`, and run each command under
`nice -n 19 timeout 600` (the measured first stage also fits this bound):

```sh
python tests/test_scen_flight_learning.py rocket --steps 200000 \
  --params '{"start_height":1.5}' \
  --reward-weights '{"speed":2,"tilt":10,"crash":200,"landed":500}' \
  --width 64 --epochs 10 --log-std -1 --eval-every 50000 \
  --output-dir /home/cristhian/excalibur-sim/scratch/scen-flight/rocket-curriculum

python tests/test_scen_flight_learning.py rocket --steps 100000 \
  --params '{"start_height":2.5}' \
  --reward-weights '{"speed":2,"tilt":10,"crash":200,"landed":500}' \
  --resume /home/cristhian/excalibur-sim/scratch/scen-flight/rocket-curriculum/rocket-last.zip \
  --eval-every 50000 \
  --output-dir /home/cristhian/excalibur-sim/scratch/scen-flight/rocket-curriculum-stage2

python tests/test_scen_flight_learning.py rocket --steps 100000 \
  --params '{"start_height":5}' \
  --reward-weights '{"speed":2,"tilt":10,"crash":200,"landed":500}' \
  --resume /home/cristhian/excalibur-sim/scratch/scen-flight/rocket-curriculum-stage2/rocket-last.zip \
  --eval-every 50000 \
  --output-dir /home/cristhian/excalibur-sim/scratch/scen-flight/rocket-curriculum-stage3
```
