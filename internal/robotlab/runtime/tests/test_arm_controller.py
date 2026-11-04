"""Public low-level contract using real simulation and deterministic interruption."""
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
    model, data = build(robot, [{'id': 'table', 'shape': 'box', 'size': [.6, .4, .025],
                                'pos': [0, 0, .725], 'dynamic': False}], Spawn(-.75, 0, 0))
    return robot, SimpleContext(robot, model, data)


def test_card_and_initial_state(arm):
    robot, ctx = arm
    card = robot.card()
    assert set(robot.tools()) == {'move_joints', 'move_gripper_to', 'gripper', 'sequence', 'ik', 'wait'}
    assert {t['name'] for t in card['tools']} == set(robot.tools()) | {'look'}
    assert card['name'] == {'en': 'Arm', 'es': 'Brazo'}
    assert card['cameras'] == ['robot/wrist', 'robot/overhead']
    for tool in card['tools']:
        assert tool['params']['type'] == 'object' and ' / ' in tool['description']
    assert set(card['joints']) == set(card['reference_pose']['joints_deg'])
    assert all(v['limits_deg'][0] < v['limits_deg'][1] for v in card['joints'].values())
    assert card['link_lengths_m']['upper_arm'] > .6
    json.dumps(card, allow_nan=False)
    state = robot.state(ctx.model, ctx.data)
    assert set(state) == {'joints_deg', 'gripper', 'held', 'objects', 'contacts'}
    assert np.allclose(state['gripper']['pos'], [-.15, 0, 1.15], atol=.001)
    assert abs(state['gripper']['pitch_deg']) <= .1
    assert state['gripper']['opening'] > .95 and state['held'] is None
    assert state['gripper']['contacts'] == {'left': None, 'right': None}
    assert state['objects'][0]['size'] == [1.2, .8, .05]
    assert robot.base_xy(ctx.model, ctx.data) == (-.75, 0)
    assert robot.fallen(ctx.model, ctx.data) is None
    assert robot.safety(ctx.model, ctx.data) is None


def test_every_tool_and_partial_joint_targets(arm):
    robot, ctx = arm
    result = robot.tools()['move_gripper_to'].fn(ctx, x=-.2, y=.1, z=1.05, yaw=35, duration=3)
    assert result['ok'], result
    assert np.allclose(result['state']['gripper']['pos'], [-.2, .1, 1.05], atol=.004)
    assert result['state']['gripper']['yaw_deg'] == pytest.approx(35, abs=.5)
    before = robot._target.copy()
    result = robot.move_joints(ctx, {'wrist_3': np.rad2deg(before[-1]) - 5}, duration=1)
    assert result['ok'], result
    assert np.allclose(robot._target[:-1], before[:-1])
    assert np.rad2deg(ctx.data.qpos[robot.qadr[-1]]) == pytest.approx(np.rad2deg(before[-1]) - 5, abs=.3)
    assert robot.gripper(ctx, opening=0, force=.4, duration=1)['ok']
    assert robot.state(ctx.model, ctx.data)['gripper']['opening'] < .1
    assert robot.gripper(ctx, opening=1)['ok']
    assert robot.state(ctx.model, ctx.data)['gripper']['opening'] > .95
    assert robot.sequence(ctx, [{'type': 'joints', 'joints': {'wrist_3': np.rad2deg(before[-1])}, 'duration': 1},
                                {'type': 'gripper_to', 'x': -.15, 'y': 0, 'z': 1.15, 'path': 'joint', 'duration': 3},
                                {'type': 'gripper', 'opening': .8, 'duration': .5}])['ok']
    before_time = ctx.data.time
    assert robot.wait(ctx, seconds=.1)['ok']
    assert ctx.data.time == pytest.approx(before_time + .1)


