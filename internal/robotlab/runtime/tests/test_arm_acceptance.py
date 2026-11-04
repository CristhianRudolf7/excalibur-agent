"""Real-physics path, contacts and small shelf feasibility proofs."""
import math
import os
import time

import mujoco
import numpy as np
import pytest

from robotlab.robots.arm import ArmRobot
from robotlab.robots.base import Spawn
from robotlab.robots.testing import SimpleContext, build


def make_arm(cubes=False, spawn=Spawn(-.75, 0, 0)):
    robot = ArmRobot()
    objects = [{'id': 'table', 'shape': 'box', 'size': [.6, .4, .025],
                'pos': [0, 0, .725], 'dynamic': False}]
    if cubes:
        objects += [{'id': 'box', 'shape': 'box', 'size': [.035] * 3,
                     'pos': [-.2, -.15, .785], 'mass': .15}]
    model, data = build(robot, objects, spawn)
    return robot, model, data


class ObservedContext(SimpleContext):
    def __init__(self, robot, model, data):
        super().__init__(robot, model, data)
        self.positions, self.speeds, self.joints = [], [], []
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


def test_line_path_stays_within_one_centimeter_of_segment():
    robot, model, data = make_arm()
    ctx = ObservedContext(robot, model, data)
    start, end = data.site_xpos[robot.site].copy(), np.array([.08, .2, 1.05])
    result = robot.move_gripper_to(ctx, *end, duration=4)
    assert result['ok'], result
    positions, delta = np.array(ctx.positions), end - start
    t = np.clip((positions - start) @ delta / (delta @ delta), 0., 1.)
    errors = positions - (start + t[:, None] * delta)
    assert np.max(np.linalg.norm(errors, axis=1)) < .01
    assert np.linalg.norm(positions[-1] - end) < .004 and max(ctx.speeds) < .5


def test_random_reachable_targets_and_orientation():
    robot, model, data = make_arm()
    ctx, rng = SimpleContext(robot, model, data), np.random.default_rng(2026)
    for _ in range(6):
        point = rng.uniform([-.4, -.24, .92], [.1, .24, 1.3])
        yaw = float(rng.uniform(-60, 60))
        result = robot.move_gripper_to(ctx, *point, yaw=yaw, duration=4)
        assert result['ok'], result
        assert np.linalg.norm(data.site_xpos[robot.site] - point) < .01
        assert result['state']['gripper']['yaw_deg'] == pytest.approx(yaw, abs=.5)


def test_grasp_requires_actual_bilateral_pad_contact():
    robot, model, data = make_arm(cubes=True)
    ctx = SimpleContext(robot, model, data)
    assert robot.move_gripper_to(ctx, -.2, -.15, .795, duration=4)['ok']
    result = robot.gripper(ctx, 0, duration=1.5)
    assert result['ok'] and result['state']['held'] == 'box', result
    assert result['state']['gripper']['contacts'] == {'left': 'box', 'right': 'box'}
    assert robot.move_gripper_to(ctx, -.2, -.15, 1.1, duration=4)['ok']
    assert ctx.object_pose('box')[0][2] > 1.05
    assert robot.gripper(ctx, .4)['state']['held'] == 'box'
    result = robot.gripper(ctx, 1)
    assert result['ok'] and result['state']['held'] is None, result


def test_grasp_does_not_assist_a_nearby_object_without_pad_contact():
    robot, model, data = make_arm(cubes=True)
    ctx = SimpleContext(robot, model, data)
    assert robot.move_gripper_to(ctx, -.2, -.15, .87, duration=4)['ok']
    result = robot.gripper(ctx, 0)
    assert result['ok'] and result['state']['held'] is None, result


def test_robot_cameras_render_and_overhead_covers_table():
    robot, model, data = make_arm(cubes=True, spawn=Spawn(-.75, 0, math.radians(15)))
    with mujoco.Renderer(model, height=120, width=160) as renderer:
        for name in robot.card()['cameras']:
            renderer.update_scene(data, camera=name)
            pixels = renderer.render()
            assert pixels.shape == (120, 160, 3) and pixels.std() > 5
    overhead = model.camera('robot/overhead').id
    assert np.allclose(data.cam_xpos[overhead], [0, 0, 1.95])
    assert np.allclose(data.cam_xmat[overhead].reshape(3, 3)[:, 2], [0, 0, 1])


def test_ten_sim_seconds_hold_within_ten_cpu_seconds_on_one_core(record_property):
    robot, model, data = make_arm(cubes=True)
    ctx, affinity = SimpleContext(robot, model, data), os.sched_getaffinity(0)
    try:
        os.sched_setaffinity(0, {min(affinity)})
        started = time.perf_counter()
        cpu_started = time.process_time()
        ctx.step(10)
        cpu_elapsed = time.process_time() - cpu_started
        elapsed = time.perf_counter() - started
    finally:
        os.sched_setaffinity(0, affinity)
    record_property('ten_sim_seconds_wall_seconds', elapsed)
    record_property('ten_sim_seconds_cpu_seconds', cpu_elapsed)
    # Other workers share this machine; scheduler waits do not measure throughput.
    assert data.time == pytest.approx(10) and cpu_elapsed < 10
    assert robot.fallen(model, data) is None


