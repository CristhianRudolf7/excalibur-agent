"""Go2-only body, home pose, cameras and bilingual low-level card."""
import json
import math

import numpy as np
import pytest

from robotlab.robots.base import Spawn
from robotlab.robots.dog import DogRobot, leg_ik
from robotlab.robots.testing import build


def test_body_home_cameras_and_card():
    robot = DogRobot()
    model, data = build(robot, spawn=Spawn(1, -2, math.pi/2))
    np.testing.assert_allclose(data.qpos[robot.lq], np.tile([0, .9, -1.8], 4))
    assert data.qpos[robot.rq+2] == .27
    assert robot.mass == pytest.approx(15.206408)
    assert not any('arm/' in model.body(i).name for i in range(model.nbody))
    assert robot.base_xy(model, data) == (1, -2)
    assert robot.state(model, data)['yaw_deg'] == 90
    front = -data.cam_xmat[model.camera('robot/head').id].reshape(3, 3)[:, 2]
    rear = -data.cam_xmat[model.camera('robot/rear').id].reshape(3, 3)[:, 2]
    forward = data.xmat[robot.base].reshape(3, 3)[:, 0]
    assert np.dot(front, forward) > .94 and front[2] < -.15
    assert np.dot(rear, forward) < -.3 and rear[2] < -.9
    rear_id = model.camera('robot/rear').id
    camera_rotation = data.cam_xmat[rear_id].reshape(3, 3)
    # Both hind feet must actually fit the image, not just be behind the camera.
    for foot in robot.feet[2:]:
        point = camera_rotation.T @ (data.geom_xpos[foot]-data.cam_xpos[rear_id])
        assert point[2] < 0
        assert abs(point[1]/point[2]) < math.tan(math.radians(40))
        assert abs(point[0]/point[2]) < math.tan(math.radians(40))*640/480
    card = robot.card()
    assert card['name'] == {'en': 'Dog', 'es': 'Perro'}
    assert card['cameras'] == ['robot/head', 'robot/rear']
    assert {t['name'] for t in card['tools']} == set(robot.tools()) | {'look'}
    assert all(' / ' in n for n in card['notes'])
    assert len(card['limits']['joints_deg']) == 12
    for i, name in enumerate(robot.joint_names):
        np.testing.assert_allclose(card['limits']['joints_deg'][name], np.rad2deg(robot.lrange[i]), atol=.01)
    assert any('triangle' in n and 'margin' in n for n in card['notes'])
    assert any('0.05 m' in n for n in card['notes'])
    json.dumps(card, allow_nan=False)


def test_analytic_ik_matches_real_foot_geometry():
    robot = DogRobot()
    model, data = build(robot)
    for leg, side in zip(robot.legs, (1, -1, 1, -1)):
        i = robot.legs.index(leg)
        hip = model.body('robot/'+leg+'_hip').id
        p = data.geom_xpos[robot.feet[i]]-data.xpos[hip]
        np.testing.assert_allclose(leg_ik(*p, side), [0, .9, -1.8], atol=1e-10)
    with pytest.raises(ValueError):
        leg_ik(1, .0955, -.2, 1)
