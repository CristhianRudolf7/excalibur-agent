"""Physics and contract checks for the classic Robot Lab scenarios."""

import itertools

import mujoco
import numpy as np
import pytest
from gymnasium.utils.env_checker import check_env

from robotlab.registry import get
from robotlab.scenarios.cartpole import CartPole
from robotlab.scenarios.ballplate import BallPlate


def cases():
    """Cover every categorical choice and numeric boundary cross-product."""
    for links, start, length, mass, force in itertools.product(
            (1, 2, 3), ("upright", "hanging"), (0.3, 1.2), (0.3, 5.0), (1., 30.)):
        yield CartPole, dict(links=links, start=start, pole_length=length,
                            cart_mass=mass, max_force=force)
    for task, size, radius, friction in itertools.product(
            ("center", "target", "circle"), (0.6, 2.), (0.02, 0.09), (0.1, 1.2)):
        yield BallPlate, dict(task=task, plate_size=size, ball_radius=radius, friction=friction)


@pytest.mark.parametrize("cls,params", list(cases()))
def test_random_actions_remain_finite(cls, params):
    env = cls(params).make_env(randomize=True)
    env.reset(seed=41)
    rng = np.random.default_rng(19)
    # Continue even past termination to stress physical limits and floor contact.
    for _ in range(2000):
        obs, reward, _, _, info = env.step(rng.uniform(-1, 1, env.model.nu))
        assert np.isfinite(obs).all()
        assert np.isfinite(reward)
        assert np.isfinite(env.data.qpos).all()
        assert np.max(np.abs(env.data.qvel)) < 1000
        assert set(info["terms"]) == set(env.weights)
    assert not any(w.number for w in env.data.warning)


@pytest.mark.parametrize("cls", [CartPole, BallPlate])
def test_gym_contract_registry_and_input_mapping(cls):
    assert get(cls.id) is cls
    scenario = cls()
    env = scenario.make_env()
    check_env(env, skip_render_check=True)
    env.reset(seed=9)
    info = scenario.info()
    assert all(set(info[key]) == {"en", "es"} for key in ("title", "description"))
    np.testing.assert_array_equal(scenario.idle_action(env), np.zeros(env.model.nu))
    for index, actuator in enumerate(scenario.actuators_for(scenario.params)):
        inputs = {actuator.name: 3.0}
        action = scenario.manual_action(env, inputs)
        expected = np.zeros(env.model.nu)
        expected[index] = 1
        np.testing.assert_array_equal(action, expected)
        np.testing.assert_allclose(env.scale_action(action)[index], actuator.range[1])
        np.testing.assert_array_equal(scenario.manual_action(env, {actuator.name: -3})[index], -1)
    assert scenario.manual_action(env, {"unknown": 1}).sum() == 0
    keys = {key for a in scenario.actuators for key in (a.key_neg, a.key_pos)}
    assert keys == ({"ArrowLeft", "ArrowRight"} if cls is CartPole else {"KeyW", "KeyA", "KeyS", "KeyD"})
    assert set(scenario.default_camera()) == {"pos", "target"}


@pytest.mark.parametrize("cls", [CartPole, BallPlate])
def test_seeded_randomization_does_not_accumulate(cls):
    env = cls().make_env(randomize=True)
    base_mass, base_friction = env._mass.copy(), env._friction.copy()
    obs, _ = env.reset(seed=14)
    masses, friction = env.model.body_mass.copy(), env.model.geom_friction.copy()
    for seed in range(20):
        env.reset(seed=seed)
        active = base_mass > 0
        assert np.all(env.model.body_mass[active] >= base_mass[active] * .85)
        assert np.all(env.model.body_mass[active] <= base_mass[active] * 1.15)
        assert np.all(env.model.geom_friction >= base_friction * .85)
        assert np.all(env.model.geom_friction <= base_friction * 1.15)
    again, _ = env.reset(seed=14)
    np.testing.assert_allclose(obs, again)
    np.testing.assert_allclose(masses, env.model.body_mass)
    np.testing.assert_allclose(friction, env.model.geom_friction)
    plain = cls().make_env()
    plain.reset(seed=3)
    np.testing.assert_array_equal(plain.model.body_mass, plain._mass)