def shelf_world():
    import json
    from pathlib import Path
    from robotlab.assets import menagerie_dir
    from robotlab.world.builder import build_world
    env_path = Path(__file__).parents[1] / 'robotlab/world/defaults/estante-pequeno.json'
    robot = ArmRobot()
    world = build_world(json.loads(env_path.read_text()), robot, menagerie_dir())
    return world, SimpleContext(robot, world.model, world.data)


def shelf_plan(ctx=None, offset=0., *, act=None):
    """Test-only plan: public tools alone pick, raise, transfer, lower and release."""
    if act is None:
        tools = ctx.robot.tools()
        act = lambda tool, **args: tools[tool].fn(ctx, **args)
    frames = [
        {'type': 'gripper_to', 'x': -.2, 'y': -.15, 'z': .98, 'duration': 3},
        {'type': 'gripper_to', 'x': -.2, 'y': -.15, 'z': .795, 'duration': 3},
        {'type': 'gripper', 'opening': 0, 'duration': 1.5},
    ]
    result = act('sequence', keyframes=frames)
    assert result['ok'] and result['state']['held'] == 'caja', result
    frames = [
        {'type': 'gripper_to', 'x': -.2, 'y': -.15, 'z': 1.35, 'duration': 4},
        {'type': 'gripper_to', 'x': .15 + offset, 'y': .2, 'z': 1.35, 'duration': 5},
        {'type': 'gripper_to', 'x': .15 + offset, 'y': .2, 'z': 1.197, 'duration': 3},
        {'type': 'gripper', 'opening': 1, 'duration': 1.5},
    ]
    result = act('sequence', keyframes=frames)
    assert result['ok'] and result['state']['held'] is None, result
    result = act('wait', seconds=2)
    assert result['ok']
    return result


def test_builtin_small_shelf_public_tool_feasibility():
    world, ctx = shelf_world()
    shelf_plan(ctx)
    box = world.object_body('caja')
    shelf = world.object_body('estante')
    top = ctx.data.xpos[shelf, 2] + .2
    bottom = ctx.data.xpos[box, 2] - .035
    velocity = np.empty(6)
    mujoco.mj_objectVelocity(ctx.model, ctx.data, mujoco.mjtObj.mjOBJ_BODY, box, velocity, 0)
    assert abs(bottom - top) < .01, (ctx.object_pose('caja'), bottom, top, velocity)
    assert np.linalg.norm(velocity[3:]) < .02
    assert np.all(np.abs(ctx.data.xpos[box, :2] - ctx.data.xpos[shelf, :2]) < .025)
    assert ctx.held() is None


def test_builtin_small_shelf_off_center_release_falls():
    world, ctx = shelf_world()
    shelf_plan(ctx, offset=.025)
    box = world.object_body('caja')
    assert ctx.data.xpos[box, 2] < 1.0, ctx.object_pose('caja')
    assert ctx.held() is None


@pytest.mark.parametrize('offset', [.012, .02, .03])
def test_one_finger_contact_during_closing_does_not_hold(offset):
    robot, model, data = make_arm(cubes=True)
    ctx = SimpleContext(robot, model, data)
    result = robot.move_gripper_to(ctx, -.2, -.15 + offset, .795, duration=4)
    assert result['ok'], result
    result = robot.gripper(ctx, .9, force=.05, duration=.05)
    assert result['state']['gripper']['contacts'] == {'left': None, 'right': 'box'}, result
    assert result['state']['held'] is None


def test_sequence_stops_on_new_non_finger_contact():
    robot = ArmRobot()
    model, data = build(robot, [
        {'id': 'table', 'shape': 'box', 'size': [.6, .4, .025], 'pos': [0, 0, .725], 'dynamic': False},
        {'id': 'obstacle', 'shape': 'box', 'size': [.035, .025, .03],
         'pos': [-.15, .15, 1.29], 'dynamic': False},
    ], Spawn(-.75, 0, 0))
    ctx = SimpleContext(robot, model, data)
    assert not robot.state(model, data)['contacts']
    result = robot.sequence(ctx, [
        {'type': 'gripper_to', 'x': -.15, 'y': .25, 'z': 1.15, 'duration': 4},
        {'type': 'gripper', 'opening': 0, 'duration': 1},
    ], stop_if={'contact': True})
    assert result['ok'] and result.get('stopped_at') == 0 and result['reason'] == 'contact', result
    assert 0 < data.time < 4 and robot._grip_target == 0
    assert any(c['object'] == 'obstacle' for c in result['state']['contacts'])
    target = robot._target.copy()
    assert robot.wait(ctx, .1)['ok']
    assert np.array_equal(robot._target, target)
