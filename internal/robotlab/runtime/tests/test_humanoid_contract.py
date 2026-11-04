"""Controller limits, body metadata, interruption and deterministic-reset checks."""
import json
import math

import mujoco
import numpy as np
import pytest

from robotlab.robots.base import ActionInterrupted, Spawn
from robotlab.robots.humanoid import HumanoidRobot
from robotlab.robots.testing import SimpleContext, build


@pytest.fixture
def body():
    robot = HumanoidRobot()
    model, data = build(robot)
    return robot, model, data, SimpleContext(robot, model, data)


def test_card_is_available_before_build():
    robot = HumanoidRobot()
    card = robot.card()
    assert card['id'] == 'humanoid'
    assert card['name'] == {'en': 'Humanoid', 'es': 'Humanoide'}
    assert card['cameras'] == ['robot/head']
    assert {tool['name'] for tool in card['tools']} == set(robot.tools()) | {'look'}
    assert set(robot.tools()) == {'stand', 'walk', 'turn', 'step', 'side_step', 'walk_to', 'squat', 'move_arm', 'hand'}
    for tool in robot.tools().values():
        assert tool.params['type'] == 'object'
        assert tool.params['additionalProperties'] is False
        assert ' / ' in tool.description
        for prop in tool.params['properties'].values():
            if prop['type'] == 'number':
                assert prop['minimum'] < prop['maximum']
    json.dumps(card, allow_nan=False)
    assert any('0.35' in note and '0.3' in note for note in card['notes'])


@pytest.mark.parametrize('value', [float('nan'), float('inf'), float('-inf'), True, 'fast', None])
def test_invalid_numeric_inputs_do_not_step(body, value):
    robot, model, data, ctx = body
    calls = [lambda: robot.stand(ctx, value), lambda: robot.walk(ctx, value),
             lambda: robot.walk(ctx, .1, step_length=value), lambda: robot.walk(ctx, .1, step_time=value),
             lambda: robot.walk(ctx, .1, step_height=value), lambda: robot.walk(ctx, .1, com_shift=value),
             lambda: robot.walk(ctx, .1, torso_lean_deg=value), lambda: robot.turn(ctx, value),
             lambda: robot.step(ctx, 'left', value, 0), lambda: robot.squat(ctx, value),
             lambda: robot.side_step(ctx, value), lambda: robot.side_step(ctx, .1, step_width=value),
             lambda: robot.side_step(ctx, .1, step_time=value),
             lambda: robot.walk_to(ctx, 0, value), lambda: robot.walk_to(ctx, 1, 1, step_time=value),
             lambda: robot.move_arm(ctx, 'left', joints_deg=[value] * 7)]
    for call in calls:
        with pytest.raises(ValueError):
            call()
        assert data.time == 0


def test_invalid_enums_and_arm_shapes_do_not_step(body):
    robot, model, data, ctx = body
    calls = [lambda: robot.step(ctx, 'both', .1, 0), lambda: robot.hand(ctx, 'bad', 'open'),
             lambda: robot.hand(ctx, 'left', 'bad'), lambda: robot.move_arm(ctx, 'left', pose='bad'),
             lambda: robot.move_arm(ctx, 'left'), lambda: robot.move_arm(ctx, 'left', pose='down', joints_deg=[0] * 7),
             lambda: robot.move_arm(ctx, 'left', joints_deg=[0]), lambda: robot.walk_to(ctx, 1, 1, invalid=1)]
    for call in calls:
        with pytest.raises(ValueError):
            call()
        assert data.time == 0


def test_rejected_workspace_and_crossed_feet(body):
    robot, model, data, ctx = body
    assert not robot.step(ctx, 'left', .01, -.15)['ok']
    assert not robot.walk_to(ctx, 10, 10)['ok']
    assert data.time == 0
    assert robot.walk_to(ctx, 0, 0)['steps_done'] == 0


def test_runtime_clamps_and_target_speed(body):
    robot, model, data, ctx = body
    assert robot._number(100, 0, .25) == .25
    assert robot._number(-100, 0, .25) == 0
    # Replace physics with a target observer to exercise limits without unsafe poses.
    seen = []
    def observe(seconds):
        seen.append((data.time, robot.target[robot.qa].copy()))
        data.time += seconds
    ctx.step = observe
    robot.move_arm(ctx, 'both', joints_deg=[1000] * 7)
    for (t0, q0), (t1, q1) in zip(seen, seen[1:]):
        assert np.max(np.abs(q1 - q0)) / (t1 - t0) <= 1.21
    assert np.all(robot.target[robot.qa] >= model.jnt_range[robot.joints, 0])
    assert np.all(robot.target[robot.qa] <= model.jnt_range[robot.joints, 1])
    robot.walk(ctx, 1000, step_length=1000, step_time=-1000, step_height=1000, com_shift=1000, torso_lean_deg=1000)
    assert np.isfinite(robot.target).all()
    assert robot.depth == 0
    robot.squat(ctx, 1000)
    assert robot.depth == .25


