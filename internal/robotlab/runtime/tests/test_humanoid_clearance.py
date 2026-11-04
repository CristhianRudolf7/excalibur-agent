"""Measured body envelope and distance-gait guidance in the body card."""
import math
import time

import mujoco
import numpy as np
import pytest

from robotlab.robots.humanoid import HumanoidRobot
from robotlab.robots.testing import SimpleContext, build


def arm_bounds(model, data, robot):
    """Exact lateral support of mesh and primitive arm geoms in pelvis frame."""
    bounds = []
    for g in range(model.ngeom):
        name = model.body(model.geom_bodyid[g]).name
        if not any(part in name for part in ('shoulder', 'elbow', 'wrist', 'hand')):
            continue
        center = (data.geom_xpos[g] - data.xpos[robot.pelvis]) @ data.xmat[robot.pelvis].reshape(3, 3)
        local = data.xmat[robot.pelvis].reshape(3, 3).T @ data.geom_xmat[g].reshape(3, 3)
        kind = model.geom_type[g]
        if kind == mujoco.mjtGeom.mjGEOM_MESH:
            mesh = model.geom_dataid[g]
            begin, count = model.mesh_vertadr[mesh], model.mesh_vertnum[mesh]
            y = (model.mesh_vert[begin:begin + count] @ local.T)[:, 1] + center[1]
            bounds.extend([float(y.min()), float(y.max())])
            continue
        size = model.geom_size[g]
        if kind == mujoco.mjtGeom.mjGEOM_SPHERE:
            radius = size[0]
        elif kind == mujoco.mjtGeom.mjGEOM_CAPSULE:
            radius = size[0] + size[1] * abs(local[1, 2])
        elif kind == mujoco.mjtGeom.mjGEOM_CYLINDER:
            radius = size[0] * math.sqrt(max(0, 1 - local[1, 2] ** 2)) + size[1] * abs(local[1, 2])
        elif kind == mujoco.mjtGeom.mjGEOM_BOX:
            radius = abs(local[1]) @ size
        else:
            raise AssertionError(f'unmeasured arm geom type {kind}')
        bounds.extend([center[1] - radius, center[1] + radius])
    return min(bounds), max(bounds)


def test_card_arm_width_matches_geometry_and_wall_contact():
    robot = HumanoidRobot()
    model, data = build(robot, [{'id': 'wall', 'shape': 'box', 'size': [4, .05, 1],
                                'pos': [0, 1.05, 1], 'dynamic': False}])
    robot.stand(SimpleContext(robot, model, data))
    lo, hi = arm_bounds(model, data, robot)
    print(f'default shoulder/arm bounds: {lo:.6f}..{hi:.6f} m from pelvis')
    half_width = max(abs(lo), abs(hi))
    assert half_width == pytest.approx(.275, abs=.002)
    limits = robot.card()['limits']
    assert half_width <= limits['shoulder_arm_half_width_m'] <= half_width + .01
    assert limits['recommended_wall_clearance_m'] >= half_width + .12 + .05
    wall = model.geom('obj/wall').id
    for clearance, expected_contact in [(.27, True), (.30, False)]:
        data.qpos[robot.root + 1] = 1 - clearance
        mujoco.mj_forward(model, data)
        arm_contact = False
        for contact in data.contact:
            if wall not in contact.geom:
                continue
            other = contact.geom[1] if contact.geom[0] == wall else contact.geom[0]
            name = model.body(model.geom_bodyid[other]).name
            arm_contact |= any(part in name for part in ('shoulder', 'elbow', 'wrist', 'hand'))
        assert arm_contact == expected_contact


def test_small_steps_match_card_speed_guidance():
    robot = HumanoidRobot()
    model, data = build(robot)
    wall = time.perf_counter()
    result = robot.walk(SimpleContext(robot, model, data), .6, step_length=.06)
    speed = result['distance_achieved'] / data.time
    print(f'small steps: {data.time:.2f}s sim, {time.perf_counter() - wall:.2f}s wall, {speed:.4f} m/s')
    assert result['ok']
    assert .02 < speed < .04
    notes = robot.card()['notes']
    assert any('step_length<0.08' in note and '0.03 m/s' in note and '60 s' in note for note in notes)
    assert any('0.45 m' in note and '0.28 m' in note for note in notes)
