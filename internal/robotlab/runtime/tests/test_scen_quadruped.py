"""Bounded physics and control checks for the quadruped scenario."""

import math
from pathlib import Path

import mujoco
import numpy as np
import pytest

from robotlab.scenarios.quadruped import QuadrupedScenario


@pytest.mark.parametrize("terrain", ["flat", "bumpy", "stairs"])
@pytest.mark.parametrize("task", ["walk", "turn", "push_recovery"])
def test_build_and_step(terrain, task):
    scenario = QuadrupedScenario({"terrain": terrain, "task": task})
    env = scenario.make_env()
    obs, _ = env.reset(seed=13)
    assert obs.shape == (48,)
    assert env.model.nu == 12
    assert env.model.nq == 19
    assert env.model.nv == 18
    for i, actuator in enumerate(scenario.actuators):
        assert actuator.name == mujoco.mj_id2name(env.model, mujoco.mjtObj.mjOBJ_ACTUATOR, i)
    obs, reward, _, _, info = env.step(np.zeros(12))
    assert np.isfinite(obs).all() and math.isfinite(reward)
    assert set(info["terms"]) == {t.name for t in scenario.reward_terms}
    assert env.model.nmesh == 0


def test_zero_action_stands_three_seconds():
    env = QuadrupedScenario().make_env()
    env.reset(seed=0)
    for _ in range(math.ceil(3 / env.dt)):
        _, _, terminated, truncated, info = env.step(np.zeros(12))
        assert not terminated and not truncated
        assert info["height"] > .3
    np.testing.assert_array_equal(env.data.ctrl, env.scenario.nominal)


def test_two_thousand_random_steps_are_finite():
    env = QuadrupedScenario().make_env(randomize=True)
    env.reset(seed=17)
    rng = np.random.default_rng(17)
    for _ in range(2000):
        obs, reward, terminated, truncated, info = env.step(rng.uniform(-1, 1, 12))
        assert np.isfinite(obs).all() and math.isfinite(reward)
        assert np.isfinite(env.data.qacc).all()
        if terminated or truncated:
            env.reset()


def test_fall_and_time_limit():
    env = QuadrupedScenario().make_env()
    env.reset(seed=0)
    assert not env._terminated()
    env.data.qpos[2] = .18
    assert env._terminated()
    env.reset(seed=0)
    env.data.qpos[3:7] = [0, 1, 0, 0]
    assert env._terminated()
    env.reset(seed=0)
    env.steps = env.max_episode_steps - 1
    assert env.step(np.zeros(12))[3]


def test_pushes_are_seeded_and_cleared():
    envs = [QuadrupedScenario({"task": "push_recovery", "push_force": 30}).make_env() for _ in range(2)]
    for env in envs:
        env.reset(seed=21)
    pushed = False
    cleared = False
    for _ in range(130):
        for env in envs:
            env.step(np.zeros(12))
        np.testing.assert_array_equal(envs[0].data.xfrc_applied, envs[1].data.xfrc_applied)
        if np.any(envs[0].data.xfrc_applied):
            pushed = True
            assert np.linalg.norm(envs[0].data.xfrc_applied[envs[0].torso_id, :2]) == pytest.approx(30)
        elif pushed:
            cleared = True
    assert pushed and cleared
    envs[0].reset()
    assert not np.any(envs[0].data.xfrc_applied)


def test_manual_gait_and_slider_override():
    scenario = QuadrupedScenario()
    env = scenario.make_env()
    env.reset()
    np.testing.assert_array_equal(scenario.manual_action(env, {}), np.zeros(12))
    env.data.time = .15
    action = scenario.manual_action(env, {"ArrowUp": 1, "fl_hip": -.7})
    assert action[1] == pytest.approx(-.7)
    assert action[2] > 0
    assert action[11] == action[2]
    assert np.max(np.abs(action)) <= 1
    assert not np.array_equal(action, scenario.manual_action(env, {"ArrowDown": 1}))
    env.data.time = .15
    assert np.any(scenario.manual_action(env, {"ArrowLeft": 1}))


@pytest.mark.parametrize("key,direction", [("ArrowUp", 1), ("ArrowDown", -1),
                                           ("ArrowLeft", 1), ("ArrowRight", -1)])
