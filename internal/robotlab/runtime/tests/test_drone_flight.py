"""Physics acceptance tests for the four-rotor controller."""
import os
import time

import numpy as np
import pytest

from robotlab.robots.drone import DroneRobot
from robotlab.robots.testing import SimpleContext, build


@pytest.fixture
def flight():
    robot = DroneRobot()
    model, data = build(robot)
    return robot, SimpleContext(robot, model, data)


def call(robot, ctx, name, **args):
    result = robot.tools()[name].fn(ctx, **args)
    assert result['ok'], result
    return result


def test_takeoff_hover_and_performance(flight):
    robot, ctx = flight
    assert not robot.state(ctx.model, ctx.data)['motors_on']
    call(robot, ctx, 'takeoff')
    assert abs(robot.state(ctx.model, ctx.data)['z'] - 1) < 0.05
    start = robot._position(ctx.data).copy()
    drift = []
    original_step = ctx.step

    def monitored_step(seconds):
        for _ in range(round(seconds / 0.02)):
            original_step(0.02)
            drift.append(float(np.linalg.norm(robot._position(ctx.data) - start)))

    ctx.step = monitored_step
    call(robot, ctx, 'hover', seconds=5)
    assert max(drift) < 0.05
    ctx.step = original_step
    allowed = os.sched_getaffinity(0)
    try:
        os.sched_setaffinity(0, {min(allowed)})
        started = time.perf_counter()
        call(robot, ctx, 'hover', seconds=10)
        elapsed = time.perf_counter() - started
    finally:
        os.sched_setaffinity(0, allowed)
    print(f'10 s flight on one core: {elapsed:.3f} s wall; maximum 5 s hover drift: {max(drift):.4f} m')
    assert elapsed < 10


def test_waypoints_turn_relative_move_and_land(flight):
    robot, ctx = flight
    call(robot, ctx, 'takeoff')
    for p in ([0.8, 0.2, 1.2], [-0.4, 0.8, 0.7], [0.1, -0.4, 1.0]):
        result = call(robot, ctx, 'fly_to', x=p[0], y=p[1], z=p[2])
        assert result['error_m'] < 0.1
    call(robot, ctx, 'turn', angle_deg=90)
    assert abs(robot.state(ctx.model, ctx.data)['yaw_deg'] - 90) < 5
    start = robot._position(ctx.data).copy()
    call(robot, ctx, 'move', forward=0.4, left=0.2, up=0.1)
    assert np.linalg.norm(robot._position(ctx.data) - (start + [-0.2, 0.4, 0.1])) < 0.1
    call(robot, ctx, 'land')
    assert abs(robot.state(ctx.model, ctx.data)['z']) < 0.04
    assert not robot._motors
    assert robot.fallen(ctx.model, ctx.data) is None
