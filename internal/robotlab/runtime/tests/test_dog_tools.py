"""Tool limits, observations, interruption, and low-level controller contracts."""
import json
import math

import mujoco
import numpy as np
import pytest

from robotlab.robots.base import ActionInterrupted, Spawn
from robotlab.robots.dog import DogRobot, STOW, leg_ik
from robotlab.robots.testing import SimpleContext, build


@pytest.fixture
def dog():
    robot = DogRobot()
    model, data = build(robot)
    return robot, model, data, SimpleContext(robot, model, data)


def test_remaining_locomotion_tools(dog):
    robot, model, data, ctx = dog
    assert robot.stand(ctx)['ok']
    origin = data.xpos[robot.base, :2].copy()
    result = robot.walk(ctx, -.5, speed=.6)
    assert result['ok'], result
    assert data.xpos[robot.base, 0] < origin[0]-.3
    origin = data.xpos[robot.base, :2].copy()
    assert robot.side_step(ctx, .4)['ok']
    assert data.xpos[robot.base, 1] > origin[1]+.25
    assert robot.side_step(ctx, -.4)['ok']
    assert abs(data.xpos[robot.base, 1]-origin[1]) < .15
    assert robot.stop(ctx)['ok']
    assert np.linalg.norm(data.qvel[robot.rv:robot.rv+2]) < .08
    assert robot.turn(ctx, -90)['ok']


def test_crouch_runtime_clamps(dog):
    robot, model, data, ctx = dog
    assert robot.crouch(ctx, -10)['height_target'] == .18
    assert .15 < data.xpos[robot.base, 2] < .21
    assert robot.crouch(ctx, 10)['height_target'] == .32
    assert .28 < data.xpos[robot.base, 2] < .35
    with pytest.raises(ValueError, match='finite'):
        robot.crouch(ctx, float('nan'))
    with pytest.raises(ValueError, match='finite'):
        robot.walk(ctx, 1, speed=float('inf'))


def test_arm_body_frame_and_invalid_targets(dog):
    robot, model, data, ctx = dog
    assert robot.stand(ctx)['ok']
    assert robot.arm_move_to(ctx, .4, 0, .12, frame='body')['ok']
    # The gripper's longitudinal axis points down by default.
    assert data.site_xmat[robot.site].reshape(3, 3)[2, 0] < -.96
    assert robot.arm_stow(ctx)['ok']
    assert np.max(np.abs(data.qpos[robot.aq]-STOW)) < .05
    assert not robot.arm_move_to(ctx, 4, 0, .2)['ok']
    assert not robot.arm_move_to(ctx, .3, 0, .2, frame='other')['ok']
    assert robot.close_gripper(ctx)['held'] is None
    assert robot.open_gripper(ctx)['released'] is None


def test_spawn_cameras_card_and_torque_limits():
    robot = DogRobot()
    model, data = build(robot, spawn=Spawn(1, -2, math.pi/2))
    assert robot.state(model, data)['yaw_deg'] == pytest.approx(90)
    assert robot.base_xy(model, data) == (1, -2)
    ctx = SimpleContext(robot, model, data)
    ctx.step(.2)
    assert robot.mass > 19  # Original dog plus the mounted arm, no mass scaling.
    assert model.body('robot/arm/link02').mass[0] == pytest.approx(1.19132)
    assert all(model.body(i).name.startswith('robot/') for i in range(1,model.nbody))
    head = model.camera('robot/head').id
    camera_z = data.cam_xmat[head].reshape(3,3)[:,2]
    forward = data.xmat[robot.base].reshape(3,3)[:,0]
    assert np.dot(-camera_z, forward) > .94
    assert -camera_z[2] < -.15
    assert model.camera('robot/arm').fovy[0] == 65
    card = robot.card()
    assert set(card['name']) == {'en', 'es'}
    assert {t['name'] for t in card['tools']} == set(robot.tools()) | {'look'}
    json.dumps(card)
    for tool in robot.tools().values():
        assert tool.params['type'] == 'object'
        assert tool.params['additionalProperties'] is False
        assert '/' in tool.description
        for value in tool.params['properties'].values():
            if value['type'] == 'number':
                assert value['minimum'] < value['maximum']
    data.qpos[robot.lq] += .2
    robot.control(model,data)
    assert np.all(data.ctrl[robot.la] >= model.actuator_ctrlrange[robot.la,0])
    assert np.all(data.ctrl[robot.la] <= model.actuator_ctrlrange[robot.la,1])


def test_fall_tilt_and_persistent_low_height(dog):
    robot, model, data, ctx = dog
    data.qpos[robot.rq+2] = .1
    mujoco.mj_forward(model, data)
    assert robot.fallen(model,data) is None
    data.time = .299
    assert robot.fallen(model,data) is None
    data.time = .301
    assert robot.fallen(model,data)
    robot.reset(model,data)
    mujoco.mj_forward(model,data)
    assert robot.fallen(model,data) is None
    angle = math.radians(51)
    data.qpos[robot.rq+3:robot.rq+7] = [math.cos(angle/2),math.sin(angle/2),0,0]
    mujoco.mj_forward(model,data)
    assert 'tilt' in robot.fallen(model,data)


