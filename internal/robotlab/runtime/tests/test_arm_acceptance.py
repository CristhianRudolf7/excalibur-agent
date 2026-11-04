"""Reachability, pick-and-place, cameras, speed and simulation throughput."""
import math
import os
import time

import mujoco
import numpy as np
import pytest

from robotlab.robots.arm import ArmRobot
from robotlab.robots.base import ActionInterrupted, Spawn
from robotlab.robots.testing import SimpleContext, build


def make_arm(cubes=False, spawn=Spawn(-0.75, 0, 0)):
    robot = ArmRobot()
    objects = [{'id': 'table', 'shape': 'box', 'size': [0.6, 0.4, 0.025],
                'pos': [0, 0, 0.725], 'dynamic': False}]
    if cubes:
        objects += [{'id': name, 'shape': 'box', 'size': [0.025] * 3,
                     'pos': pos, 'mass': 0.1}
                    for name, pos in [('red', [-0.15, 0, 0.775]),
                                      ('blue', [-0.35, -0.2, 0.775]),
                                      ('green', [0.15, -0.2, 0.775])]]
    model, data = build(robot, objects, spawn)
    return robot, model, data


class ObservedContext(SimpleContext):
    def __init__(self, robot, model, data):
        super().__init__(robot, model, data)
        self.positions = []
        self.speeds = []
        self.joints = []
        self.velocity = np.zeros(6)

    def step(self, seconds):
        dt = self.model.opt.timestep
        for _ in range(max(1, round(seconds / dt))):
            super().step(dt)
            self.positions.append(self.data.site_xpos[self.robot.site].copy())
            mujoco.mj_objectVelocity(self.model, self.data, mujoco.mjtObj.mjOBJ_SITE,
                                     self.robot.site, self.velocity, 0)
            self.speeds.append(np.linalg.norm(self.velocity[3:]))
            self.joints.append(self.data.qpos[self.robot.qadr].copy())


def test_ten_random_reachable_points_within_one_centimeter():
    robot, model, data = make_arm(cubes=True)
    ctx = ObservedContext(robot, model, data)
    rng = np.random.default_rng(2026)
    for i in range(10):
        point = rng.uniform([-0.4, -0.24, 0.92], [0.15, 0.24, 1.3])
        yaw = float(rng.uniform(-85, 85))
        result = robot.tools()['move_to'].fn(ctx, *point, yaw_deg=yaw)
        assert result['ok'], (i, point, result)
        assert np.linalg.norm(data.site_xpos[robot.site] - point) < 0.01
        assert abs(robot.state(model, data)['yaw_deg'] - yaw) < 1
        assert np.allclose(data.site_xmat[robot.site].reshape(3, 3)[:, 2], [0, 0, -1], atol=0.01)
        assert robot.fallen(model, data) is None
    joints = np.array(ctx.joints)
    assert np.all(joints >= robot.limits[:, 0] - 0.001)
    assert np.all(joints <= robot.limits[:, 1] + 0.001)
    assert max(ctx.speeds) < 0.5


def test_cube_pick_lift_place_and_release():
    robot, model, data = make_arm(cubes=True)
    ctx = SimpleContext(robot, model, data)
    assert robot.move_to(ctx, -0.15, 0, 0.92)['ok']
    assert robot.move_to(ctx, -0.15, 0, 0.785)['ok']
    initial_z = ctx.object_pose('red')[0][2]
    result = robot.tools()['close_gripper'].fn(ctx)
    assert result['ok'] and result['holding'] == 'red', result
    assert ctx.held() == 'red'
    assert robot.state(model, data)['holding'] == 'red'
    result = robot.tools()['lift'].fn(ctx, dz=0.2)
    assert result['ok'], result
    assert ctx.object_pose('red')[0][2] - initial_z >= 0.15
    destination = np.array([0.1, 0.15])
    assert robot.move_to(ctx, *destination, 1.0)['ok']
    result = robot.tools()['open_gripper'].fn(ctx)
    assert result['ok'] and result['released'] == 'red', result
    assert ctx.held() is None
    assert robot.state(model, data)['holding'] is None
    pos, _ = ctx.object_pose('red')
    assert np.linalg.norm(pos[:2] - destination) < 0.03
    assert abs(pos[2] - 0.775) < 0.005
    assert abs(ctx.object_pose('blue')[0][2] - 0.775) < 0.005
    assert abs(ctx.object_pose('green')[0][2] - 0.775) < 0.005


