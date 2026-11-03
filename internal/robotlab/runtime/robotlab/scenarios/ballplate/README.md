# Ball on a plate

Two position-controlled gimbal axes tilt a square plate under a rolling ball.
Choose `task="center"` for the origin, `"target"` for a seeded stationary marker,
or `"circle"` for a moving marker with an eight-second period. Parameters are
`plate_size` (full side length in meters, default 1.2), `ball_radius` (meters,
default 0.045), and `friction` (sliding coefficient, default 0.6). Numeric limits
appear in `BallPlate.params_schema`; invalid and unknown parameters are rejected.

Actions are `(tilt_x, tilt_y)` normalized to ±1, corresponding to ±0.2 radians.
W/S controls positive/negative X-axis rotation; D/A controls positive/negative
Y-axis rotation. Sliders use the actuator names directly. Zero requests a level
plate. The axes have damping, torque limits and soft angular stops. Control runs
at 50 Hz with four 5 ms implicit physics steps and rolling contact friction.

Observations contain plate-local ball XY position (scaled by plate half-size),
ball XY world velocity rotated into plate coordinates, target XY and target
error (both scaled by half-size), gimbal angles and velocities, previous action,
and sine/cosine of the circle phase. The gold target is a non-colliding primitive
on a moving visual body, so the web viewer receives its pose in normal frames.

Reward terms are `distance` (negative ball-to-target distance in meters, weight
4), `on_plate` (1 while supported, weight 1), `smooth` (negative squared action
change, weight 0.01), and `effort` (negative squared action, weight 0.001).
The survival weight keeps ordinary center-task steps positive, avoiding an
incentive to drop the ball to escape distance penalties. Episodes end when the ball clears an edge or falls below the plate, or truncate
after 500 steps (ten simulated seconds). Resets perturb position and velocity;
`randomize=True` additionally varies masses, matching inertias and friction by
±15% from original values. Randomization is seeded and never accumulates.
`EXCALIBUR_ROBOTLAB_CLASSIC=off` disables environment creation.

## Local learning check

Start with the center task and a 200,000-step PPO budget. Measurements use SB3
PPO `MlpPolicy`, seed 7, CPU, `n_steps=1024`, `batch_size=64`, `n_epochs=10`,
`learning_rate=3e-4`. Set `OMP_NUM_THREADS=2 MKL_NUM_THREADS=2 MUJOCO_GL=osmesa`,
call `torch.set_num_threads(2)`, and run under `timeout 1500s nice -n 19`, saving
artifacts outside the repository. Evaluate deterministically on reset seeds
100–109 and compare return, episode length and mean distance against zero tilt.
The 1000-step unit smoke test checks integration rather than learning quality.
The target and circle tasks need separate training; a center policy does not
establish tracking performance.

Measured on the shared CPU host: 76,800 steps in 217.34 seconds reached mean
return **455.85**, with all ten evaluation episodes lasting 500 steps. Mean
ball-to-target distance fell from **0.1773 m** with zero tilt to **0.0219 m**.
At 102,400 steps (275.73 seconds), return reached **482.21 / 500** and mean
distance fell to **0.00887 m**, again over ten full-length episodes. The same
reset seeds and default reward weights were used for both policies. The
200,000-step default is a conservative experiment budget rather than a minimum
needed for the center task.

Verification: 99 classic-scenario and shared-contract tests passed in 54.01
seconds, plus the deliberate-drop reward regression passed in 1.88 seconds.
This covers all 72 parameter boundary combinations at 2000 random actions each,
manual inputs, seeded randomization, reward signs, termination and both
1000-step PPO smoke runs. Longer learning checks run outside ordinary tests.
