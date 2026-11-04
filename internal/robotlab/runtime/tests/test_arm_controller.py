"""Tool behavior and runtime limits using only the shared robot test context."""
import json

import mujoco
import numpy as np
import pytest

from robotlab.robots.arm import ArmRobot
from robotlab.robots.base import ActionInterrupted, Spawn
from robotlab.robots.testing import SimpleContext, build


@pytest.fixture
def arm():
    robot = ArmRobot()
    objects = [{'id': 'table', 'shape': 'box', 'size': [0.6, 0.4, 0.025],
                'pos': [0, 0, 0.725], 'dynamic': False}]
    model, data = build(robot, objects, Spawn(-0.75, 0, 0))
    return robot, SimpleContext(robot, model, data)


def test_card_and_initial_state(arm):
    robot, ctx = arm
    card = robot.card()
    assert card['id'] == 'arm'
    assert card['name'] == {'en': 'Arm', 'es': 'Brazo'}
    assert {t['name'] for t in card['tools']} == set(robot.tools()) | {'look'}
    assert card['cameras'] == ['robot/wrist', 'robot/overhead']
    assert robot.default_camera == 'robot/wrist'
    for tool in card['tools']:
        assert tool['params']['type'] == 'object'
        assert '/' in tool['description']
    json.dumps(card, allow_nan=False)
    state = robot.state(ctx.model, ctx.data)
    assert np.allclose([state[k] for k in ('x', 'y', 'z')], [-0.15, 0, 1.15], atol=0.001)
    assert state['gripper'] == 'open' and state['holding'] is None
    assert robot.base_xy(ctx.model, ctx.data) == (-0.75, 0)
    assert robot.fallen(ctx.model, ctx.data) is None
    assert robot.safety(ctx.model, ctx.data) is None


def test_every_motion_tool(arm):
    robot, ctx = arm
    result = robot.tools()['move_to'].fn(ctx, x=-0.2, y=0.1, z=1.05, yaw_deg=35)
    assert result['ok'], result
    assert result['error_m'] < 0.01
    assert abs(result['pose']['yaw_deg'] - 35) < 1
    before = ctx.data.site_xpos[robot.site].copy()
    result = robot.tools()['lift'].fn(ctx, dz=0.1)
    assert result['ok'], result
    assert np.allclose(ctx.data.site_xpos[robot.site], before + [0, 0, 0.1], atol=0.004)
    q = np.rad2deg(ctx.data.qpos[robot.qadr])
    q[0] += 3
    q[5] -= 5
    result = robot.tools()['move_joints'].fn(ctx, q_deg=q.tolist(), speed=200)
    assert result['ok'], result
    assert np.allclose(np.rad2deg(ctx.data.qpos[robot.qadr]), q, atol=0.2)
    result = robot.tools()['home'].fn(ctx)
    assert result['ok'], result
    assert np.allclose(ctx.data.site_xpos[robot.site], [-0.15, 0, 1.15], atol=0.004)
    assert robot.close_gripper(ctx)['ok']
    assert ctx.held() is None
    assert robot.state(ctx.model, ctx.data)['gripper'] == 'closed'
    assert robot.open_gripper(ctx)['ok']
    assert robot.state(ctx.model, ctx.data)['gripper'] == 'open'


@pytest.mark.parametrize('tool,args', [
    ('move_to', {'x': 3, 'y': 0, 'z': 1}),
    ('move_to', {'x': 0, 'y': 0, 'z': 0.75}),
    ('move_to', {'x': 0, 'y': float('nan'), 'z': 1}),
    ('move_to', {'x': 0, 'y': 0, 'z': 1, 'speed': -1}),
    ('move_to', {'x': False, 'y': 0, 'z': 1}),
    ('move_to', {'x': 0, 'y': 0, 'z': 1, 'yaw_deg': float('inf')}),
    ('move_joints', {'q_deg': [0] * 5}),
    ('move_joints', {'q_deg': [0] * 5 + ['bad']}),
    ('move_joints', {'q_deg': [0] * 6, 'speed': 0}),
    ('lift', {'dz': float('inf')}),
    ('lift', {'dz': 0.7}),
    ('lift', {'dz': -0.6}),
])
def test_invalid_actions_do_not_step(arm, tool, args):
    robot, ctx = arm
    q = ctx.data.qpos.copy()
    result = robot.tools()[tool].fn(ctx, **args)
    assert not result['ok'], result
    assert ctx.data.time == 0
    assert np.array_equal(ctx.data.qpos, q)


def test_joint_limits_and_unreachable_targets_do_not_move(arm):
    robot, ctx = arm
    result = robot.move_joints(ctx, [10000] * 6)
    assert not result['ok'], result
    assert ctx.data.time == 0
    # Inside the declared box, but beyond the physical arm's reach.
    result = robot.move_to(ctx, 0.6, 0.4, 1.65)
    assert not result['ok'], result
    assert ctx.data.time == 0
    assert np.all(ctx.data.qpos[robot.qadr] <= robot.limits[:, 1])
    assert np.all(ctx.data.qpos[robot.qadr] >= robot.limits[:, 0])


def test_table_penetration_and_contact_stop():
    robot = ArmRobot()
    # A deliberately raised table intersects the start configuration.
    model, data = build(robot, [{'id': 'table', 'shape': 'box',
                                'size': [0.6, 0.4, 0.25], 'pos': [0, 0, 1.1],
                                'dynamic': False}], Spawn(-0.75, 0, 0))
    ctx = SimpleContext(robot, model, data)
    assert robot.fallen(ctx.model, ctx.data) == 'collision'
    assert robot.safety(ctx.model, ctx.data) == 'table_collision'
    with pytest.raises(ActionInterrupted, match='safety: table_collision'):
        robot.home(ctx)
    assert ctx.data.time == 0


def test_shallow_table_contact_is_safety_stop_before_deep_penetration():
    robot = ArmRobot()
    model, data = build(robot, [{'id': 'table', 'shape': 'box',
                                'size': [0.6, 0.4, 0.025], 'pos': [0, 0, 1.125],
                                'dynamic': False}], Spawn(-0.75, 0, 0))
    ctx = SimpleContext(robot, model, data)
    assert robot.fallen(model, data) is None
    assert robot.safety(model, data) == 'table_collision'
    with pytest.raises(ActionInterrupted, match='safety: table_collision'):
        robot.home(ctx)
    assert data.time == 0


def test_measured_tcp_speed_triggers_safety_stop(arm):
    robot, ctx = arm
    ctx.data.qvel[robot.dofs[0]] = 3
    mujoco.mj_forward(ctx.model, ctx.data)
    assert robot.safety(ctx.model, ctx.data) == 'tcp_speed'


def test_interruptions_propagate_and_freeze_target(arm):
    robot, ctx = arm

    def stopped(seconds):
        raise ActionInterrupted('stopped', 'test stop')

    ctx.step = stopped
    with pytest.raises(ActionInterrupted, match='stopped'):
        robot.move_to(ctx, -0.2, 0.1, 1.1)
    assert np.array_equal(robot._target, ctx.data.qpos[robot.qadr])
