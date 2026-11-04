"""Real-physics stability, trunk interpolation, torque bounds and fall rules."""
import math

import mujoco
import numpy as np
import pytest

from robotlab.robots.dog import DogRobot
from robotlab.robots.testing import SimpleContext, build


def setup(objects=None):
    robot = DogRobot()
    model, data = build(robot, objects)
    return robot, model, data, SimpleContext(robot, model, data)


def test_standing_ten_seconds_without_commands():
    robot, model, data, ctx = setup()
    origin = data.xpos[robot.base].copy()
    ctx.step(10)
    state = robot.state(model, data)
    assert np.linalg.norm(data.xpos[robot.base]-origin) < .02
    assert abs(state['roll_deg']) < 3 and abs(state['pitch_deg']) < 3
    assert len(state['support']) == 4
    assert robot.fallen(model, data) is None


def test_trunk_shift_keeps_unlisted_feet_planted():
    robot, model, data, ctx = setup()
    ctx.step(.5)
    feet = data.geom_xpos[robot.feet].copy()
    origin = data.xpos[robot.base].copy()
    result = robot.move_feet(ctx, {}, trunk={'x': float(origin[0]+.04), 'y': .02}, duration=1)
    assert result['ok'], result
    np.testing.assert_allclose(data.xpos[robot.base, :2], origin[:2]+[.04, .02], atol=.01)
    assert np.max(np.linalg.norm(data.geom_xpos[robot.feet]-feet, axis=1)) < .01
    assert len(result['state']['support']) == 4


def test_pd_torque_limits():
    robot, model, data, ctx = setup()
    data.qpos[robot.lq] += .5
    robot.control(model, data)
    assert np.all(data.ctrl[robot.la] >= model.actuator_ctrlrange[robot.la, 0])
    assert np.all(data.ctrl[robot.la] <= model.actuator_ctrlrange[robot.la, 1])


def test_fall_tilt_and_persistent_terrain_clearance():
    robot, model, data, ctx = setup()
    data.qpos[robot.rq+2] = .115
    mujoco.mj_forward(model, data)
    assert robot.fallen(model, data) is None
    data.time = .299
    assert robot.fallen(model, data) is None
    data.time = .301
    assert 'clearance' in robot.fallen(model, data)
    robot.reset(model, data)
    angle = math.radians(51)
    data.qpos[robot.rq+3:robot.rq+7] = [math.cos(angle/2), math.sin(angle/2), 0, 0]
    mujoco.mj_forward(model, data)
    assert 'tilt' in robot.fallen(model, data)


def test_clearance_timer_resets_and_uses_terrain_height():
    box = dict(id='platform', shape='box', size=[.5, .5, .15], pos=[0, 0, .15], dynamic=False)
    robot, model, data, ctx = setup([box])
    for height, when in [(.415, 0), (.415, .2), (.6, .25), (.415, .3), (.415, .55)]:
        data.qpos[robot.rq+2] = height
        data.time = when
        mujoco.mj_forward(model, data)
        assert robot.fallen(model, data) is None
    data.time = .61
    assert 'clearance' in robot.fallen(model, data)


@pytest.mark.parametrize('link,fallen', [('base', True), ('FL_hip', True), ('FL_thigh', False), ('FL_calf', False)])
def test_only_trunk_and_hip_fixed_contacts_cause_fall(link, fallen):
    robot, model, data, ctx = setup()
    body = model.body('robot/'+link).id
    geoms = [g for g in robot.robot_geoms if model.geom_bodyid[g] == body and model.geom_group[g] == 3 and g not in robot.feet]
    # Inject a genuine MuJoCo contact at a selected collision link, leaving the
    # trunk height and attitude unchanged to isolate this specific fall rule.
    contact = mujoco.MjContact()
    contact.geom1, contact.geom2 = geoms[0], model.geom('floor').id
    contact.dist = -.001
    mujoco.mj_addContact(model, data, contact)
    assert bool(robot.fallen(model, data)) is fallen
