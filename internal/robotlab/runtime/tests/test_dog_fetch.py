"""Deterministic fetch with the mounted physical arm and a 0.2 kg ball."""
import time

import numpy as np

from robotlab.robots.dog import DogRobot
from robotlab.robots.testing import SimpleContext, build


def fetch():
    robot = DogRobot()
    ball = dict(id='ball', shape='sphere', size=[.06], pos=[1.5, 0, .06], mass=.2)
    model, data = build(robot, [ball])
    ctx = SimpleContext(robot, model, data)
    start = time.perf_counter()
    assert robot.walk_to(ctx, 1.16, 0)['ok']
    assert robot.crouch(ctx, .22)['ok']
    pos, _ = ctx.object_pose('ball')
    assert robot.arm_move_to(ctx, pos[0], pos[1], pos[2]+.15)['ok']
    pos, _ = ctx.object_pose('ball')
    assert robot.arm_move_to(ctx, *pos)['ok']
    assert robot.close_gripper(ctx)['held'] == 'ball'
    assert robot.state(model, data)['holding'] == 'ball'
    assert robot.arm_move_to(ctx, 1.5, 0, .3)['ok']
    assert robot.arm_stow(ctx)['ok']
    returned = robot.walk_to(ctx, 0, 0)
    assert returned['ok'], returned
    assert ctx.held() == 'ball'
    assert np.linalg.norm(ctx.object_pose('ball')[0][:2]) < .5
    assert robot.fallen(model, data) is None
    print(f'fetch: {data.time:.2f} sim s / {time.perf_counter()-start:.2f} wall s')
    # Finish by releasing: both the simulator and the body state must agree.
    assert robot.open_gripper(ctx)['released'] == 'ball'
    assert ctx.held() is None
    assert robot.state(model, data)['holding'] is None
    return data.qpos.copy()


def test_fetch_is_deterministic():
    first = fetch()
    second = fetch()
    np.testing.assert_array_equal(first, second)


def test_stow_with_held_ball_after_lift_allows_walking_back():
    from robotlab.robots.dog import DogRobot
    from robotlab.robots import testing
    robot = DogRobot()
    model, data = testing.build(robot, [{'id': 'b', 'shape': 'sphere', 'size': [0.035], 'pos': [2.0, 0, 0.035], 'mass': 0.1}])
    ctx = testing.SimpleContext(robot, model, data)
    assert robot.walk_to(ctx, 1.5, 0)['ok'] and robot.crouch(ctx, .22)['ok']
    pos = ctx.object_pose('b')[0]
    assert robot.arm_move_to(ctx, pos[0], pos[1], pos[2] + .04)['ok']
    assert robot.close_gripper(ctx)['ok'] and ctx.held() == 'b'
    assert robot.arm_move_to(ctx, 1.75, 0, .35)['ok']
    assert robot.arm_stow(ctx)['ok']
    assert robot.walk_to(ctx, 0, 0)['ok']
    assert ctx.held() == 'b'
