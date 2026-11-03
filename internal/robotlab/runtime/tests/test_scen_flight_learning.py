"""Manual learning check, excluded from ordinary tests by the main guard.

Run from runtime with OMP/MKL=2, MUJOCO_GL=osmesa, nice 19 and a timeout.
Outputs and checkpoints belong in scratch storage outside the repository.
"""


def main() -> None:
    """Train CPU PPO and report fixed-seed evaluations as JSON lines."""
    import argparse
    import json
    import time
    from pathlib import Path
    parser = argparse.ArgumentParser()
    parser.add_argument('scenario', choices=['drone', 'rocket'])
    parser.add_argument('--steps', type=int, default=100000)
    parser.add_argument('--output-dir', type=Path, required=True)
    parser.add_argument('--envs', type=int, default=8)
    parser.add_argument('--width', type=int, default=32)
    parser.add_argument('--n-steps', type=int, default=256)
    parser.add_argument('--batch-size', type=int, default=256)
    parser.add_argument('--epochs', type=int, default=3)
    parser.add_argument('--eval-every', type=int, default=8192)
    parser.add_argument('--log-std', type=float, default=0.0)
    parser.add_argument('--params', type=json.loads, default={})
    parser.add_argument('--reward-weights', type=json.loads, default={})
    parser.add_argument('--motor-mix', action='store_true')
    parser.add_argument('--evaluate-only', action='store_true')
    parser.add_argument('--fixed-start', action='store_true',
                        help='Use the SDK default reset behavior instead of randomized starts')
    parser.add_argument('--eval-seed', type=int, default=100)
    parser.add_argument('--resume', type=Path)
    args = parser.parse_args()
    if min(args.steps, args.envs, args.width, args.n_steps, args.batch_size, args.epochs, args.eval_every) <= 0:
        parser.error('training counts must be positive')
    if not isinstance(args.params, dict) or not isinstance(args.reward_weights, dict):
        parser.error('params and reward-weights must be JSON objects')
    if args.motor_mix and args.scenario != 'drone':
        parser.error('motor-mix applies only to drone')
    import gymnasium as gym
    import torch
    import numpy as np
    from stable_baselines3 import PPO
    from stable_baselines3.common.vec_env import DummyVecEnv
    from stable_baselines3.common.callbacks import BaseCallback
    from robotlab.scenarios.drone import Drone
    from robotlab.scenarios.rocket import Rocket

    args.output_dir.mkdir(parents=True, exist_ok=True)
    torch.set_num_threads(2)
    factory = Drone if args.scenario == 'drone' else Rocket

    def make_env() -> gym.Env:
        """Keep training and held-out evaluation parameters and rewards identical."""
        params = {**args.params, **({"action_mode": "mixed"} if args.motor_mix else {})}
        return factory(params).make_env(args.reward_weights, randomize=not args.fixed_start)

    training = DummyVecEnv([make_env for _ in range(args.envs)])
    model = PPO('MlpPolicy', training, n_steps=args.n_steps, batch_size=args.batch_size, n_epochs=args.epochs,
                learning_rate=3e-4, policy_kwargs={'net_arch': [args.width, args.width],
                                                'log_std_init': args.log_std}, seed=17,
                device='cpu', verbose=0)
    if args.resume:
        model = PPO.load(args.resume, env=training, device='cpu')

    def evaluate() -> dict:
        """Evaluate the same ten held-out reset seeds at every checkpoint."""
        rows = []
        for seed in range(args.eval_seed, args.eval_seed + 10):
            env = make_env()
            obs, _ = env.reset(seed=seed)
            total = 0.
            errors = []
            for step in range(750):
                action, _ = model.predict(obs, deterministic=True)
                obs, reward, done, truncated, info = env.step(action)
                total += reward
                errors.append(float(np.linalg.norm(obs[:3])))
                if done or truncated:
                    break
            physical = env.unwrapped
            rows.append({'return': total, 'steps': step+1, 'error': float(np.mean(errors)),
                         'max_error': max(errors),
                         'outcome': info.get('outcome', 'crash' if done else 'survived'),
                         'speed': float(np.linalg.norm(physical.data.qvel[:3])),
                         'height': float(physical.data.qpos[2]),
                         'upright': float(physical.data.xmat[physical.body,8])})
            env.close()
        return {'return': float(np.mean([r['return'] for r in rows])),
                'steps': float(np.mean([r['steps'] for r in rows])),
                'error': float(np.mean([r['error'] for r in rows])),
                'survived': sum(r['outcome'] == 'survived' for r in rows),
                'soft_landings': sum(r['outcome'] == 'landed' for r in rows), 'episodes': rows}

    class Progress(BaseCallback):
        """Write a recovery checkpoint and a small progress record every 8,192 steps."""
        def __init__(self) -> None:
            super().__init__()
            self.next_checkpoint = model.num_timesteps + 8192

        def _on_step(self) -> bool:
            if self.num_timesteps >= self.next_checkpoint:
                print(json.dumps({'progress': self.num_timesteps}), flush=True)
                self.model.save(str(args.output_dir / (args.scenario+'-last')))
                self.next_checkpoint = self.num_timesteps + 8192
            return True

    start = time.monotonic()
    print(json.dumps({'configuration': vars(args)}, default=str), flush=True)
    print(json.dumps({'scenario': args.scenario, 'step': model.num_timesteps, 'evaluation': evaluate()}), flush=True)
    remaining = 0 if args.evaluate_only else args.steps
    while remaining > 0:
        before = model.num_timesteps
        model.learn(total_timesteps=min(args.eval_every, remaining), reset_num_timesteps=False, callback=Progress())
        remaining -= model.num_timesteps - before
        model.save(str(args.output_dir / (args.scenario+'-last')))
        model.save(str(args.output_dir / f'{args.scenario}-{model.num_timesteps}'))
        print(json.dumps({'scenario': args.scenario, 'step': model.num_timesteps,
                          'wall_s': time.monotonic()-start, 'evaluation': evaluate()}), flush=True)
    training.close()


if __name__ == "__main__":
    main()