def test_grasp_assist_does_not_pick_distant_objects():
    robot, model, data = make_arm(cubes=True)
    ctx = SimpleContext(robot, model, data)
    assert robot.move_to(ctx, -0.15, 0, 0.85)['ok']
    result = robot.close_gripper(ctx)
    assert result['ok'] and result['holding'] is None
    assert ctx.held() is None


def test_close_gripper_exposes_attachment_to_step_goal_check():
    robot, model, data = make_arm(cubes=True)
    ctx = SimpleContext(robot, model, data)
    assert robot.move_to(ctx, -0.15, 0, 0.785)['ok']
    normal_step = ctx.step

    def goal_step(seconds):
        normal_step(seconds)
        if ctx.held() == 'red':
            raise ActionInterrupted('goal', 'object held')

    ctx.step = goal_step
    with pytest.raises(ActionInterrupted, match='goal'):
        robot.close_gripper(ctx)
    assert robot.state(model, data)['holding'] == 'red'
    assert np.array_equal(robot._target, data.qpos[robot.qadr])


def test_straight_cartesian_path_and_runtime_speed_clamp():
    robot, model, data = make_arm()
    ctx = ObservedContext(robot, model, data)
    start = data.site_xpos[robot.site].copy()
    target = np.array([0.08, 0.2, 1.05])
    result = robot.move_to(ctx, *target, speed=1000)
    assert result['ok'], result
    positions = np.array(ctx.positions)
    direction = (target - start) / np.linalg.norm(target - start)
    offsets = positions - start
    errors = offsets - np.outer(offsets @ direction, direction)
    assert np.max(np.linalg.norm(errors, axis=1)) < 0.01
    assert max(ctx.speeds) <= 0.5
    assert np.min(positions[:, 2]) >= 0.76


def test_joint_limit_clamp_executes_safe_wrist_rotation():
    robot, model, data = make_arm()
    ctx = ObservedContext(robot, model, data)
    target = np.rad2deg(data.qpos[robot.qadr]).tolist()
    target[-1] = 10000
    result = robot.move_joints(ctx, target, speed=1000)
    assert result['ok'] and result['clamped'], result
    assert abs(data.qpos[robot.qadr[-1]] - robot.limits[-1, 1]) < 0.004
    assert max(ctx.speeds) <= 0.5


def test_robot_cameras_render_and_overhead_covers_table():
    robot, model, data = make_arm(cubes=True, spawn=Spawn(-0.75, 0, math.radians(15)))
    with mujoco.Renderer(model, height=120, width=160) as renderer:
        for name in robot.card()['cameras']:
            renderer.update_scene(data, camera=name)
            pixels = renderer.render()
            assert pixels.shape == (120, 160, 3)
            assert pixels.std() > 5
    overhead = model.camera('robot/overhead').id
    assert np.allclose(data.cam_xpos[overhead], [0, 0, 1.95])
    assert np.allclose(data.cam_xmat[overhead].reshape(3, 3)[:, 2], [0, 0, 1])
    height = 2 * 1.2 * math.tan(math.radians(model.cam_fovy[overhead] / 2))
    assert height >= 0.8 and height * 4 / 3 >= 1.2


def test_ten_sim_seconds_faster_than_ten_wall_seconds_on_one_core(record_property):
    robot, model, data = make_arm(cubes=True)
    ctx = SimpleContext(robot, model, data)
    affinity = os.sched_getaffinity(0)
    try:
        os.sched_setaffinity(0, {min(affinity)})
        started = time.perf_counter()
        ctx.step(10)
        elapsed = time.perf_counter() - started
    finally:
        os.sched_setaffinity(0, affinity)
    record_property('ten_sim_seconds_wall_seconds', elapsed)
    print(f'10 simulated seconds on one core: {elapsed:.3f} wall seconds')
    assert data.time == pytest.approx(10)
    assert elapsed < 10
    assert robot.fallen(model, data) is None