@pytest.mark.parametrize("links", [1, 2, 3])
def test_cartpole_termination_and_rewards(links):
    env = CartPole({"links": links}).make_env()
    env.reset(seed=1)
    env.data.qpos[:] = 0
    assert not env._terminated()
    terms = env._reward_terms(np.ones(1))
    assert terms == {"upright": 1., "centered": 1., "effort": -1., "alive": 1.}
    env.data.qpos[links] = np.pi / 4 + .001
    assert env._terminated()
    # Absolute angles, rather than just relative angles, determine a fall.
    env.data.qpos[1:] = .3
    assert env._terminated() == (links == 3)
    env.data.qpos[:] = 0
    env.data.qpos[0] = 2.401
    assert env._terminated()
    assert 0 < env._reward_terms(np.zeros(1))["centered"] < 1
    swing = CartPole({"links": links, "start": "hanging"}).make_env()
    swing.reset(seed=1)
    assert swing._reward_terms(np.zeros(1))["upright"] < -.99
    swing.data.qpos[0] = 3
    assert not swing._terminated()
    env.max_episode_steps = 1
    env.reset(seed=1)
    assert env.step([0])[3]
    assert env.action_space.shape == (1,)


@pytest.mark.parametrize("task", ["center", "target", "circle"])
def test_ballplate_targets_termination_and_rewards(task):
    env = BallPlate({"task": task}).make_env()
    env.reset(seed=4)
    initial = env.target.copy()
    assert not env._terminated()
    _, reward, _, _, info = env.step([.3, -.4])
    terms = info["terms"]
    assert terms["distance"] <= 0 and terms["on_plate"] == 1
    assert terms["smooth"] == pytest.approx(-.25)
    assert terms["effort"] == pytest.approx(-.25)
    assert reward == pytest.approx(sum(env.weights[k] * v for k, v in terms.items()))
    assert env.step([.3, -.4])[4]["terms"]["smooth"] == 0
    if task == "center":
        np.testing.assert_array_equal(env.target, [0, 0])
    elif task == "target":
        np.testing.assert_array_equal(env.target, initial)
        assert np.linalg.norm(env.target) > 0
    else:
        assert not np.array_equal(env.target, initial)
        assert np.linalg.norm(env.target) == pytest.approx(env.params["plate_size"] * .22)
    plate = env.data.body("plate")
    marker = plate.xpos + plate.xmat.reshape(3, 3) @ np.r_[env.target, .028]
    np.testing.assert_allclose(env.data.body("target").xpos, marker)
    env.data.qpos[2] = 2
    mujoco.mj_forward(env.model, env.data)
    assert env._terminated()
    assert env._reward_terms(np.zeros(2))["on_plate"] == 0
    env.data.qpos[2] = 0
    env.data.qpos[4] = .1
    mujoco.mj_forward(env.model, env.data)
    assert env._terminated()


@pytest.mark.parametrize("cls,params", [
    (CartPole, {"links": 0}), (CartPole, {"links": True}),
    (CartPole, {"start": "bad"}), (CartPole, {"max_force": float("nan")}),
    (CartPole, {"pole_length": 0}), (CartPole, {"cart_mass": "1"}),
    (BallPlate, {"task": "bad"}), (BallPlate, {"friction": -1}),
    (BallPlate, {"plate_size": float("inf")}), (BallPlate, {"ball_radius": True}),
    (CartPole, {"unknown": 1}), (BallPlate, {"unknown": 1}),
])
def test_invalid_params(cls, params):
    with pytest.raises(ValueError):
        cls(params)


@pytest.mark.parametrize("cls", [CartPole, BallPlate])
def test_switch_and_custom_reward_weights(cls, monkeypatch):
    monkeypatch.setenv("EXCALIBUR_ROBOTLAB_CLASSIC", "OFF")
    with pytest.raises(RuntimeError, match="disabled"):
        cls().make_env()
    monkeypatch.setenv("EXCALIBUR_ROBOTLAB_CLASSIC", "on")
    env = cls().make_env(reward_weights={"effort": 0})
    assert env.weights["effort"] == 0
    with pytest.raises(ValueError, match="unknown reward"):
        cls().make_env(reward_weights={"missing": 1})


def test_ballplate_center_does_not_reward_deliberate_drops():
    """Remaining on a level plate beats escaping ordinary distance penalties."""
    returns = []
    for action in (np.zeros(2), np.array([1., 0.])):
        scores = []
        env = BallPlate().make_env()
        for seed in range(100, 103):
            env.reset(seed=seed)
            score = 0.
            for _ in range(env.max_episode_steps):
                _, reward, terminated, truncated, _ = env.step(action)
                score += reward
                if terminated or truncated:
                    break
            scores.append(score)
        returns.append(float(np.mean(scores)))
    assert returns[0] > 0
    assert returns[0] > returns[1]