@pytest.mark.parametrize('tool,args', [
    ('move_gripper_to', dict(x=3, y=0, z=1, duration=1)),
    ('move_gripper_to', dict(x=0, y=0, z=.75, duration=1)),
    ('move_gripper_to', dict(x=0, y=float('nan'), z=1, duration=1)),
    ('move_gripper_to', dict(x=False, y=0, z=1, duration=1)),
    ('move_gripper_to', dict(x=0, y=0, z=1, yaw=float('inf'), duration=1)),
    ('move_gripper_to', dict(x=0, y=0, z=1, path='curve', duration=1)),
    ('move_joints', dict(joints={'wrist_3': 10000}, duration=1)),
    ('move_joints', dict(joints={'wrist_3': 'bad'}, duration=1)),
    ('move_joints', dict(joints={'bad_joint': 0}, duration=1)),
    ('move_joints', dict(joints={}, duration=1)),
    ('move_joints', dict(joints=[0] * 6, duration=1)),
    ('move_joints', dict(joints={'elbow': 0}, duration=0)),
    ('move_joints', dict(joints={'elbow': 0}, duration=31)),
    ('move_joints', dict(joints={'elbow': 0}, duration=1, settle=3)),
    ('gripper', dict(opening=1.1)),
    ('gripper', dict(opening=0, force=-.1)),
    ('gripper', dict(opening=0, duration=float('nan'))),
    ('wait', dict(seconds=.01)), ('wait', dict(seconds=6)), ('wait', dict(seconds=True)),
    ('sequence', dict(keyframes=[])), ('sequence', dict(keyframes=[{}] * 17)),
    ('sequence', dict(keyframes=[{'type': 'home', 'duration': 1}])),
    ('sequence', dict(keyframes=[None])),
    ('sequence', dict(keyframes=[{'type': 'gripper', 'opening': 1, 'duration': 1, 'extra': 1}])),
    ('sequence', dict(keyframes=[{'type': 'gripper', 'opening': 1, 'duration': 1}], stop_if={'margin_below': 0})),
    ('sequence', dict(keyframes=[{'type': 'gripper', 'opening': 1, 'duration': 1}], stop_if={'contact': 1})),
    ('sequence', dict(keyframes=[{'type': 'gripper', 'opening': 1, 'duration': 16}] * 2)),
])
def test_invalid_actions_do_not_step_or_change_targets(arm, tool, args):
    robot, ctx = arm
    q, target = ctx.data.qpos.copy(), robot._target.copy()
    result = robot.tools()[tool].fn(ctx, **args)
    assert not result['ok'] and 'state' in result and 'details' in result, result
    assert ctx.data.time == 0 and np.array_equal(ctx.data.qpos, q)
    assert np.array_equal(robot._target, target) and robot._grip_target == 0
    json.dumps(result, allow_nan=False)


def test_whole_sequence_preflight_and_unreachable_target(arm):
    robot, ctx = arm
    frames = [{'type': 'gripper', 'opening': 0, 'duration': 1},
              {'type': 'gripper_to', 'x': .6, 'y': .4, 'z': 1.65, 'duration': 2}]
    result = robot.sequence(ctx, frames)
    assert not result['ok'] and result['details'][0]['keyframe'] == 1
    assert ctx.data.time == 0 and robot._grip_target == 0
    result = robot.move_gripper_to(ctx, .6, .4, 1.65, duration=3, path='joint')
    assert not result['ok'] and ctx.data.time == 0


def test_ik_is_pure_and_reports_margins_and_orientation(arm):
    from robotlab.robots.arm_kinematics import grasp_angles
    robot, ctx = arm
    before, target = ctx.data.qpos.copy(), robot._target.copy()
    result = robot.tools()['ik'].fn(ctx, x=-.25, y=.1, z=1.1, yaw=25, pitch=10, roll=-8)
    assert result['ok'] and result['reachable'], result
    assert all(v >= 0 for v in result['limit_margin_deg'].values())
    pos, rot = robot._pose(np.deg2rad(list(result['joints_deg'].values())))
    assert np.allclose(pos, [-.25, .1, 1.1], atol=.002)
    assert np.allclose(grasp_angles(rot), [25, 10, -8], atol=.2)
    bad = robot.ik(ctx, .6, .4, 1.65)
    assert not bad['reachable'] and not bad['ok']
    assert ctx.data.time == 0 and np.array_equal(ctx.data.qpos, before)
    assert np.array_equal(robot._target, target)