@pytest.mark.parametrize('reason', ['goal', 'stopped', 'timeout', 'safety'])
def test_interruptions_propagate_without_further_steps(body, reason):
    robot, model, data, ctx = body
    count = 0
    def stop(seconds):
        nonlocal count
        count += 1
        raise ActionInterrupted(reason)
    ctx.step = stop
    for name, args in [('stand', {}), ('walk', {'distance': .6}), ('turn', {'angle_deg': 45}),
                       ('step', {'foot': 'left', 'dx': .1, 'dy': 0}), ('walk_to', {'x': 1, 'y': 0}),
                       ('side_step', {'distance': .5}),
                       ('squat', {'depth': .1}), ('move_arm', {'side': 'left', 'pose': 'forward'}),
                       ('hand', {'side': 'left', 'state': 'close'})]:
        count = 0
        with pytest.raises(ActionInterrupted) as caught:
            robot.tools()[name].fn(ctx, **args)
        assert caught.value.reason == reason
        assert count == 1


def test_fall_thresholds(body):
    robot, model, data, ctx = body
    assert robot.fallen(model, data) is None
    data.qpos[robot.root + 2] = .44
    mujoco.mj_forward(model, data)
    assert 'pelvis' in robot.fallen(model, data)
    robot.reset(model, data)
    a = math.radians(46) / 2
    data.qpos[robot.root + 3:robot.root + 7] = [math.cos(a), math.sin(a), 0, 0]
    mujoco.mj_forward(model, data)
    assert 'tilt' in robot.fallen(model, data)
    data.qpos[robot.root] = np.nan
    assert 'non-finite' in robot.fallen(model, data)


def test_repeatable_after_reset(body):
    robot, model, data, ctx = body
    result = robot.walk(ctx, .24)
    assert result['ok']
    qpos, qvel, stamp = data.qpos.copy(), data.qvel.copy(), data.time
    mujoco.mj_resetData(model, data)
    robot.reset(model, data)
    mujoco.mj_forward(model, data)
    second = robot.walk(ctx, .24)
    assert second == result
    np.testing.assert_array_equal(data.qpos, qpos)
    np.testing.assert_array_equal(data.qvel, qvel)
    assert data.time == stamp


def test_reset_preserves_object_pose_and_uses_separate_joint_addresses():
    robot = HumanoidRobot()
    model, data = build(robot, [{'id': 'ball', 'shape': 'sphere', 'size': [.02], 'pos': [2, 0, .02]}])
    assert robot.root >= 7
    body = model.body('obj/ball').id
    q = model.jnt_qposadr[model.body_jntadr[body]]
    data.qpos[q:q + 3] = [3, 2, .03]
    robot.reset(model, data)
    np.testing.assert_array_equal(data.qpos[q:q + 3], [3, 2, .03])
    mujoco.mj_forward(model, data)
    assert robot.stand(SimpleContext(robot, model, data))['ok']


def test_camera_is_forward_down_and_renders(body):
    robot, model, data, ctx = body
    camera = model.camera('robot/head').id
    assert 60 <= model.cam_fovy[camera] <= 70
    forward = -data.cam_xmat[camera].reshape(3, 3)[:, 2]
    assert forward[0] > .8 and -.5 < forward[2] < -.1
    for side in ('left', 'right'):
        assert model.site('robot/' + side + '_grasp').id >= 0
    with mujoco.Renderer(model, height=120, width=160) as renderer:
        renderer.update_scene(data, camera='robot/head')
        image = renderer.render()
    assert image.shape == (120, 160, 3)
    assert image.std() > 2


def test_no_keyframe_places_soles_on_floor():
    class NoKeyRobot(HumanoidRobot):
        def attach(self, spec, spawn, menagerie):
            super().attach(spec, spawn, menagerie)
            for key in list(spec.keys):
                spec.delete(key)
    robot = NoKeyRobot()
    model, data = build(robot, spawn=Spawn(1, 2, .5))
    assert model.nkey == 0
    assert robot.base_xy(model, data) == pytest.approx((1, 2))
    foot_bodies = set(model.site_bodyid[robot.sites])
    bottom = [data.geom_xpos[g, 2] - model.geom_size[g, 0] for g in range(model.ngeom)
              if model.geom_bodyid[g] in foot_bodies and model.geom_type[g] == mujoco.mjtGeom.mjGEOM_SPHERE
              and model.geom_contype[g]]
    assert min(bottom) == pytest.approx(0, abs=1e-6)
    assert robot.stand(SimpleContext(robot, model, data), 2)['ok']


def test_long_actions_return_partial_results_with_a_time_limit(body):
    robot, model, data, ctx = body
    ctx.step = lambda seconds: setattr(data, 'time', data.time + seconds)
    result = robot.walk(ctx, 3, step_length=.02, step_time=2)
    assert not result['ok']
    assert 'time budget' in result['error']
    assert data.time < 55
    data.time = 0
    result = robot.side_step(ctx, 1, step_width=.02, step_time=2)
    assert not result['ok']
    assert 'time budget' in result['error']
    assert result['steps_done'] % 2 == 0
    assert data.time < 55
    data.time = 0
    result = robot.turn(ctx, 180, step_time=2)
    assert not result['ok']
    assert 'time budget' in result['error']
    assert data.time < 55
