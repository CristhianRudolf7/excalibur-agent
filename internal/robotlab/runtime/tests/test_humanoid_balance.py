"""Locomotion acceptance and one-core controller performance."""
import os
import time

from robotlab.robots.humanoid import HumanoidRobot
from robotlab.robots.testing import SimpleContext, build


def test_stand_ten_seconds_on_one_core():
    robot = HumanoidRobot()
    model, data = build(robot)
    ctx = SimpleContext(robot, model, data)
    previous = os.sched_getaffinity(0)
    try:
        os.sched_setaffinity(0, {min(previous)})
        wall_start, cpu_start = time.perf_counter(), time.process_time()
        result = robot.stand(ctx, 10)
        wall, cpu = time.perf_counter() - wall_start, time.process_time() - cpu_start
    finally:
        os.sched_setaffinity(0, previous)
    print(f'10 s sim: {wall:.3f} s wall, {cpu:.3f} s CPU, {10 / wall:.2f}x real time, one CPU core')
    assert result['ok']
    assert robot.fallen(model, data) is None
    assert data.time >= 9.99
    assert wall < 10
    assert result['state']['tilt_deg'] < 5


def test_default_walk_half_meter():
    robot = HumanoidRobot()
    model, data = build(robot)
    ctx = SimpleContext(robot, model, data)
    result = robot.walk(ctx, .6)
    print('default walk:', result)
    assert result['ok']
    assert result['steps_done'] >= 5
    assert result['distance_achieved'] >= .5
    assert abs(result['error_m']) < .15
    assert result['max_tilt_deg'] < 45
    assert robot.fallen(model, data) is None
