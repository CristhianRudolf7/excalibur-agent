"""Opt-in, bounded arm verification; never imported by the ordinary test suite.

Run with low process priority and two BLAS/PyTorch threads. Store any checkpoints
outside the repository. The commands print JSON records suitable for evidence.
"""
from __future__ import annotations

import argparse
import json
import time
from pathlib import Path

import numpy as np

from . import ArmScenario


def skills(layouts: int) -> dict:
    """Exercise varied object classes, mark references and bins across fixed seeds."""
    start = time.monotonic()
    successes = 0
    for seed in range(layouts):
        scenario = ArmScenario({"task": "sort", "objects": 8, "seed": seed, "randomize": True})
        env = scenario.make_env()
        obj = seed % 8 + 1
        target = ("A", "B", "reject")[seed % 3]
        frames = 0
        result = {"seed": seed, "object": obj, "target": target, "ok": False}
        try:
            generator = scenario.skill(env, "pick_place", {"object": obj, "target": target})
            for _ in generator:
                frames += 1
                if frames > 1500:
                    generator.close()
                    raise RuntimeError("skill exceeded 1500 control steps")
            result["ok"] = env.in_bin(f"part_{obj}", target)
        except (ValueError, RuntimeError) as error:
            result["error"] = str(error)
        finally:
            env.close()
        successes += int(result["ok"])
        print(json.dumps({**result, "frames": frames}), flush=True)
    return {"successes": successes, "layouts": layouts, "wall_s": time.monotonic() - start}


def learning(steps: int, output: Path, algorithm: str = "PPO", resume: bool = False) -> dict:
    """Train a small CPU policy and compare twenty held-out goals against its initial policy."""
    import torch
    from stable_baselines3 import PPO, SAC
    from stable_baselines3.common.vec_env import DummyVecEnv, VecNormalize

    torch.set_num_threads(2)
    torch.set_num_interop_threads(2)
    output.mkdir(parents=True, exist_ok=True)
    env = ArmScenario().make_env()
    normalized = None
    if algorithm == "PPO":
        vector = DummyVecEnv([lambda: env])
        if resume:
            normalized = VecNormalize.load(output / "reach-ppo-normalization.pkl", vector)
            model = PPO.load(output / "reach-ppo", env=normalized, device="cpu")
        else:
            normalized = VecNormalize(vector, norm_obs=True, norm_reward=False)
            model = PPO("MlpPolicy", normalized, seed=17, device="cpu", learning_rate=.0003,
                        n_steps=1024, batch_size=64, n_epochs=5, ent_coef=.001,
                        policy_kwargs={"net_arch": {"pi": [64, 64], "vf": [64, 64]}, "log_std_init": -2.3})
            # Start with a safe holding command; target-dependent behaviour is learned.
            with torch.no_grad():
                model.policy.action_net.bias.copy_(torch.as_tensor(env.normalized(env.data.ctrl)))
    else:
        model = SAC("MlpPolicy", env, seed=17, device="cpu", learning_rate=.0003,
                    buffer_size=100_000, learning_starts=1000, batch_size=128,
                    train_freq=4, gradient_steps=1, policy_kwargs={"net_arch": [128, 128]})

    def evaluate() -> dict:
        evaluation = ArmScenario().make_env()
        best_distances, returns = [], []
        successes = 0
        for seed in range(100, 120):
            obs, _ = evaluation.reset(seed=seed)
            best, total = float("inf"), 0.
            for _ in range(200):
                policy_obs = normalized.normalize_obs(obs) if normalized is not None else obs
                action, _ = model.predict(policy_obs, deterministic=True)
                obs, reward, done, truncated, _ = evaluation.step(action)
                best = min(best, float(np.linalg.norm(evaluation.target - evaluation.tcp)))
                total += reward
                if done or truncated:
                    successes += int(done)
                    break
            best_distances.append(best)
            returns.append(total)
        evaluation.close()
        return {"successes": successes, "episodes": 20,
                "mean_best_distance_m": float(np.mean(best_distances)),
                "mean_return": float(np.mean(returns))}

    start = time.monotonic()
    cpu_start = time.process_time()
    completed = int(model.num_timesteps)
    if completed >= steps:
        raise ValueError("step budget must exceed the resumed checkpoint's step count")
    print(json.dumps({"algorithm": algorithm, "steps": completed, **evaluate()}), flush=True)
    metrics = {}
    while completed < steps:
        chunk = min(10_000 if algorithm == "PPO" else 25_000, steps - completed)
        model.learn(chunk, reset_num_timesteps=False)
        completed = int(model.num_timesteps)
        metrics = {"algorithm": algorithm, "steps": completed, "wall_s": time.monotonic() - start,
                   "cpu_s": time.process_time() - cpu_start, **evaluate()}
        print(json.dumps(metrics), flush=True)
        model.save(output / f"reach-{algorithm.lower()}")
        if normalized is not None:
            normalized.save(output / "reach-ppo-normalization.pkl")
        if metrics["successes"] >= 16:
            break
    env.close()
    return metrics


def main() -> None:
    """Run only the explicitly requested verification with bounded budgets."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("skills", "learning"))
    parser.add_argument("--layouts", type=int, default=40)
    parser.add_argument("--steps", type=int, default=300_000)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--algorithm", choices=("SAC", "PPO"), default="PPO")
    parser.add_argument("--resume", action="store_true", help="continue the saved PPO policy and observation statistics")
    args = parser.parse_args()
    if not 1 <= args.layouts <= 100 or not 1 <= args.steps <= 500_000:
        parser.error("layouts must be 1..100 and steps must be 1..500000")
    if args.resume and (args.mode != "learning" or args.algorithm != "PPO"):
        parser.error("resume is supported only for PPO learning verification")
    if args.mode == "learning" and args.output is None:
        parser.error("learning requires an explicit scratch --output directory")
    if args.output:
        repository = Path(__file__).resolve().parents[6]
        if args.output.resolve().is_relative_to(repository):
            parser.error("checkpoints must be written outside the repository")
    result = skills(args.layouts) if args.mode == "skills" else learning(args.steps, args.output, args.algorithm, args.resume)
    print(json.dumps(result), flush=True)
    if args.mode == "skills" and result["successes"] / args.layouts < .95:
        raise SystemExit(1)
    if args.mode == "learning" and result["successes"] < 16:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
