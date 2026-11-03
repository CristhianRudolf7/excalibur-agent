"""Bounded physics, control, reward and training checks for the flight worlds."""
import mujoco
import numpy as np
import pytest

from robotlab.scenarios.drone import Drone
from robotlab.scenarios.rocket import Rocket


@pytest.mark.parametrize('task', ['hover', 'waypoints', 'wind'])
@pytest.mark.parametrize('mass', [.2, 1., 5.])
@pytest.mark.parametrize('mode', ['assisted', 'raw'])
@pytest.mark.parametrize('action_mode', ['mixed', 'raw'])
def test_drone_parameter_builds(task, mass, mode, action_mode):
    scenario = Drone({'task': task, 'mass': mass, 'manual_mode': mode, 'action_mode': action_mode, 'wind_strength': 10,
                      'waypoints': [[-2, 1, .4], [2, 2, 8]]})
    env = scenario.make_env()
    env.reset(seed=12)
    assert env.model.nu == 4
    assert env.model.body_mass[env.body] == mass
    assert np.allclose([a.range for a in scenario.actuators_for(scenario.params)], env.model.actuator_ctrlrange)
    assert np.isfinite(env.step(np.zeros(4))[0]).all()


@pytest.mark.parametrize('gravity,acceleration', [('earth', 9.81), ('moon', 1.62), ('mars', 3.71)])
@pytest.mark.parametrize('height,fuel,wind', [(1.5, 0, 0), (5, 180, 3), (20, 1000, 10)])
def test_rocket_parameter_builds(gravity, acceleration, height, fuel, wind):
    scenario = Rocket({'gravity': gravity, 'start_height': height, 'fuel': fuel, 'wind': wind})
    env = scenario.make_env()
    env.reset(seed=1)
    assert env.model.opt.gravity[2] == -acceleration
    assert env.model.nu == 5
    assert np.allclose([a.range for a in scenario.actuators_for(scenario.params)], env.model.actuator_ctrlrange)
    assert np.isfinite(env.step(np.zeros(5))[0]).all()


@pytest.mark.parametrize('factory', [Drone, Rocket])
def test_random_steps_stay_finite(factory):
    env = factory().make_env(randomize=True)
    env.reset(seed=10)
    rng = np.random.default_rng(22)
    for _ in range(2000):
        obs, reward, terminal, truncated, info = env.step(rng.uniform(-1, 1, env.model.nu))
        assert np.isfinite(obs).all() and np.isfinite(reward)
        assert all(np.isfinite(value) for value in info['terms'].values())
        if terminal or truncated:
            env.reset()
    assert env.data.warning.number.sum() == 0


@pytest.mark.parametrize('mass', [.2, 1., 5.])
def test_assisted_hover_and_keyboard(mass):
    scenario = Drone({'mass': mass})
    env = scenario.make_env(randomize=True)
    env.reset(seed=2)
    initial = env.data.qpos[2]
    for _ in range(500):
        _, _, terminal, _, _ = env.step(scenario.manual_action(env, {}))
        assert not terminal
        assert abs(env.data.qpos[2] - initial) < .15
    assert np.allclose(env.data.qvel[:3], 0, atol=.02)
    neutral = scenario.manual_action(env, {})
    assert not np.allclose(scenario.manual_action(env, {'motor1': 1}), neutral)
    assert {a.key_pos for a in scenario.actuators} == {'KeyW', 'KeyD', 'ArrowUp', 'ArrowRight'}
    raw = Drone({'manual_mode': 'raw'})
    raw_env = raw.make_env()
    raw_env.reset()
    action = raw.manual_action(raw_env, {'motor2': -.7})
    raw_env.step(action)
    assert np.allclose(raw_env.data.ctrl, raw_env.model.actuator_ctrlrange[:, 1] * np.array([.5, .15, .5, .5]))


@pytest.mark.parametrize('position,quat', [([0, 0, .05], [1, 0, 0, 0]),
                                          ([5.1, 0, 2], [1, 0, 0, 0]),
                                          ([0, 0, 2], [0, 1, 0, 0])])
def test_drone_crashes(position, quat):
    env = Drone().make_env()
    env.reset()
    env.data.qpos[:3] = position
    env.data.qpos[3:7] = quat
    mujoco.mj_forward(env.model, env.data)
    assert env._terminated()
    terms = env._reward_terms(np.ones(4))
    assert terms['crash'] < 0 and terms['effort'] < 0
    assert terms['distance'] <= 0 and terms['velocity'] <= 0


