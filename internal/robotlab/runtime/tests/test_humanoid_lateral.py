"""Lateral shuffle acceptance with physical contacts on every simulation step."""
import time
import math
import json
from importlib.resources import files

import numpy as np
import pytest

from robotlab.robots.humanoid import HumanoidRobot
from robotlab.robots.base import Spawn
from robotlab.robots.testing import SimpleContext, build
from robotlab.assets import menagerie_dir
from robotlab.world.builder import build_world


@pytest.mark.parametrize('foot', ['left', 'right'])
@pytest.mark.parametrize('dy', [.12, .15, -.12, -.15])
@pytest.mark.parametrize('settled', [False, True])
def test_lateral_step(foot, dy, settled):
    robot = HumanoidRobot()
    model, data = build(robot)
    ctx = SimpleContext(robot, model, data)
    if settled:
        robot.stand(ctx)
    start = data.xpos[robot.pelvis].copy()
    feet_y = data.site_xpos[robot.sites, 1].copy()
    wall = time.perf_counter()
    result = robot.step(ctx, foot, 0, dy)
    print(f'{foot} dy={dy} settled={settled}: sim={data.time:.2f}s wall={time.perf_counter() - wall:.2f}s {result}')
    assert result['ok']
    assert robot.fallen(model, data) is None
    assert data.xpos[robot.pelvis, 1] - start[1] == pytest.approx(dy, abs=.04)
    np.testing.assert_allclose(data.site_xpos[robot.sites, 1] - feet_y, dy, atol=.04)


@pytest.mark.parametrize('distance', [.5, -.5])
@pytest.mark.parametrize('width', [.10, .15])
def test_side_step(distance, width):
    robot = HumanoidRobot()
    model, data = build(robot)
    ctx = SimpleContext(robot, model, data)
    wall = time.perf_counter()
    result = robot.tools()['side_step'].fn(ctx, distance=distance, step_width=width)
    print(f'side_step {distance} width={width}: sim={data.time:.2f}s wall={time.perf_counter() - wall:.2f}s {result}')
    assert result['ok']
    assert result['distance_achieved'] == pytest.approx(distance, abs=.05)
    assert abs(result['state']['x']) < .05
    assert abs(result['state']['yaw_deg']) < 5
    assert result['max_tilt_deg'] < 20
    assert result['steps_done'] == 2 * math.ceil(abs(distance) / width)
    assert robot.fallen(model, data) is None
    assert data.time < 60


def test_side_step_respects_rotated_heading():
    robot = HumanoidRobot()
    model, data = build(robot, spawn=Spawn(1, -1, math.pi / 2))
    result = robot.side_step(SimpleContext(robot, model, data), .3)
    assert result['ok']
    assert result['state']['x'] == pytest.approx(.7, abs=.05)
    assert result['state']['y'] == pytest.approx(-1, abs=.05)
    assert result['state']['yaw_deg'] == pytest.approx(90, abs=5)


def test_zero_side_step_does_not_advance_physics():
    robot = HumanoidRobot()
    model, data = build(robot)
    result = robot.side_step(SimpleContext(robot, model, data), 0)
    assert result['ok']
    assert result['distance_achieved'] == 0
    assert result['max_tilt_deg'] == 0
    assert result['steps_done'] == 0
    assert data.time == 0


@pytest.mark.parametrize('distance', [.15, -.15])
def test_shuffle_transfers_com_before_lifting(distance):
    robot = HumanoidRobot()
    model, data = build(robot)
    phase = robot._phase
    supports = []

    def observe(ctx, duration, update):
        if update.__name__ == 'swing':
            stance = 1 - robot.swing_foot
            supports.append(stance)
            # The dynamic transfer must bring COM close to the stance sole;
            # it still decelerates at lift-off, so its projection need not be
            # at the static center of pressure.
            com = data.subtree_com[robot.pelvis]
            assert abs(com[1] - data.site_xpos[robot.sites[stance], 1]) < .05
            other = data.site_xpos[robot.sites[1 - stance], 1]
            assert abs(com[1] - other) > .18
            assert robot.weights[stance] == 1
        phase(ctx, duration, update)

    robot._phase = observe
    result = robot.side_step(SimpleContext(robot, model, data), distance, step_width=.15)
    assert result['ok']
    assert supports == ([1, 0] if distance > 0 else [0, 1])


def test_corridor_chained_locomotion():
    env = json.loads(files('robotlab.world').joinpath('defaults/pasillo.json').read_text())
    robot = HumanoidRobot()
    world = build_world(env, robot, menagerie_dir())
    model, data = world.model, world.data
    ctx = SimpleContext(robot, model, data)
    wall = time.perf_counter()
    timings = []
    for name, args in [('walk', {'distance': .45}), ('turn', {'angle_deg': 30}),
                       ('walk', {'distance': .9}), ('side_step', {'distance': -.3}),
                       ('walk', {'distance': .8})]:
        sim_start, wall_start = data.time, time.perf_counter()
        result = robot.tools()[name].fn(ctx, **args)
        timings.append((name, data.time - sim_start, time.perf_counter() - wall_start))
        print(f'chained {name} {args}: {timings[-1]} {result}')
        assert result['ok']
        assert robot.fallen(model, data) is None
        assert data.time - sim_start < 60
    print(f'chained total: {data.time:.2f}s sim, {time.perf_counter() - wall:.2f}s wall; {timings}')
    assert robot.state(model, data)['yaw_deg'] == pytest.approx(30, abs=5)
    assert robot.state(model, data)['x'] == pytest.approx(-1.13, abs=.15)
    assert robot.state(model, data)['y'] == pytest.approx(.59, abs=.15)
