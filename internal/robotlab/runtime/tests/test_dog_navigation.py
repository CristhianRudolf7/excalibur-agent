"""Support polygon geometry, contact sensors and real terrain rays."""
import json
import math

import numpy as np
import pytest

from robotlab.robots.dog import DogRobot
from robotlab.robots.dog_sensors import support_geometry, terrain
from robotlab.robots.testing import SimpleContext, build


def test_signed_polygon_and_degenerate_support():
    square = [[-1, -1], [1, -1], [1, 1], [-1, 1]]
    assert support_geometry(square, [.2, 0], 0)[0] == pytest.approx(.8)
    margin, angle = support_geometry(square, [1.2, 0], math.pi/2)
    assert margin == pytest.approx(-.2) and angle == pytest.approx(-90)
    assert support_geometry([[0, 0], [1, 0]], [.5, .3], 0)[0] == pytest.approx(-.3)
    assert support_geometry([[0, 0]], [.3, .4], 0)[0] == pytest.approx(-.5)
    assert support_geometry([], [0, 0], 0)[0] < 0


def test_terrain_rays_ignore_robot_and_find_dropoffs():
    robot = DogRobot()
    model, data = build(robot, [dict(id='ledge', shape='box', size=[.5, .225, .06], pos=[.8, 0, .06], dynamic=False)])
    ctx = SimpleContext(robot, model, data)
    ctx.step(.5)
    assert terrain(model, data, 0, 0) == pytest.approx(0)
    assert terrain(model, data, .4, 0) == pytest.approx(.12)
    state = robot.state(model, data)
    assert len(state['terrain_ahead']) == 18
    assert any(p['z'] == .12 for p in state['terrain_ahead'])
    assert any(p['side'] == 'left' and .21 < p['dy'] < .24 for p in state['edges'])
    assert any(p['side'] == 'right' and -.24 < p['dy'] < -.21 for p in state['edges'])
    contacts = robot.feet_contacts(model, data)
    assert all(ids == [model.geom('floor').id] for ids in contacts.values())
    assert all(v['force_n'] > 20 for v in state['legs'].values())
    assert state['contacts'] == []
    json.dumps(state, allow_nan=False)
