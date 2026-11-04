"""All public low-level tools, atomic validation and stop/interruption semantics."""
import json

import numpy as np
import pytest

from robotlab.robots.base import ActionInterrupted
from robotlab.robots.dog import DogRobot
from robotlab.robots.testing import SimpleContext, build


@pytest.fixture
def dog():
    robot = DogRobot()
    model, data = build(robot)
    ctx = SimpleContext(robot, model, data)
    ctx.step(.5)
    return robot, model, data, ctx


def assert_result(result):
    json.dumps(result, allow_nan=False)
    state = result['state']
    assert {'x', 'y', 'z', 'roll_deg', 'pitch_deg', 'yaw_deg', 'vel', 'ang_vel_deg', 'com', 'support', 'margin', 'margin_dir_deg', 'lift', 'legs', 'terrain_ahead', 'edges', 'contacts'} <= state.keys()
    for leg in state['legs'].values():
        assert {'joints_deg', 'target_deg', 'foot_world', 'foot_body', 'contact', 'force_n', 'terrain_z', 'edge_ahead', 'edge_side'} == leg.keys()
    assert len(state['terrain_ahead']) == 18


def test_only_low_level_tools_and_schemas(dog):
    robot, model, data, ctx = dog
    assert set(robot.tools()) == {'move_joints', 'move_feet', 'sequence', 'ik', 'wait'}
    for tool in robot.tools().values():
        assert tool.params['type'] == 'object'
        assert tool.params['additionalProperties'] is False
        assert ' / ' in tool.description


def test_joint_motion_wait_sequence_and_state(dog):
    robot, model, data, ctx = dog
    result = robot.move_joints(ctx, {'FL_thigh': 53}, duration=.3)
    assert result['ok']
    assert data.qpos[robot.lq[1]] == pytest.approx(np.deg2rad(53), abs=.02)
    assert_result(result)
    before = data.time
    result = robot.wait(ctx, .2)
    assert data.time == pytest.approx(before+.2)
    assert_result(result)
    result = robot.sequence(ctx, [dict(type='joints', joints={'FL_thigh': 51.6}, duration=.3), dict(type='feet', feet={}, trunk={'x': .02}, duration=.5)])
    assert result['ok'], result
    assert_result(result)


def test_feet_body_frame_and_ik_no_physics(dog):
    robot, model, data, ctx = dog
    state = robot.state(model, data)
    time, qpos, qvel = data.time, data.qpos.copy(), data.qvel.copy()
    result = robot.ik(ctx, {'FL': state['legs']['FL']['foot_body']}, frame='body')
    assert result['ok'] and all(v['reachable'] for v in result['legs'].values())
    assert data.time == time
    np.testing.assert_array_equal(data.qpos, qpos)
    np.testing.assert_array_equal(data.qvel, qvel)
    assert_result(result)
    result = robot.move_feet(ctx, {}, trunk={'y': -.04}, duration=.7)
    assert result['ok'], result
    p = np.array(result['state']['legs']['FL']['foot_body'])
    p[2] += .025
    result = robot.move_feet(ctx, {'FL': p.tolist()}, frame='body', duration=.5)
    assert result['ok'], result
    assert_result(result)


@pytest.mark.parametrize('tool,args', [
    ('move_joints', dict(joints={'FL_hip': 90}, duration=.2)),
    ('move_joints', dict(joints={'unknown': 0}, duration=.2)),
    ('move_feet', dict(feet={'FL': [2, 0, 0]}, duration=.5)),
    ('move_feet', dict(feet={'FL': [0, float('nan'), 0]}, duration=.5)),
    ('move_feet', dict(feet={}, frame='other', duration=.5)),
    ('move_feet', dict(feet={}, trunk={'z': .9}, duration=.5)),
    ('move_feet', dict(feet={}, trunk={'yaw': 360}, duration=.5)),
    ('move_feet', dict(feet={}, trunk=[], duration=.5)),
    ('move_feet', dict(feet={}, duration=0)),
    ('move_feet', dict(feet={}, duration=.5, settle=3)),
    ('wait', dict(seconds=6)),
    ('sequence', dict(keyframes=[])),
    ('sequence', dict(keyframes=[dict(type='joints', joints={}, duration=2)]*16)),
    ('sequence', dict(keyframes=[dict(type='feet', feet={}, trunk={'x': .01}, duration=.2), dict(type='feet', feet={'FR': [5, 0, 0]}, duration=.5)])),
])
def test_invalid_plan_rejected_atomically(dog, tool, args):
    robot, model, data, ctx = dog
    time, qpos, targets, mode = data.time, data.qpos.copy(), robot.target.copy(), robot.mode
    result = getattr(robot, tool)(ctx, **args)
    assert not result['ok'] and 'details' in result
    assert data.time == time
    np.testing.assert_array_equal(data.qpos, qpos)
    np.testing.assert_array_equal(robot.target, targets)
    assert robot.mode == mode
    assert_result(result)


def test_sequence_stops_mid_keyframe_when_margin_falls(dog):
    robot, model, data, ctx = dog
    time = data.time
    result = robot.sequence(ctx, [dict(type='feet', feet={}, trunk={'y': .09}, duration=2), dict(type='feet', feet={}, trunk={'y': 0}, duration=.5)], stop_if={'margin_below': .1})
    assert result['ok'] and result['stopped_at'] == 0 and result['reason'] == 'margin_below'
    assert .1 < data.time-time < 2
    target = robot.trunk_target.copy()
    ctx.step(.1)
    np.testing.assert_array_equal(robot.trunk_target, target)
    assert_result(result)


def test_interruption_preserves_intermediate_targets(dog):
    robot, model, data, ctx = dog
    original = ctx.step
    until = data.time+.2
    def stop(seconds):
        if data.time >= until:
            raise ActionInterrupted('stopped')
        original(seconds)
    ctx.step = stop
    with pytest.raises(ActionInterrupted):
        robot.move_feet(ctx, {}, trunk={'x': .05}, duration=1)
    assert 0 < robot.trunk_target[0] < .05
    ctx.step = original
    target = robot.trunk_target.copy()
    robot.wait(ctx, .1)
    np.testing.assert_array_equal(robot.trunk_target, target)


def test_sequence_stops_on_new_nonfoot_contact(dog):
    import mujoco
    robot, model, data, ctx = dog
    original = ctx.step
    start = data.time
    calf = next(g for g in robot.robot_geoms if model.geom_group[g] == 3
                and model.body(model.geom_bodyid[g]).name == 'robot/FL_calf' and g not in robot.feet)
    def contact_after_tick(seconds):
        original(seconds)
        if data.time > start+.1:
            contact = mujoco.MjContact()
            contact.geom1, contact.geom2, contact.dist = calf, model.geom('floor').id, -.001
            mujoco.mj_addContact(model, data, contact)
    ctx.step = contact_after_tick
    result = robot.sequence(ctx, [dict(type='joints', joints={'FL_thigh': 52}, duration=1),
                                  dict(type='joints', joints={'FL_thigh': 51.6}, duration=1)], stop_if={'contact': True})
    assert result['ok'] and result['reason'] == 'contact' and result['stopped_at'] == 0
    assert .09 < data.time-start < .2
    assert result['state']['contacts'][0]['link'] == 'FL_calf'
    assert robot.fallen(model, data) is None
