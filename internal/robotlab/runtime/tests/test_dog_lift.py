"""Per-leg lift sensing, independent geometry and planted-foot weight transfer."""
import itertools
import json
import math

import mujoco
import numpy as np
import pytest

from robotlab.robots.base import Spawn
from robotlab.robots.dog import DogRobot
from robotlab.robots.dog_sensors import lift_geometry
from robotlab.robots.testing import SimpleContext, build


def reference_geometry(triangle, com):
    """Independent half-plane calculation with all active-set projections."""
    triangle, com = np.asarray(triangle)[:, :2], np.asarray(com)[:2]
    normals, offsets, distances = [], [], []
    for i in range(3):
        a, b, opposite = triangle[i], triangle[(i+1) % 3], triangle[(i+2) % 3]
        edge = b-a
        normal = np.array([-edge[1], edge[0]])/np.linalg.norm(edge)
        if normal @ (opposite-a) < 0:
            normal = -normal
        normals.append(normal)
        offsets.append(normal @ a)
        nearest = a+np.clip((com-a) @ edge/(edge @ edge), 0, 1)*edge
        distances.append(np.linalg.norm(com-nearest))
    normals, offsets = np.array(normals), np.array(offsets)
    inside = np.all(normals @ com >= offsets-1e-12)
    margin = min(distances)*(1 if inside else -1)
    # Equal distances to the three lines locate the incenter independently.
    center = np.linalg.solve(np.column_stack((normals, -np.ones(3))), offsets)[:2]
    inset_offsets = offsets+.04
    candidates = [com]
    candidates.extend(com+(offset-normal @ com)*normal for normal, offset in zip(normals, inset_offsets))
    candidates.extend(np.linalg.solve(normals[[i, j]], inset_offsets[[i, j]])
                      for i, j in itertools.combinations(range(3), 2))
    feasible = [point for point in candidates if np.all(normals @ point >= inset_offsets-1e-12)]
    nearest = min(feasible, key=lambda point: np.linalg.norm(point-com))
    return margin, center, nearest-com


@pytest.mark.parametrize('com', [[-.2, -.1], [.2, .2], [.01, .15], [.05, .05], [.1, .1]])
@pytest.mark.parametrize('order', list(itertools.permutations(range(3))))
def test_lift_incenter_and_nearest_clearance_point(com, order):
    triangle = np.array([[0., 0.], [.4, 0.], [0., .3]])[list(order)]
    angle = .7
    rotation = np.array([[math.cos(angle), -math.sin(angle)], [math.sin(angle), math.cos(angle)]])
    triangle = triangle @ rotation.T+[1., -2.]
    com = rotation @ com+[1., -2.]
    margin, center, shift = reference_geometry(triangle, com)
    result = lift_geometry(triangle, com)
    assert result['margin'] == pytest.approx(margin, abs=.0005)
    np.testing.assert_allclose(result['triangle_center'], center, atol=.0005)
    np.testing.assert_allclose(result['shift_needed'], shift, atol=.0005)
    if margin >= .04:
        assert result['shift_needed'] == [0., 0.]
    else:
        projected_margin, _, _ = reference_geometry(triangle, com+result['shift_needed'])
        assert projected_margin >= .039
    json.dumps(result, allow_nan=False)


@pytest.mark.parametrize('triangle', [
    [[0., 0.], [.4, 0.], [.2, .01]],
    [[0., 0.], [.4, 0.], [.2, 0.]],
    [[0., 0.], [0., 0.], [0., 0.]],
])
def test_impossible_clearance_uses_finite_incenter_fallback(triangle):
    com = np.array([.1, -.1])
    result = lift_geometry(triangle, com)
    np.testing.assert_allclose(com+result['shift_needed'], result['triangle_center'], atol=.001)
    assert result['margin'] < 0
    json.dumps(result, allow_nan=False)


