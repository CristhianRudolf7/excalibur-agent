# Cart and poles

A cart slides along a rail and pushes one, two or three serial poles. Choose
`start="upright"` to balance, or `start="hanging"` to learn swing-up. Parameters
are `links` (default 1), `pole_length` (meters per link, default 0.6), `cart_mass`
(kg, default 1), and `max_force` (newtons, default 10). Numeric limits appear in
`CartPole.params_schema`; invalid and unknown parameters are rejected.

The normalized action has shape `(1,)`: `cart_force=-1` pushes left and `+1`
pushes right. ArrowLeft/ArrowRight and the slider drive the same input. Zero is
idle. The continuous action is compatible with the SDK DQN discretizer.
Observation order is cart position, sine of each absolute pole angle, cosine of
each absolute pole angle, and generalized velocities. Control runs at 50 Hz
with four 5 ms implicit physics steps. No pole collisions are needed for this
constrained mechanism. The rail height adapts to keep hanging poles above the floor.

Reward is the weighted sum of `upright` (mean cosine, weight 1), `centered`
(exp(−cart_position²), weight 0.1), `effort` (negative squared normalized force,
weight 0.001), and `alive` (1 per step, weight 1). Maximum episode return is
2100 over 1000 steps. Upright episodes end when any absolute pole angle exceeds
45 degrees or the cart exceeds ±2.4 m. Swing-up episodes only end at the time
limit; soft rail stops remain active at ±2.65 m.

Every reset adds small seeded position/velocity perturbations. With
`randomize=True`, masses, corresponding inertias and friction coefficients vary
independently by ±15% from their original values, without accumulating changes.
`EXCALIBUR_ROBOTLAB_CLASSIC=off` disables environment creation.

The default is one link, so a first PPO experiment with the default 60,000 steps learns
in a few minutes. Raise `links` for the harder coupled system and budget roughly
500,000–1,000,000 steps for two links and 1–3 million for three links, with seed
sweeps and tuning; these are planning budgets, not measured learning guarantees.
Swing-up is harder and needs a separate longer budget. Recommended alternatives
are SAC, A2C, and DQN through the SDK discretizer.

## Local learning check

Measurements use SB3 PPO with `MlpPolicy`, seed 7, CPU, `n_steps=1024`,
`batch_size=64`, `n_epochs=10`, and `learning_rate=3e-4`. Set
`OMP_NUM_THREADS=2 MKL_NUM_THREADS=2 MUJOCO_GL=osmesa`, call
`torch.set_num_threads(2)`, and run with `timeout 1500s nice -n 19`. Save policies
outside the repository. Evaluate deterministically on reset seeds 100–109,
comparing with zero-force control. The 1000-step unit smoke test checks the
training path only; it does not claim convergence.

Measured on the shared CPU host: 51,200 steps in 209.40 seconds (3.49 minutes)
reached mean return **2096.55 / 2100**, with all ten evaluation episodes lasting
1000 steps. Zero force scored 129.43 over 62.6 steps on the same seeds. At
25,600 steps the policy scored 1093.94, showing intermediate learning progress.
The 60,000-step default leaves a small margin beyond the observed convergence
point and targets roughly 2–5 minutes on two CPU threads; host contention and
seeds can change that. These measurements cover one upright link only.

Verification: all 100 classic-scenario and shared-contract checks passed across the
suite and an additional plate reward regression, including
all 72 parameter boundary combinations at 2000 random actions each and both
1000-step PPO integration checks. The final suite took 54.01 seconds on the loaded host, with another 1.88 seconds
for the added reward regression; ordinary tests do not perform the longer
convergence run.

Continuing to 102,400 steps took 360.05 seconds and scored 2098.77 / 2100,
with all ten episodes still lasting the full 1000 steps.
