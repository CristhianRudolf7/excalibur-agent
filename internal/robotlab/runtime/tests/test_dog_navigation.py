"""Furniture-aware locomotion acceptance tests with rigid-body physics."""
import time

import numpy as np
import pytest

from robotlab.assets import menagerie_dir
from robotlab.core.context import ToolContext
from robotlab.robots.base import ActionInterrupted
from robotlab.robots.dog import DogRobot
from robotlab.robots.testing import SimpleContext, build
from robotlab.world.builder import build_world


@pytest.mark.parametrize('tool,args,position', [
    ('walk', {'distance': 2}, [1, 0, .4]),
    ('walk_to', {'x': 2, 'y': 0}, [1, 0, .4]),
    ('side_step', {'distance': 2}, [0, 1, .4]),
])
def test_fixed_box_stops_motion(tool, args, position):
    robot = DogRobot()
    box = dict(id='cabinet', shape='box', size=[.3, .6, .4],
               pos=position, dynamic=False)
    model, data = build(robot, [box])
    ctx = SimpleContext(robot, model, data)
    start = time.perf_counter()
    result = getattr(robot, tool)(ctx, **args)
    wall = time.perf_counter()-start
    print(f'{tool} blocked: {data.time:.2f} sim s / {wall:.2f} wall s: {result}')
    assert not result['ok']
    assert result['blocking_object'] == 'cabinet'
    assert 'blocked by cabinet after' in result['error']
    np.testing.assert_allclose(result['position'], data.xpos[robot.base])
    assert 0 <= result['distance_done'] < 1
    assert result['distance_to_target'] > 1
    assert data.time < 12
    assert robot.height == .30
    assert np.linalg.norm(robot.velocity) == 0
    assert robot.turn_rate == 0
    assert np.linalg.norm(data.qvel[robot.rv:robot.rv+2]) < .08
    assert robot.fallen(model, data) is None


@pytest.mark.parametrize('distance,speed', [(1., .6), (15., .6)])
def test_walk_to_deadline_includes_preparation_and_settling(monkeypatch, distance, speed):
    robot = DogRobot()
    model, data = build(robot)
    ctx = SimpleContext(robot, model, data)
    # A deterministic slow actuator makes progress above the stall threshold
    # but cannot reach the target before either the short or 60-second bound.
    def slow_step(seconds):
        data.time += seconds
        if np.linalg.norm(robot.velocity) > 0:
            data.xpos[robot.base, 0] += .1*seconds
    monkeypatch.setattr(ctx, 'step', slow_step)
    result = robot.walk_to(ctx, distance, 0, speed=speed)
    bound = min(60., 2.5*distance/speed+3.)
    assert not result['ok']
    assert result['stopped'] == 'timeout'
    assert 'simulation timeout' in result['error']
    assert bound-.02 <= data.time <= bound+1e-8
    assert result['distance_to_target'] == pytest.approx(distance-data.xpos[robot.base, 0])
    assert result['distance_done'] == pytest.approx(data.xpos[robot.base, 0])
    assert robot.height == .30
    assert np.linalg.norm(robot.velocity) == 0
    assert robot.turn_rate == 0


def test_walk_to_deadline_stops_turning(monkeypatch):
    robot = DogRobot()
    model, data = build(robot)
    ctx = SimpleContext(robot, model, data)
    # Frozen orientation must consume the same budget as forward travel.
    monkeypatch.setattr(ctx, 'step', lambda seconds: setattr(data, 'time', data.time+seconds))
    result = robot.walk_to(ctx, -.1, 0)
    assert not result['ok']
    assert result['stopped'] == 'timeout'
    assert data.time <= 2.5*.1/.3+3.+1e-8
    assert result['distance_to_target'] == pytest.approx(.1)
    assert robot.turn_rate == 0


def test_walk_to_reports_success_distance():
    robot = DogRobot()
    model, data = build(robot)
    result = robot.walk_to(SimpleContext(robot, model, data), .5, 0)
    assert result['ok'], result
    assert result['distance_to_target'] < .1
    assert result['distance_to_target'] == pytest.approx(np.linalg.norm(data.xpos[robot.base, :2]-[.5, 0]))


def test_walk_path_routes_around_fixed_box():
    robot = DogRobot()
    box = dict(id='cabinet', shape='box', size=[.3, .4, .4], pos=[1, 0, .4], dynamic=False)
    model, data = build(robot, [box])
    ctx = SimpleContext(robot, model, data)
    contacts = []
    original_step = ctx.step
    def record_contacts(seconds):
        original_step(seconds)
        contacts.extend(robot._blocking_contacts(ctx))
    ctx.step = record_contacts
    start = time.perf_counter()
    result = robot.walk_path(ctx, [[0, -1.1], [2, -1.1], [2, 0]])
    print(f'path around cabinet: {data.time:.2f} sim s / {time.perf_counter()-start:.2f} wall s: {result}')
    assert result['ok'], result
    assert result['waypoints_completed'] == 3
    assert result['waypoint_index'] == 2
    assert result['distance_to_target'] < .1
    assert result['path_distance_done'] > 3.8
    assert np.linalg.norm(data.xpos[robot.base, :2]-[2, 0]) < .1
    assert data.time < 60
    assert contacts == []
    assert robot.fallen(model, data) is None


