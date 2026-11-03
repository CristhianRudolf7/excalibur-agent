"""Experiment API for SB3 and from-scratch CPU algorithms.

Custom policies are saved as ``policy.pkl`` (cloudpickle) or ``policy.pt``
(torch state). Callables replay directly. For state playback, place ``policy.py`` in the
run or experiment folder exposing ``act(obs)``. Load the saved state relative to ``__file__``;
the sidecar calls this function deterministically for each observation.
"""
from __future__ import annotations

from dataclasses import dataclass
import importlib
import json
import os
from pathlib import Path
import time
from typing import Any

import gymnasium as gym
import numpy as np

from .algorithms import DEFAULTS


@dataclass(frozen=True)
class Context:
    """Immutable run configuration supplied by sdk_runner."""
    experiment: str
    run: str
    scenario: str
    params: dict
    reward_weights: dict
    total_steps: int
    algorithm: str
    hyperparams: dict
    run_dir: Path
    experiment_dir: Path
    started_at: float

    @property
    def experiment_id(self) -> str:
        return self.experiment

    @property
    def run_id(self) -> str:
        return self.run


def context() -> Context:
    """Read the runner's explicit configuration; no process-global SDK state."""
    directory = Path(os.environ['EXCALIBUR_ROBOTLAB_EXPERIMENT_DIR'])
    experiment = json.loads((directory / 'experiment.json').read_text())
    run_id = os.environ['EXCALIBUR_ROBOTLAB_RUN_ID']
    run = next(run for run in experiment['runs'] if run['id'] == run_id)
    return Context(experiment['id'], run_id, experiment['scenario'], experiment['params'],
                   experiment['reward_weights'], experiment['total_steps'], run['algorithm'],
                   dict(run['hyperparams']), directory / 'runs' / run_id, directory,
                   float(os.environ.get('EXCALIBUR_ROBOTLAB_RUN_STARTED', time.time())))


def load_scenarios() -> dict:
    """Discover installed scenarios plus explicit extra module imports.

    EXCALIBUR_ROBOTLAB_SCENARIO_MODULES is a comma-separated import list.
    Modules may live on PYTHONPATH; this also permits small integration fixtures.
    """
    from . import registry
    for name in os.environ.get('EXCALIBUR_ROBOTLAB_SCENARIO_MODULES', '').split(','):
        if name.strip():
            importlib.import_module(name.strip())
    return registry.load_all()


def make_env() -> gym.Env:
    """Create the configured scenario with weighted rewards and episode Monitor."""
    from stable_baselines3.common.monitor import Monitor
    from . import registry
    ctx = context()
    load_scenarios()
    scenario = registry.get(ctx.scenario)(ctx.params)
    options = {"randomize": True} if getattr(scenario, "training_randomize", False) else {}
    return Monitor(scenario.make_env(reward_weights=ctx.reward_weights, **options))


def discrete_action(index: int, dimensions: int) -> np.ndarray:
    """A small action lattice: neutral, then positive/negative on each axis."""
    action = np.zeros(dimensions, dtype=np.float32)
    if index:
        axis, sign = divmod(index - 1, 2)
        if axis >= dimensions:
            raise ValueError('discrete action out of range')
        action[axis] = -1.0 if sign == 0 else 1.0
    return action


class Discretise(gym.ActionWrapper):
    """Bound DQN action counts linearly in the number of Box dimensions."""
    def __init__(self, env):
        super().__init__(env)
        if len(env.action_space.shape) != 1:
            raise ValueError('DQN requires a flat Box or Discrete action space')
        self.dimensions = env.action_space.shape[0]
        self.action_space = gym.spaces.Discrete(1 + 2 * self.dimensions)

    def action(self, action: int) -> np.ndarray:
        return discrete_action(int(action), self.dimensions)


def algorithm(name: str, env: gym.Env, **hp) -> Any:
    """Build PPO, SAC, TD3, A2C or DQN using two CPU threads."""
    import stable_baselines3 as sb3
    import torch
    torch.set_num_threads(2)
    name = name.upper()
    if name not in DEFAULTS:
        raise ValueError(f'unsupported algorithm {name!r}')
    if name == 'DQN' and isinstance(env.action_space, gym.spaces.Box):
        env = Discretise(env)
    if name == 'DQN' and not isinstance(env.action_space, gym.spaces.Discrete):
        raise ValueError('DQN requires discrete actions')
    params = {**DEFAULTS[name], **hp}
    for key in ('preview_every', 'cpu_seconds'):
        params.pop(key, None)
    params['device'] = 'cpu'
    params.setdefault('policy_kwargs', {'net_arch': [64, 64]})
    policy = params.pop('policy', 'MlpPolicy')
    return getattr(sb3, name)(policy, env, **params)