def test_waypoint_bonus_and_marker():
    env = Drone({'task': 'waypoints'}).make_env()
    env.reset(seed=1)
    _, reward, terminal, _, info = env.step(np.zeros(4))
    assert not terminal and info['terms']['waypoint'] == 1
    assert info['goal_index'] == 1
    assert np.allclose(env.data.mocap_pos[0], [2, 0, 1.5])
    assert reward > 0


def test_wind_applies_only_in_wind_task():
    env = Drone({'task': 'wind', 'wind_strength': 3}).make_env()
    env.reset(seed=0)
    env.step(np.zeros(4))
    assert np.linalg.norm(env.data.xfrc_applied[env.body, :2]) > 0


@pytest.mark.parametrize('speed,xy,tilt,expected', [(-.2, 0, 0, 'landed'), (-3, 0, 0, 'crash'),
                                                 (-.2, 2, 0, 'crash'), (-.2, 0, .5, 'crash')])
def test_actual_touchdown_classification(speed, xy, tilt, expected):
    env = Rocket().make_env()
    env.reset(seed=0)
    env.data.qpos[:3] = [xy, 0, .902]
    env.data.qpos[3:7] = [np.cos(tilt/2), np.sin(tilt/2), 0, 0]
    env.data.qvel[:] = 0
    env.data.qvel[2] = speed
    mujoco.mj_forward(env.model, env.data)
    _, reward, terminal, _, info = env.step([-1, 0, 0, 0, 0])
    assert terminal and info['outcome'] == expected
    assert (reward > 0) == (expected == 'landed')
    assert info['terms']['landed'] == (expected == 'landed')
    assert info['terms']['crash'] == -(expected == 'crash')


def test_fuel_is_an_exact_impulse_budget_and_resets():
    env = Rocket({'fuel': .1}).make_env()
    obs, _ = env.reset(seed=1)
    assert obs[-1] == 1
    _, _, terminal, _, info = env.step(np.ones(5))
    assert terminal and info['outcome'] == 'out_of_fuel'
    assert env.fuel_remaining == 0 and env.fuel_used == pytest.approx(.1)
    assert info['terms']['fuel'] < 0
    env.reset(seed=1)
    assert env.fuel_remaining == .1 and env.outcome == 'flying'


@pytest.mark.parametrize('factory', [Drone, Rocket])
def test_seed_reward_reweight_and_switch(factory, monkeypatch):
    env = factory().make_env({'crash': 0}, randomize=True)
    obs, _ = env.reset(seed=4)
    obs2, _ = env.reset(seed=4)
    assert np.array_equal(obs, obs2)
    result = env.step(np.zeros(env.model.nu))
    assert result[1] == pytest.approx(sum(env.weights[k]*v for k, v in result[4]['terms'].items()))
    with pytest.raises(ValueError):
        factory().make_env({'missing': 1})
    monkeypatch.setenv('EXCALIBUR_ROBOTLAB_FLIGHT', 'off')
    with pytest.raises(RuntimeError, match='disabled'):
        factory().make_env()


@pytest.mark.parametrize('factory,params', [(Drone, {'mass': 0}), (Drone, {'task': 'missing'}),
    (Drone, {'waypoints': []}), (Drone, {'waypoints': [[0, 0, -1]]}),
    (Rocket, {'gravity': 'jupiter'}), (Rocket, {'fuel': -1}), (Rocket, {'wind': float('nan')})])
def test_invalid_parameters(factory, params):
    with pytest.raises(ValueError):
        factory(params)


@pytest.mark.parametrize('factory', [Drone, Rocket])
def test_ppo_1000_steps(factory, monkeypatch):
    import torch
    from stable_baselines3 import PPO
    torch.set_num_threads(2)
    if factory is Drone:
        from types import SimpleNamespace
        from robotlab import sdk
        monkeypatch.setattr(sdk, 'context', lambda: SimpleNamespace(
            scenario='drone', params={}, reward_weights={}))
        env = sdk.make_env()
        model = sdk.algorithm('PPO', env, seed=3)
    else:
        env = factory().make_env(randomize=True)
        model = PPO('MlpPolicy', env, n_steps=64, batch_size=64, n_epochs=1,
                    policy_kwargs={'net_arch': [16, 16]}, seed=3, device='cpu')
    model.learn(total_timesteps=1000)
    assert model.num_timesteps == 1024
    obs, _ = env.reset(seed=10)
    action, _ = model.predict(obs, deterministic=True)
    assert np.isfinite(env.step(action)[1])