@pytest.mark.parametrize('spawn', [Spawn(), Spawn(1., -2., math.pi/2)])
def test_home_lift_state_matches_measured_world_geometry(spawn):
    robot = DogRobot()
    model, data = build(robot, spawn=spawn)
    ctx = SimpleContext(robot, model, data)
    ctx.step(.5)
    time, qpos, targets = data.time, data.qpos.copy(), robot.target.copy()
    state = robot.state(model, data)
    assert set(state['lift']) == set(robot.legs)
    for i, leg in enumerate(robot.legs):
        info = state['lift'][leg]
        assert set(info) == {'margin', 'triangle_center', 'shift_needed'}
        triangle = np.delete(data.geom_xpos[robot.feet], i, axis=0)
        margin, center, shift = reference_geometry(triangle, data.subtree_com[robot.base])
        assert info['margin'] == pytest.approx(margin, abs=.0005)
        assert abs(info['margin']) < .02
        np.testing.assert_allclose(info['triangle_center'], center, atol=.0005)
        np.testing.assert_allclose(info['shift_needed'], shift, atol=.0005)
    assert any(info['margin'] < 0 for info in state['lift'].values())
    assert data.time == time
    np.testing.assert_array_equal(data.qpos, qpos)
    np.testing.assert_array_equal(robot.target, targets)
    json.dumps(state, allow_nan=False)


@pytest.mark.parametrize('missing', [(), ('FL',), ('FR', 'RL'), ('FL', 'FR', 'RL', 'RR')])
def test_lift_reports_available_support_geometry(missing):
    robot = DogRobot()
    model, data = build(robot)
    # Isolate contact gating without changing the measured foot geometry.
    data.ncon = 0
    for leg, foot in zip(robot.legs, robot.feet):
        if leg not in missing:
            contact = mujoco.MjContact()
            contact.geom1, contact.geom2 = foot, model.geom('floor').id
            mujoco.mj_addContact(model, data, contact)
    state = robot.state(model, data)
    assert set(state['support']) == set(robot.legs)-set(missing)
    assert set(state['lift']) == (set(robot.legs) if len(missing) <= 1 else set())
    if len(missing) == 1:
        assert state['lift'][missing[0]] == {'aerial': True, 'support_margin': state['margin']}
        assert all(state['lift'][leg] == {'margin': None} for leg in state['support'])
    json.dumps(state, allow_nan=False)


def test_sensed_trunk_shift_allows_front_left_lift():
    robot = DogRobot()
    model, data = build(robot)
    ctx = SimpleContext(robot, model, data)
    state = robot.wait(ctx, .5)['state']
    shift = state['lift']['FL']['shift_needed']
    assert state['lift']['FL']['margin'] < .02
    planted = data.geom_xpos[robot.feet].copy()
    result = robot.move_feet(ctx, {}, trunk={'x': state['x']+shift[0], 'y': state['y']+shift[1]}, duration=1)
    assert result['ok'], result
    assert result['state']['lift']['FL']['margin'] >= .03
    np.testing.assert_allclose(data.geom_xpos[robot.feet], planted, atol=.01)
    foot = result['state']['legs']['FL']['foot_world']
    result = robot.move_feet(ctx, {'FL': [foot[0], foot[1], foot[2]+.08]}, duration=.7)
    assert result['ok'], result
    state = robot.wait(ctx, 3)['state']
    assert set(state['support']) == {'FR', 'RL', 'RR'}
    assert state['lift']['FL'] == {'aerial': True, 'support_margin': state['margin']}
    assert all(state['lift'][leg] == {'margin': None} for leg in state['support'])
    assert state['margin'] >= .03
    assert abs(state['roll_deg']) < 3 and abs(state['pitch_deg']) < 3
    assert robot.fallen(model, data) is None
    json.dumps(state, allow_nan=False)


def test_card_explains_lift_sensing_and_manual_trunk_shift():
    notes = DogRobot().card()['notes']
    assert all(' / ' in note for note in notes)
    sensing = next(note for note in notes if 'triangle_center' in note)
    spanish, english = sensing.split(' / ')
    for text in (spanish, english):
        assert all(field in text for field in ('lift', 'margin', 'triangle_center', 'shift_needed', '0.04 m'))
    guidance = next(note for note in notes if 'lift.<leg>.margin' in note)
    spanish, english = guidance.split(' / ')
    for text in (spanish, english):
        assert all(field in text for field in ('move_feet', 'feet={}', 'trunk', 'shift_needed', 'COM'))
