# Quadruped

A free-floating blue robot with four three-joint legs, built entirely from
boxes, capsules and spheres. The torso and head weigh 5.3 kg; each leg weighs
0.96 kg. Twelve bounded position servos use 65 N m/rad stiffness, 3 N m s/rad
damping and a 25 N m force limit. Contact dynamics run at 500 Hz and control
runs at 50 Hz. Zero action holds a bent-knee standing pose.

Parameters are `task` (`walk`, `turn`, `push_recovery`), `target_speed`
(0–2 m/s), `terrain` (`flat`, `bumpy`, `stairs`) and `push_force` (0–150 N).
Obstacles start ahead of the spawn area. Bumps are deterministic small boxes;
stairs rise by 2.5 cm per step. Push recovery applies seeded horizontal force
pulses lasting 120 ms, separated by 1.5–3 seconds.

`QuadrupedScenario.make_env(reward_weights=None, randomize=False,
render_mode=None)` returns a Gymnasium `QuadrupedEnv`. Reset accepts a seed;
randomization adds small joint and velocity perturbations. Episodes end on a
low torso or excessive tilt, and truncate after 20 seconds.

The 48-element float32 observation is torso height (1), gravity in body frame
(3), linear and angular velocities (6), joint offsets (12), joint velocities
(12), previous normalized action (12), and forward/yaw commands (2).
Actions are twelve normalized position offsets ordered front-left,
front-right, rear-left, rear-right, with abduction, hip flexion and knee for
each leg. Offset scales are 0.3, 0.6 and 0.7 radians respectively. Rewards
expose all seven named unweighted terms in `info["terms"]` and accept custom
weights through the shared factory.

`manual_action(env, inputs)` accepts slider values under the twelve actuator
names (`fl_abduction`, `fl_hip`, `fl_knee`, then `fr`, `rl`, `rr`). Arrow key
values use the browser codes `ArrowUp`, `ArrowDown`, `ArrowLeft`, `ArrowRight`
and range 0–1. Up/down set stride direction; left/right change the two sides'
stride lengths. The helper ramps up over 0.4 seconds and uses two-link inverse
kinematics to trace lifted swing and grounded stance paths at 1.8 Hz. It is an
open-loop diagonal trot, without balance or
obstacle avoidance. Sliders override the helper per joint. With no input the
robot stands. Keyboard callers should send raw arrow key values to this hook.

`EXCALIBUR_ROBOTLAB_QUADRUPED=off` disables environment creation. Metadata
remains available for discovery. See `docs/development/agent-quality.md` for
switch semantics. The bundled PPO policy is described in `pretrained/README.md`.