def test_walk_path_stops_at_blocked_segment():
    robot = DogRobot()
    box = dict(id='cabinet', shape='box', size=[.3, .6, .4], pos=[1, 0, .4], dynamic=False)
    model, data = build(robot, [box])
    result = robot.walk_path(SimpleContext(robot, model, data), [[0, 0], [2, 0], [3, 0]])
    assert not result['ok']
    assert result['blocking_object'] == 'cabinet'
    assert result['waypoints_completed'] == 1
    assert result['waypoint_index'] == 1
    assert data.time < 12


@pytest.mark.parametrize('points', [[], [[0, 0]]*9, [[0]], [[0, 0, 0]], [None], [[0, 0], [float('nan'), 1]]])
def test_walk_path_validates_before_moving(points):
    robot = DogRobot()
    model, data = build(robot)
    with pytest.raises(ValueError):
        robot.walk_path(SimpleContext(robot, model, data), points)
    assert data.time == 0


def test_walk_path_tool_schema():
    robot = DogRobot()
    params = robot.tools()['walk_path'].params
    assert params['required'] == ['points']
    points = params['properties']['points']
    assert points['minItems'] == 1
    assert points['maxItems'] == 8
    assert points['items']['minItems'] == points['items']['maxItems'] == 2


@pytest.mark.parametrize('obstacle', ['wall', 'compound'])
def test_world_contacts_report_wall_or_compound_object_id(obstacle):
    robot = DogRobot()
    env = dict(id='dog-contacts', size=[2, 3])
    if obstacle == 'compound':
        env.update(size=[5, 4], objects=[dict(id='bin', shape='box', size=[.6, 1.2, .8],
                                           pos=[1, 0, .4], compound=True, dynamic=False)])
    world = build_world(env, robot, menagerie_dir())
    start = time.perf_counter()
    result = robot.walk(ToolContext(world), 2)
    print(f'{obstacle} blocked: {world.data.time:.2f} sim s / {time.perf_counter()-start:.2f} wall s')
    assert not result['ok']
    assert result['blocking_object'] == ('wall/1' if obstacle == 'wall' else 'bin')
    assert world.data.time < 12


def test_remaining_core_budget_returns_result_before_guard(monkeypatch):
    robot = DogRobot()
    model, data = build(robot)
    ctx = SimpleContext(robot, model, data)
    ctx.action_time, ctx.limit = 0., 2.
    data.time = 1.
    def limited_step(seconds):
        if data.time+seconds > ctx.limit:
            raise ActionInterrupted('timeout')
        data.time += seconds
    monkeypatch.setattr(ctx, 'step', limited_step)
    result = robot.walk_to(ctx, 1, 0)
    assert not result['ok']
    assert result['stopped'] == 'timeout'
    assert data.time == pytest.approx(1.99, abs=.002)
    assert np.linalg.norm(robot.velocity) == 0


def test_unidentified_stall_keeps_blocked_result_when_settling_is_cut_short(monkeypatch):
    robot = DogRobot()
    model, data = build(robot)
    ctx = SimpleContext(robot, model, data)
    ctx.step(.1)
    ctx.action_time, ctx.limit = 0., 2.
    monkeypatch.setattr(ctx, 'step', lambda seconds: setattr(data, 'time', data.time+seconds))
    result = robot.walk_to(ctx, 1, 0)
    assert not result['ok']
    assert result['blocking_object'] is None
    assert 'blocked by unknown obstacle' in result['error']
    assert result['distance_done'] == 0
    assert data.time <= 1.99+1e-8
    assert robot.height == .30


def test_walk_path_has_one_total_budget(monkeypatch):
    robot = DogRobot()
    model, data = build(robot)
    ctx = SimpleContext(robot, model, data)
    def slow_step(seconds):
        data.time += seconds
        if np.linalg.norm(robot.velocity) > 0:
            data.xpos[robot.base, 0] += .05*seconds
    monkeypatch.setattr(ctx, 'step', slow_step)
    result = robot.walk_path(ctx, [[2, 0], [4, 0], [6, 0]], speed=.05)
    assert not result['ok']
    assert result['waypoints_completed'] == 1
    assert result['waypoint_index'] == 1
    assert result['stopped'] == 'timeout'
    assert 59.97 <= data.time < 60


def test_path_goal_interrupt_clears_motion():
    robot = DogRobot()
    world = build_world(dict(id='dog-goal', size=[5, 4]), robot, menagerie_dir())
    ctx = ToolContext(world, goal=dict(type='robot_near', target=dict(point=[.5, 0]), radius=.1))
    with pytest.raises(ActionInterrupted) as exc:
        robot.walk_path(ctx, [[.5, 0], [1, 0]])
    assert exc.value.reason == 'goal'
    assert ctx.goal_result()['met']
    assert np.linalg.norm(robot.velocity) == 0
    assert robot.turn_rate == 0


def test_insufficient_free_floor_progress_does_not_invent_object_id():
    robot = DogRobot()
    model, data = build(robot)
    result = robot.walk_to(SimpleContext(robot, model, data), .3, 0, speed=.05)
    # At the minimum command speed the existing gait does not make 0.05 m of
    # forward progress per 1.5 s. Apply the prescribed threshold consistently.
    assert not result['ok']
    assert result['blocking_object'] is None
    assert 'blocked by unknown obstacle' in result['error']
    assert result['distance_to_target'] > .2
    assert data.time < 6
