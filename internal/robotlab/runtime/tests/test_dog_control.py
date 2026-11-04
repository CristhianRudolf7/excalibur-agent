"""Mounted-arm controller acceptance scenarios against real rigid-body physics."""
import time

import numpy as np

from robotlab.robots.dog import DogRobot
from robotlab.robots.testing import SimpleContext, build


def setup(objects=None):
    robot = DogRobot()
    model, data = build(robot, objects)
    return robot, model, data, SimpleContext(robot, model, data)


def test_stand_speed():
    robot, model, data, ctx = setup()
    start = time.perf_counter()
    ctx.step(10)
    wall = time.perf_counter()-start
    print('stand', robot.state(model, data), 'sim/wall', 10/wall)
    assert robot.fallen(model, data) is None
    assert data.xpos[robot.base, 2] > .23
    assert wall < 10


def test_walk_forward():
    robot, model, data, ctx = setup()
    result = robot.walk(ctx, 2.)
    print('walk', result)
    assert result['ok']
    assert np.linalg.norm(data.xpos[robot.base, :2]-[2,0]) < .25
    assert abs(robot.state(model,data)['yaw_deg']) < 15


def test_turn():
    robot, model, data, ctx = setup()
    result = robot.turn(ctx, 90)
    print('turn', result)
    assert result['ok']
    assert abs(robot.state(model,data)['yaw_deg']-90) < 10


def test_walk_to_diagonal():
    robot, model, data, ctx = setup()
    target = np.array([2**.5,2**.5])
    result = robot.walk_to(ctx, *target)
    print('diagonal', result)
    assert result['ok']
    assert np.linalg.norm(data.xpos[robot.base, :2]-target) < .2

