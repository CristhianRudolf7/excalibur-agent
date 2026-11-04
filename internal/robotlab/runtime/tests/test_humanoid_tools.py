import math

import pytest

from robotlab.robots.base import ActionInterrupted, Spawn
from robotlab.robots.humanoid import HumanoidRobot
from robotlab.robots.testing import SimpleContext, build


@pytest.fixture
def body():
    robot = HumanoidRobot()
    model, data = build(robot)
    return robot, model, data, SimpleContext(robot, model, data)


@pytest.mark.parametrize('depth', [.1, .25])
def test_squat_and_rise(body, depth):
    robot, model, data, ctx = body
    robot.stand(ctx)
    print('squat', robot.squat(ctx, depth))
    assert abs(data.xpos[robot.pelvis, 2] - (robot.height - depth)) < .04
    print('rise', robot.stand(ctx))
    assert abs(data.xpos[robot.pelvis, 2] - robot.height) < .04


def test_turn(body):
    robot, model, data, ctx = body
    print('turn', robot.turn(ctx, 45))
    assert abs(robot.state(model, data)['yaw_deg'] - 45) <= 15
    assert math.hypot(*robot.base_xy(model, data)) < .20


def test_walk_to(body):
    robot, model, data, ctx = body
    print('walk_to', robot.walk_to(ctx, 1, 0))
    assert math.hypot(data.xpos[robot.pelvis, 0] - 1, data.xpos[robot.pelvis, 1]) <= .25


def test_aggressive_gait_falls(body):
    robot, model, data, ctx = body
    with pytest.raises(ActionInterrupted, match='fell'):
        robot.walk(ctx, 1, step_length=.35, step_time=.3)
    assert robot.fallen(model, data)
    print('aggressive', data.time, robot.state(model, data))


def test_step(body):
    robot, model, data, ctx = body
    robot.stand(ctx)
    print('step', robot.step(ctx, 'left', .08, 0))
    assert data.site_xpos[robot.sites[0], 0] > .04


@pytest.mark.parametrize('pose', ['down', 'forward', 'up', 'wave'])
def test_arm_poses(body, pose):
    robot, model, data, ctx = body
    robot.stand(ctx)
    result = robot.move_arm(ctx, 'both', pose=pose)
    print('arm', pose, result)
    assert result['ok']
    assert robot.fallen(model, data) is None


def test_arm_joints_and_hands(body):
    robot, model, data, ctx = body
    robot.stand(ctx)
    print(robot.move_arm(ctx, 'left', joints_deg=[-50, 15, 0, 60, 0, 0, 0]))
    print(robot.hand(ctx, 'both', 'close'))
    print(robot.hand(ctx, 'both', 'open'))
    assert robot.hand_states == {'left': 'open', 'right': 'open'}


def test_spawn():
    robot = HumanoidRobot()
    model, data = build(robot, spawn=Spawn(2, -1, math.pi / 2))
    print('spawn', robot.state(model, data))
    assert robot.base_xy(model, data) == pytest.approx((2, -1))
    assert robot.state(model, data)['yaw_deg'] == pytest.approx(90)


def test_transformed_spawn_walks_forward():
    robot = HumanoidRobot()
    model, data = build(robot, spawn=Spawn(2, -1, math.pi / 2))
    ctx = SimpleContext(robot, model, data)
    result = robot.walk(ctx, .3)
    print('rotated walk', result)
    assert result['ok']
    assert data.xpos[robot.pelvis, 1] == pytest.approx(-.7, abs=.1)
    assert data.xpos[robot.pelvis, 0] == pytest.approx(2, abs=.1)


def test_walk_to_diagonal():
    robot = HumanoidRobot()
    model, data = build(robot)
    ctx = SimpleContext(robot, model, data)
    result = robot.walk_to(ctx, .5, .5)
    print('diagonal walk_to', result)
    assert result['ok']
    assert result['error_m'] <= .25
