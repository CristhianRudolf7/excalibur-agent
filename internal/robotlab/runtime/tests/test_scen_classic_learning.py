"""Tiny local PPO integration checks; training quality is measured separately."""

import numpy as np
import pytest

from robotlab.scenarios.cartpole import CartPole
from robotlab.scenarios.ballplate import BallPlate


@pytest.mark.parametrize("scenario", [CartPole({"links": 1}), BallPlate()])
def test_ppo_1000_steps(scenario):
    import torch
    from stable_baselines3 import PPO

    torch.set_num_threads(2)
    env = scenario.make_env()
    model = PPO("MlpPolicy", env, n_steps=100, batch_size=50, n_epochs=1,
                seed=3, device="cpu", policy_kwargs={"net_arch": [16, 16]})
    model.learn(total_timesteps=1000)
    assert model.num_timesteps == 1000
    observation, _ = env.reset(seed=5)
    action, _ = model.predict(observation, deterministic=True)
    assert env.action_space.contains(action)
    assert np.isfinite(env.step(action)[1])