def test_rotors_and_gimbal_produce_physical_torque():
    drone = Drone({"action_mode": "raw"}).make_env()
    drone.reset(seed=0)
    drone.step([.2, -.2, .2, -.2])
    assert drone.data.qvel[5] > 0
    rocket = Rocket().make_env()
    rocket.reset(seed=0)
    rocket.data.qpos[:2] = 0
    rocket.data.qpos[3:7] = [1, 0, 0, 0]
    rocket.data.qvel[:] = 0
    mujoco.mj_forward(rocket.model, rocket.data)
    for _ in range(20):
        rocket.step([0, 0, .5, 0, 0])
    assert rocket.data.qpos[8] > .05
    assert abs(rocket.data.qvel[4]) > .01


def test_assisted_stabilizer_recovers_small_tilt():
    scenario = Drone()
    env = scenario.make_env()
    env.reset(seed=0)
    env.data.qpos[3:7] = [np.cos(.04), np.sin(.04), 0, 0]
    mujoco.mj_forward(env.model, env.data)
    for _ in range(250):
        _, _, terminal, _, _ = env.step(scenario.manual_action(env, {}))
        assert not terminal
    assert env.data.xmat[env.body, 8] > .999
    assert abs(env.data.qpos[2]-1.5) < .05


@pytest.mark.parametrize('channel,axis,sign', [('motor2', 5, -1), ('motor3', 4, 1), ('motor4', 3, 1)])
def test_assisted_directional_keys(channel, axis, sign):
    scenario = Drone()
    env = scenario.make_env()
    env.reset(seed=0)
    env.step(scenario.manual_action(env, {channel: 1}))
    assert sign * env.data.qvel[axis] > 0


def test_default_motor_allocation_and_raw_compatibility():
    mixed = Drone().make_env()
    raw = Drone({"action_mode": "raw"}).make_env()
    mixed.reset(seed=1)
    raw.reset(seed=1)
    command = np.array([.1, .3, -.4, .5])
    motors = .1 + .015*.3*np.array([1, -1, -1, 1]) + .015*-.4*np.array([-1, -1, 1, 1]) + .05*.5*np.array([1, -1, 1, -1])
    mixed.step(command)
    raw.step(motors)
    assert np.allclose(mixed.data.ctrl, raw.data.ctrl)
    assert np.allclose(mixed.data.qpos, raw.data.qpos)
    assert Drone.default_params["action_mode"] == "mixed"
    with pytest.raises(ValueError):
        Drone({"action_mode": "unknown"})


def test_localized_flight_metadata():
    for factory in (Drone, Rocket):
        info = factory.info()
        strings = [info["title"], info["description"]]
        strings += [a["label"] for a in info["actuators"]]
        strings += [r["description"] for r in info["reward_terms"]]
        assert all(item["es"] and item["es"] != item["en"] for item in strings)


@pytest.mark.parametrize('seed', [100, 200, 209])
def test_bundled_hover_policy(seed):
    import torch
    from pathlib import Path
    from stable_baselines3 import PPO
    torch.set_num_threads(2)
    scenario = Drone()
    path = Path(scenario.pretrained_policy())
    assert scenario.has_pretrained and path.stat().st_size < 1_000_000
    model = PPO.load(path, device='cpu')
    env = scenario.make_env(randomize=True)
    obs, _ = env.reset(seed=seed)
    errors = []
    for _ in range(750):
        obs, _, terminal, truncated, _ = env.step(model.predict(obs, deterministic=True)[0])
        assert not terminal
        errors.append(np.linalg.norm(obs[:3]))
    assert truncated
    assert max(errors) < .5 and np.mean(errors) < .25
    assert abs(env.data.qpos[2] - 1.5) < .1
    assert Drone({'action_mode': 'raw'}).pretrained_policy() is None
    assert Drone({'task': 'wind'}).pretrained_policy() is None


def test_sdk_default_environment_uses_mixed_actions(monkeypatch):
    from types import SimpleNamespace
    from robotlab import sdk
    monkeypatch.setattr(sdk, 'context', lambda: SimpleNamespace(
        scenario='drone', params={}, reward_weights={}))
    env = sdk.make_env()
    env.reset(seed=17)
    action = np.array([.1, .2, -.3, .4])
    env.step(action)
    base = env.unwrapped
    expected = base.scale_action(base.allocation @ action)
    assert np.allclose(base.data.ctrl, expected)
    assert base.params['action_mode'] == 'mixed'
    assert base.randomize
    first, _ = env.reset(seed=17)
    second, _ = env.reset(seed=18)
    assert not np.array_equal(first, second)
    assert not Drone().make_env().randomize
    env.close()
