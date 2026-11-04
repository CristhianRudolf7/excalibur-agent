"""Measured body footprint and crouched floor-reach guidance."""
import time

import numpy as np
import pytest

from robotlab.robots.dog import DogRobot
from robotlab.robots.testing import SimpleContext, build


def test_body_card_footprint_matches_standing_collision_bounds():
    robot = DogRobot()
    model, data = build(robot)
    robot.stand(SimpleContext(robot, model, data))
    rotation = data.xmat[robot.base].reshape(3, 3)
    bounds = []
    for geom in range(model.ngeom):
        if model.geom_group[geom] != 3 or not model.body(model.geom_bodyid[geom]).name.startswith('robot/'):
            continue
        local_rotation = rotation.T @ data.geom_xmat[geom].reshape(3, 3)
        center = rotation.T @ (data.geom_xpos[geom]-data.xpos[robot.base])
        center += local_rotation @ model.geom_aabb[geom, :3]
        extent = np.abs(local_rotation) @ model.geom_aabb[geom, 3:]
        bounds.append([center-extent, center+extent])
    bounds = np.array(bounds)
    footprint = bounds[:, 1, :2].max(axis=0)-bounds[:, 0, :2].min(axis=0)
    card = robot.card()
    print(f'standing collision footprint including folded arm: {footprint} m')
    np.testing.assert_allclose(card['limits']['body_footprint_m'], footprint, atol=.01)
    assert np.all(np.array(card['limits']['body_footprint_m']) >= footprint)
    assert card['limits']['recommended_clearance_m'] == .35
    assert card['limits']['crouched_floor_reach_m'] == [.25, .60]
    assert all(' / ' in note for note in card['notes'])
    assert any('0.35–0.5 m in front of the trunk center' in note and 'arm_move_to' in note
               for note in card['notes'])


@pytest.mark.parametrize('distance', [.25, .35, .5, .6])
def test_crouched_gripper_reaches_floor_ahead_of_trunk(distance):
    robot = DogRobot()
    model, data = build(robot)
    ctx = SimpleContext(robot, model, data)
    start = time.perf_counter()
    assert robot.crouch(ctx, .22)['ok']
    center = data.xpos[robot.base].copy()
    heading = data.xmat[robot.base].reshape(3, 3)[:2, 0]
    target = np.r_[center[:2]+distance*heading, .025]
    result = robot.arm_move_to(ctx, *target)
    reach = float(np.linalg.norm(data.site_xpos[robot.site, :2]-data.xpos[robot.base, :2]))
    print(f'floor reach {distance:.2f} m: actual {reach:.4f} m, '
          f'grasp height {data.site_xpos[robot.site, 2]:.4f} m, '
          f'{time.perf_counter()-start:.2f} wall s')
    assert result['ok'], result
    assert reach == pytest.approx(distance, abs=.02)
    assert 0 <= data.site_xpos[robot.site, 2] <= .03
    assert robot.fallen(model, data) is None


@pytest.mark.parametrize('distance', [.20, .65])
def test_floor_targets_outside_measured_range_fail_clearly(distance):
    robot = DogRobot()
    model, data = build(robot)
    ctx = SimpleContext(robot, model, data)
    robot.crouch(ctx, .22)
    target = data.xpos[robot.base].copy()
    target[0] += distance
    target[2] = .025
    result = robot.arm_move_to(ctx, *target)
    assert not result['ok']
    assert result['error'] == 'No reachable downward arm solution'