def test_motion_interruption_clears_velocity(dog):
    robot, model, data, ctx = dog
    original = ctx.step
    def interrupt(seconds):
        if np.linalg.norm(robot.velocity) > .01:
            raise ActionInterrupted('stopped')
        original(seconds)
    ctx.step = interrupt
    with pytest.raises(ActionInterrupted, match='stopped'):
        robot.walk(ctx, 1)
    assert np.linalg.norm(robot.velocity) == 0
    assert robot.turn_rate == 0


def test_leg_ik_known_stance():
    for side in [-1,1]:
        q = leg_ik(0, side*.0955, -.278, side)
        assert abs(q[0]) < 1e-8
        assert q[1] == pytest.approx(-q[2]/2)
        assert .426*math.cos(q[1]) == pytest.approx(.278)


def test_runtime_gait_limits_and_joint_targets(dog):
    robot, model, data, ctx = dog
    assert robot.walk(ctx, 0, speed=10, step_height=10, gait_period=10)['ok']
    assert robot.step_height == .12
    assert robot.period == .8
    robot.velocity[:] = [.6, .6]
    robot.phase = .9
    data.time += .02
    robot.control(model, data)
    assert np.all(robot.target >= robot.lrange[:, 0])
    assert np.all(robot.target <= robot.lrange[:, 1])
    robot.velocity[:] = 0
    assert not robot.arm_move_to(ctx, .3, 0, -.5, frame='body')['ok']
    assert np.all(robot.arm_target >= robot.arange[:, 0])
    assert np.all(robot.arm_target <= robot.arange[:, 1])


def test_turn_and_arm_interruption(dog):
    robot, model, data, ctx = dog
    original = ctx.step
    def interrupt_turn(seconds):
        if abs(robot.turn_rate) > .01:
            raise ActionInterrupted('goal')
        original(seconds)
    ctx.step = interrupt_turn
    with pytest.raises(ActionInterrupted, match='goal'):
        robot.turn(ctx, 90)
    assert robot.turn_rate == 0
    assert np.linalg.norm(robot.velocity) == 0
    def interrupt_arm(seconds):
        raise ActionInterrupted('stopped')
    ctx.step = interrupt_arm
    with pytest.raises(ActionInterrupted, match='stopped'):
        robot.arm_move_to(ctx, .4, 0, .3)
    assert not robot.state(model, data)['arm_stowed']


def test_stowed_mass_and_center_of_mass(dog):
    robot, model, data, ctx = dog
    arm = model.body('robot/arm/link00').id
    assert model.body_subtreemass[arm] == pytest.approx(4.694932)
    assert robot.mass == pytest.approx(19.90134)
    local_com = data.xmat[robot.base].reshape(3, 3).T @ (data.subtree_com[robot.base]-data.xpos[robot.base])
    print(f'combined mass {robot.mass:.4f} kg, stowed local COM {local_com}')
    assert -.05 < local_com[0] < .01
    assert abs(local_com[1]) < .01
    assert abs(local_com[2]) < .1
    assert robot.arm_move_to(ctx, .4, 0, .4)['ok']
    extended_com = data.xmat[robot.base].reshape(3, 3).T @ (data.subtree_com[robot.base]-data.xpos[robot.base])
    assert extended_com[0] > local_com[0]+.025
    assert robot.fallen(model, data) is None


def test_grasp_tolerance_rejects_fixed_far_and_outside_fingers(dog):
    robot, model, data, ctx = dog
    # Isolate the grasp selection from finger/ball contact dynamics, already
    # covered by the real-physics fetch. These objects must never be attached.
    site = data.site_xpos[robot.site].copy()
    rotation = data.site_xmat[robot.site].reshape(3, 3)
    entries = [dict(id='fixed', dynamic=False), dict(id='far', dynamic=True),
               dict(id='outside_fingers', dynamic=True)]
    positions = {'fixed':site, 'far':site+rotation@np.array([.041,0,0]),
                 'outside_fingers':site+rotation@np.array([0,.036,0])}
    ctx.step = lambda seconds: None
    ctx.objects = lambda: entries
    ctx.object_pose = lambda oid: (positions[oid], np.array([1.,0,0,0]))
    attached = []
    ctx.hold = lambda oid, name: attached.append((oid,name))
    assert robot.close_gripper(ctx)['held'] is None
    assert attached == []
    entries.append(dict(id='boundary',dynamic=True))
    positions['boundary'] = site+rotation@np.array([.0399,0,0])
    robot.close_gripper(ctx)
    assert attached == [('boundary','robot/arm/grasp')]


def test_intermediate_recovery_resets_low_height_timer(dog):
    robot, model, data, ctx = dog
    for height, when in [(.1,0), (.1,.2), (.3,.25), (.1,.3), (.1,.55)]:
        data.qpos[robot.rq+2] = height
        data.time = when
        mujoco.mj_forward(model,data)
        assert robot.fallen(model,data) is None
    data.time = .61
    assert robot.fallen(model,data)


def test_unreached_stow_returns_json_serializable_state(dog, monkeypatch):
    robot, model, data, ctx = dog
    data.qpos[robot.aq[1]] = -.5
    monkeypatch.setattr(ctx, 'step', lambda seconds: None)
    monkeypatch.setattr(ctx, 'run_until', lambda done, timeout: done())
    result = robot.arm_stow(ctx)
    assert not result['ok']
    assert result['state']['arm_stowed'] is False
    json.dumps(result, allow_nan=False)