def log(*, step: int, **values: Any) -> None:
    """Append one durable metric point for either SB3 or custom training code."""
    ctx = context()
    if step < 0:
        raise ValueError('step must be nonnegative')
    point = {'run': ctx.run, 'step': int(step), 'wall_s': max(0.0, time.time() - ctx.started_at), **values}
    raw = json.dumps(point, allow_nan=False, separators=(',', ':'))
    if len(raw.encode()) > 64 * 1024:
        raise ValueError('metric point exceeds 64 KiB')
    with (ctx.run_dir / 'metrics.jsonl').open('a') as stream:
        stream.write(raw + '\n')


def preview(env: gym.Env, policy_fn, *, max_steps: int = 1000, step: int = 0) -> None:
    """Evaluate one episode, streaming world frames at no more than 15 Hz.

    Uses a separate environment during SB3 callbacks so evaluation never resets
    or perturbs the training rollout. Custom callers own the supplied environment.
    """
    from .scene import frame, export_scene
    ctx = context()
    descriptor = os.environ.get('EXCALIBUR_ROBOTLAB_PREVIEW_FD')
    if not descriptor:
        return
    base = env.unwrapped
    descriptor = int(descriptor)
    os.set_blocking(descriptor, True)
    total = 0.0
    emitted = 0.0
    obs, _ = env.reset()
    try:
        for index in range(min(10_000, max(1, max_steps))):
            action = policy_fn(obs)
            obs, reward, terminated, truncated, info = env.step(action)
            total += float(reward)
            now = time.monotonic()
            if now - emitted >= 1 / 15:
                point = frame(base, ctx.run, step + index, 0, reward, total,
                              terminated or truncated, info, 'policy', preview=True)
                if emitted == 0:
                    point['scene'] = export_scene(base)
                try:
                    packet = json.dumps(point, allow_nan=False).encode() + b'\n'
                    if len(packet) > 8 * 1024**2:
                        raise ValueError('preview frame exceeds 8 MiB')
                    pending = memoryview(packet)
                    while pending:
                        count = os.write(descriptor, pending)
                        pending = pending[count:]
                except OSError:
                    pass
                emitted = time.monotonic()
            if terminated or truncated:
                break
    finally:
        pass


def _checkpoint(model: Any, directory: Path, name: str) -> None:
    """Publish complete checkpoints so live playback never reads a partial zip."""
    temporary = directory / (name + '.tmp.zip')
    model.save(temporary)
    temporary.replace(directory / (name + '.zip'))


def callback(*, preview_every: int | None = None) -> Any:
    """Episode metrics, checkpoints, rollout losses and periodic evaluation."""
    from stable_baselines3.common.callbacks import BaseCallback
    ctx = context()
    default_every = max(1_000, min(10_000, int(ctx.total_steps) // 20))  # about 20 previews per run
    interval = preview_every if preview_every is not None else int(ctx.hyperparams.get('preview_every', default_every))

    class TrainingCallback(BaseCallback):
        def __init__(self):
            super().__init__()
            self.best = -float('inf')
            self.next_preview = max(1, interval)

        def _on_step(self) -> bool:
            for info in self.locals.get('infos', []):
                episode = info.get('episode')
                if episode:
                    reward = float(episode['r'])
                    log(step=self.num_timesteps, episode_return=reward, episode_length=int(episode['l']))
                    if reward > self.best:
                        self.best = reward
                        _checkpoint(self.model, ctx.run_dir, 'best')
            if interval > 0 and self.num_timesteps >= self.next_preview:
                self.next_preview = self.num_timesteps + interval
                evaluation = make_env()
                try:
                    if ctx.algorithm == 'DQN' and isinstance(evaluation.action_space, gym.spaces.Box):
                        evaluation = Discretise(evaluation)
                    preview(evaluation, lambda obs: self.model.predict(obs, deterministic=True)[0],
                            max_steps=evaluation.unwrapped.max_episode_steps, step=self.num_timesteps)
                finally:
                    evaluation.close()
            return True

        def _on_rollout_end(self) -> None:
            losses = {key: float(value) for key, value in self.model.logger.name_to_value.items()
                      if 'loss' in key and isinstance(value, (int, float, np.number)) and np.isfinite(value)}
            if losses:
                log(step=self.num_timesteps, loss=next(iter(losses.values())), extra=losses)

        def _on_training_end(self) -> None:
            _checkpoint(self.model, ctx.run_dir, 'last')
            if not (ctx.run_dir / 'best.zip').exists():
                _checkpoint(self.model, ctx.run_dir, 'best')
            log(step=self.num_timesteps)

    return TrainingCallback()


def save(model) -> None:
    """Save the final SB3 checkpoint."""
    _checkpoint(model, context().run_dir, 'last')


def save_policy(obj) -> str:
    """Save a callable with cloudpickle, or a torch tensor/state as policy.pt."""
    ctx = context()
    if callable(obj):
        import cloudpickle
        path = ctx.run_dir / 'policy.pkl'
        with path.open('wb') as stream:
            cloudpickle.dump(obj, stream)
    else:
        import torch
        torch.set_num_threads(2)
        path = ctx.run_dir / 'policy.pt'
        torch.save(obj, path)
    return str(path)