def test_manual_gait_drives_commanded_direction(key, direction):
    """The keyboard helper makes progress and turns in the requested direction."""
    scenario = QuadrupedScenario()
    env = scenario.make_env()
    env.reset(seed=0)
    for _ in range(round(10 / env.dt)):
        _, _, terminated, _, _ = env.step(scenario.manual_action(env, {key: 1}))
        assert not terminated
    if key in {"ArrowUp", "ArrowDown"}:
        assert direction * env.data.qpos[0] > .5
    else:
        yaw = 2 * math.atan2(env.data.qpos[6], env.data.qpos[3])
        assert direction * yaw > .2
    env.close()


def test_reward_reweighting_and_observation_history():
    env = QuadrupedScenario({"task": "turn"}).make_env({"forward": 0, "turn": 2})
    env.reset(seed=0)
    action = np.full(12, .1)
    obs, reward, _, _, info = env.step(action)
    np.testing.assert_allclose(obs[34:46], action)
    np.testing.assert_allclose(obs[-2:], [.5, .6])
    assert reward == pytest.approx(sum(env.weights[k] * v for k, v in info["terms"].items()))
    with pytest.raises(ValueError, match="unknown reward"):
        QuadrupedScenario().make_env({"missing": 1})


def test_body_frame_observation_and_reset_history():
    """Yaw rotates world velocity into the body frame without tilting gravity."""
    env = QuadrupedScenario().make_env()
    env.reset(seed=0)
    env.data.qpos[3:7] = [math.sqrt(.5), 0, 0, math.sqrt(.5)]
    env.data.qvel[:3] = [0, .4, 0]
    obs = env._observation()
    np.testing.assert_allclose(obs[1:4], [0, 0, -1], atol=1e-6)
    np.testing.assert_allclose(obs[4:7], [.4, 0, 0], atol=1e-6)
    env.step(np.full(12, .2))
    obs, _ = env.reset(seed=0)
    np.testing.assert_array_equal(obs[34:46], np.zeros(12))
    np.testing.assert_array_equal(env.previous_action, np.zeros(12))


def test_tracking_rewards_peak_at_the_command():
    """Velocity and yaw tracking distinguish the requested motion from rest."""
    env = QuadrupedScenario({"task": "turn"}).make_env()
    env.reset(seed=0)
    resting = env._reward_terms(np.zeros(12))
    env.data.qvel[0] = .5
    env.data.qvel[5] = .6
    tracking = env._reward_terms(np.zeros(12))
    assert tracking["forward"] == pytest.approx(1)
    assert tracking["turn"] == pytest.approx(1)
    assert tracking["forward"] > resting["forward"]
    assert tracking["turn"] > resting["turn"]


@pytest.mark.parametrize("params", [{"task": "jump"}, {"terrain": "water"}, {"target_speed": float("nan")},
                                         {"target_speed": -1}, {"push_force": 151}, {"push_force": True}, {"unknown": 1}])
def test_invalid_params(params):
    with pytest.raises(ValueError):
        QuadrupedScenario(params)


def test_switch(monkeypatch):
    monkeypatch.setenv("EXCALIBUR_ROBOTLAB_QUADRUPED", "OFF")
    with pytest.raises(ValueError, match="disabled"):
        QuadrupedScenario().make_env()
    assert QuadrupedScenario.info()["id"] == "quadruped"
    monkeypatch.setenv("EXCALIBUR_ROBOTLAB_QUADRUPED", "on")
    assert QuadrupedScenario().id == "quadruped"


def test_invalid_actions():
    env = QuadrupedScenario().make_env()
    env.reset()
    for action in [np.zeros(11), np.full(12, float("nan"))]:
        with pytest.raises(ValueError, match="12 finite"):
            env.step(action)


def test_pretrained_walks_one_meter_in_ten_seconds():
    from stable_baselines3 import PPO
    import torch

    torch.set_num_threads(2)
    scenario = QuadrupedScenario()
    path = scenario.pretrained_policy()
    if not Path(path).is_file():
        pytest.skip("bundled quadruped walk.zip is absent; train with quadruped.train")
    assert Path(path).stat().st_size < 3_000_000, "bundled policy exceeds its size budget"
    policy = PPO.load(path, device="cpu")
    env = scenario.make_env()
    obs, _ = env.reset(seed=0)
    start = env.data.qpos[0]
    for _ in range(round(10 / env.dt)):
        action, _ = policy.predict(obs, deterministic=True)
        obs, _, terminated, _, _ = env.step(action)
        assert not terminated, "bundled policy fell before ten seconds"
    assert env.data.qpos[0] - start >= 1.0
