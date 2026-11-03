# Bundled walking policy

`walk.zip` is an SB3 PPO policy for `quadruped` with `task=walk`,
`terrain=flat` and `target_speed=0.5`. It uses the standard 48-element
observation and twelve joint-position offsets. Playback needs no gait script,
clock, observation wrapper or normalization statistics.

The selected checkpoint has **280,000 cumulative environment steps** and is
220,915 bytes. A deterministic evaluation from reset seed 0 covers 500 control
steps (10 seconds): **2.858623 m forward**, **0.285862 m/s mean speed**,
**2061.619985 return**, with no fall. These are measurements in this scenario,
not claims about other terrains or commands. The acceptance test loads this
archive and requires at least 1 m forward without falling in ten seconds.

Training used MuJoCo 3.17.0, Gymnasium 1.4.0, Stable-Baselines3 2.9.2,
Torch 2.15.0+cpu and seed 7. PPO used four environments, a 64-by-64 Tanh MLP,
1,024 rollout steps per environment, batch size 256, five epochs per update,
learning rate 0.0003, gamma 0.99, GAE lambda 0.95, clip range 0.2 and entropy
coefficient 0.003. Only the built-in scenario rewards were used by PPO.

The first run stopped at 18,556 steps. A resumed run reached 261,616 steps but
remained nearly stationary. The final run initialized the actor from eight
ten-second keyboard-trot demonstrations, with small action perturbations and
randomized resets: 4,000 observation/action examples, 200 supervised epochs,
batch size 256 and Adam learning rate 0.001. Exploration log standard deviation
was then set to -2.3. PPO continued from that initialization; the selected
checkpoint includes another 18,384 environment steps and improved evaluation
return from 1980.61 to 2061.62. Demonstrations are collected during training and
are not stored in or required by the archive.

All three invocations ran sequentially at nice priority 19 with Torch limited
to two threads, outside tests. Total process wall time was approximately
**47 minutes 15 seconds**: 15:24 in the first run, 28:52 in the resumed run,
and 2:59 in the final run, including initialization and evaluation. Shared-host
contention accounts for much of the initial elapsed time. Training stopped
early after meeting acceptance, below the three-hour total limit. Scratch
checkpoints and logs stayed outside the repository.

The final invocation, from `internal/robotlab/runtime`, was:

```sh
MUJOCO_GL=osmesa OMP_NUM_THREADS=2 MKL_NUM_THREADS=2 \
  timeout --signal=INT --kill-after=30s 7100s nice -n 19 \
  /home/cristhian/excalibur-sim/venv/bin/python \
  -m robotlab.scenarios.quadruped.train \
  --output /home/cristhian/excalibur-sim/scratch/scen-quadruped/run3 \
  --steps 1500000 \
  --resume /home/cristhian/excalibur-sim/scratch/scen-quadruped/run1/last.zip \
  --warm-start-gait
```

It was interrupted cleanly after acceptance; `best.zip` at step 280,000 was
selected, while `last.zip` preserved the interrupted run. To train from scratch,
omit `--resume`, retain `--warm-start-gait`, and choose an external output
directory and a timeout within the remaining total training budget.

SHA-256: `bf425a171e78c2de836bc872f0092a9517b7f1bbe73c642ccb0018c28adbfb25`.
