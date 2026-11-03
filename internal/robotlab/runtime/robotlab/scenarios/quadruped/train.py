"""Reproducible, bounded PPO training; output belongs outside the source tree.

Example: python -m robotlab.scenarios.quadruped.train --output /scratch/quadruped
The caller must also impose a process-level timeout (at most three hours).
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import time

import numpy as np
import torch
from stable_baselines3 import PPO
from stable_baselines3.common.callbacks import BaseCallback
from stable_baselines3.common.monitor import Monitor
from stable_baselines3.common.vec_env import DummyVecEnv

from . import QuadrupedScenario


def gait_demonstrations(episodes: int = 8, steps: int = 500) -> tuple[np.ndarray, np.ndarray]:
    """Collect slightly perturbed keyboard-trot examples for policy initialization."""
    if episodes < 1 or steps < 1:
        raise ValueError("demonstration episodes and steps must be positive")
    scenario = QuadrupedScenario()
    env = scenario.make_env(randomize=True)
    rng = np.random.default_rng(7)
    observations, actions = [], []
    try:
        for episode in range(episodes):
            obs, _ = env.reset(seed=episode + 7)
            for _ in range(steps):
                action = scenario.manual_action(env, {"ArrowUp": 1})
                observations.append(obs.copy())
                actions.append(action.copy())
                obs, _, terminated, truncated, _ = env.step(
                    np.clip(action + rng.normal(0, .025, 12), -1, 1))
                if terminated or truncated:
                    break
    finally:
        env.close()
    return np.asarray(observations, dtype=np.float32), np.asarray(actions, dtype=np.float32)


def warm_start_gait(model: PPO, epochs: int = 200) -> None:
    """Fit the actor to a working trot before PPO optimizes the physical reward.

    The policy still consumes only the standard observation: no clock or scripted
    gait is used during policy playback. PPO subsequently updates actor and critic.
    """
    observations, actions = gait_demonstrations()
    obs = torch.as_tensor(observations, device=model.device)
    targets = torch.as_tensor(actions, device=model.device)
    optimizer = torch.optim.Adam(model.policy.parameters(), lr=1e-3)
    model.policy.set_training_mode(True)
    for epoch in range(epochs):
        permutation = torch.randperm(len(obs), device=model.device)
        for batch in permutation.split(256):
            mean = model.policy.get_distribution(obs[batch]).distribution.mean
            loss = torch.nn.functional.mse_loss(mean, targets[batch])
            optimizer.zero_grad()
            loss.backward()
            optimizer.step()
        if (epoch + 1) % 50 == 0:
            print(json.dumps({"initialization_epoch": epoch + 1, "loss": float(loss.detach())}), flush=True)
    with torch.no_grad():
        model.policy.log_std.fill_(-2.3)


class Evaluation(BaseCallback):
    """Save the highest-return deterministic ten-second evaluation checkpoint."""

    def __init__(self, output: Path, started: float, interval: int = 10000):
        super().__init__()
        self.output, self.started, self.interval = output, started, interval
        self.next_eval, self.best = interval, -float("inf")

    def _on_training_start(self) -> None:
        self.next_eval = (self.num_timesteps // self.interval + 1) * self.interval
        metrics = self.output / "metrics.jsonl"
        if metrics.is_file():
            records = [json.loads(line) for line in metrics.read_text().splitlines() if line]
            self.best = max((record["return"] for record in records), default=self.best)
        self.evaluate()

    def evaluate(self) -> None:
        """Measure displacement, survival and return without exploration noise."""
        env = QuadrupedScenario().make_env()
        obs, _ = env.reset(seed=0)
        total, count, fell = 0.0, 0, False
        for _ in range(500):
            action, _ = self.model.predict(obs, deterministic=True)
            obs, reward, fell, _, _ = env.step(action)
            total += reward
            count += 1
            if fell:
                break
        record = {"steps": self.num_timesteps, "wall_s": round(time.monotonic() - self.started, 1),
                  "return": total, "distance": float(env.data.qpos[0]),
                  "speed": float(env.data.qpos[0] / (count * env.dt)), "fell": fell}
        print(json.dumps(record), flush=True)
        with (self.output / "metrics.jsonl").open("a") as stream:
            stream.write(json.dumps(record) + "\n")
        if total > self.best:
            self.best = total
            self.model.save(self.output / "best.zip")
        self.model.save(self.output / "last.zip")
        env.close()

    def _on_step(self) -> bool:
        if self.num_timesteps >= self.next_eval:
            self.next_eval += self.interval
            self.evaluate()
        return True

    def _on_training_end(self) -> None:
        self.evaluate()


def main() -> None:
    """Train a compact policy on four seeded, flat-ground environments."""
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--steps", type=int, default=2_000_000)
    parser.add_argument("--resume", type=Path)
    parser.add_argument("--eval-interval", type=int, default=10000)
    parser.add_argument("--warm-start-gait", action="store_true",
                        help="initialize the actor from the built-in keyboard trot before PPO")
    args = parser.parse_args()
    if args.steps < 1 or args.eval_interval < 1:
        parser.error("steps and eval-interval must be positive")
    args.output.mkdir(parents=True, exist_ok=True)
    torch.set_num_threads(2)
    torch.set_num_interop_threads(2)
    started = time.monotonic()
    env = DummyVecEnv([lambda: Monitor(QuadrupedScenario().make_env(randomize=True)) for _ in range(4)])
    if args.resume:
        model = PPO.load(args.resume, env=env, device="cpu")
    else:
        model = PPO("MlpPolicy", env, learning_rate=3e-4, n_steps=1024, batch_size=256,
                    n_epochs=5, gamma=.99, gae_lambda=.95, clip_range=.2, ent_coef=.003,
                    policy_kwargs={"net_arch": [64, 64], "log_std_init": -.7}, seed=7,
                    device="cpu", verbose=0)
    try:
        if args.warm_start_gait:
            warm_start_gait(model)
            model.save(args.output / "warm-start.zip")
        model.learn(args.steps, callback=Evaluation(args.output, started, args.eval_interval),
                    reset_num_timesteps=not bool(args.resume))
    finally:
        model.save(args.output / "last.zip")
        env.close()


if __name__ == "__main__":
    main()