def test_duration_settle_and_wait_hold(arm):
    robot, ctx = arm
    target = robot._target.copy()
    assert robot.move_joints(ctx, {'wrist_3': np.rad2deg(target[-1]) + 3}, duration=1, settle=.4)['ok']
    assert ctx.data.time == pytest.approx(1.4)
    before = robot._target.copy()
    assert robot.wait(ctx, 5)['ok']
    assert ctx.data.time == pytest.approx(6.4) and np.array_equal(robot._target, before)
    assert np.max(np.abs(ctx.data.qpos[robot.qadr] - before)) < .003


def test_interruptions_propagate_and_freeze(arm):
    robot, ctx = arm
    def stopped(seconds):
        raise ActionInterrupted('stopped', 'test stop')
    ctx.step = stopped
    with pytest.raises(ActionInterrupted, match='stopped'):
        robot.move_gripper_to(ctx, -.2, .1, 1.1, duration=3)
    assert np.array_equal(robot._target, ctx.data.qpos[robot.qadr])


def test_table_collision_and_speed_safety(arm):
    robot, ctx = arm
    ctx.data.qvel[robot.dofs[0]] = 3
    mujoco.mj_forward(ctx.model, ctx.data)
    assert robot.safety(ctx.model, ctx.data) == 'tcp_speed'
    robot = ArmRobot()
    model, data = build(robot, [{'id': 'table', 'shape': 'box', 'size': [.6, .4, .25],
                                'pos': [0, 0, 1.1], 'dynamic': False}], Spawn(-.75, 0, 0))
    ctx = SimpleContext(robot, model, data)
    assert robot.fallen(model, data) == 'collision'
    with pytest.raises(ActionInterrupted, match='safety: table_collision'):
        robot.wait(ctx, .1)
    assert data.time == 0


def test_card_is_available_without_compiling_a_world():
    card = ArmRobot().card()
    assert card['reference_pose']['grasp_pos'] == [-.15, 0., 1.15]
    assert len(card['joints']) == 6
    assert {t['name'] for t in card['tools']} == {'move_joints', 'move_gripper_to', 'gripper', 'sequence', 'ik', 'wait', 'look'}
    json.dumps(card, allow_nan=False)


@pytest.mark.parametrize('opening', [.25, .5, .75])
def test_partial_opening_means_measured_aperture_fraction(arm, opening):
    robot, ctx = arm
    result = robot.gripper(ctx, opening, duration=1.5)
    assert result['ok'], result
    assert result['state']['gripper']['opening_m'] == pytest.approx(.085 * opening, abs=.004)


def test_pitch_roll_and_omitted_yaw_are_executed(arm):
    robot, ctx = arm
    result = robot.move_gripper_to(ctx, -.2, .1, 1.1, yaw=20, pitch=10, roll=-8, duration=3)
    assert result['ok'], result
    assert [result['state']['gripper'][k + '_deg'] for k in ('yaw', 'pitch', 'roll')] == pytest.approx([20, 10, -8], abs=.5)
    result = robot.move_gripper_to(ctx, -.2, .1, 1.15, duration=2)
    assert result['ok'], result
    assert [result['state']['gripper'][k + '_deg'] for k in ('yaw', 'pitch', 'roll')] == pytest.approx([20, 0, 0], abs=.5)


def test_previous_snapshot_gripper_command_is_accepted(arm):
    robot, ctx = arm
    robot._gripper = 'open'
    robot.control(ctx.model, ctx.data)
    assert robot._gripper == {'opening': 1., 'force': 1.}
    assert robot.wait(ctx, .1)['ok']


@pytest.mark.parametrize('args', [
    {'keyframes': [{'type': []}]},
    {'keyframes': [{'type': {}}]},
    {'keyframes': [{'type': 'gripper_to', 'x': 10**1000, 'y': 0, 'z': 1, 'duration': 1}]},
])
def test_malformed_json_plans_return_errors_without_physics(arm, args):
    robot, ctx = arm
    result = robot.sequence(ctx, **args)
    assert not result['ok'] and result['details'][0]['keyframe'] == 0
    assert ctx.data.time == 0
