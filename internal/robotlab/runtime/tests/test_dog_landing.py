"""Landing sensors checked against analytic geometry of the built-in narrow stairs."""
import json
import math

import mujoco
import numpy as np
import pytest

from tests.test_dog_stairs import stairs


def stair_surface(x, y):
    """Independent box-top oracle; dimensions are the built-in world geometry."""
    if abs(y) > .225 or x < 0 or x > 1.5:
        return 0., 'floor'
    if x < .3:
        return .12, 'escalon-1'
    if x < .6:
        return .24, 'escalon-2'
    return .36, 'descanso'


def reference_edge(point, direction, reach, drop_only=False):
    candidates = []
    for axis, boundaries in ((0, (0., .3, .6, 1.5)), (1, (-.225, .225))):
        if abs(direction[axis]) < 1e-10:
            continue
        for boundary in boundaries:
            distance = (boundary-point[axis])/direction[axis]
            if 0 < distance <= reach:
                candidates.append(distance)
    for distance in sorted(candidates):
        before = stair_surface(*(point[:2]+direction*(distance-1e-6)))[0]
        after = stair_surface(*(point[:2]+direction*(distance+1e-6)))[0]
        change = before-after if drop_only else abs(after-before)
        if change > .03:
            return distance, after
    return None, None


def place_front_left(robot, model, data, x, y, yaw):
    """Position a measurement fixture without invoking motion or a gait."""
    data.qpos[robot.rq:robot.rq+7] = [0., 0., .8, math.cos(yaw/2), 0., 0., math.sin(yaw/2)]
    mujoco.mj_forward(model, data)
    data.qpos[robot.rq:robot.rq+2] += np.array([x, y])-data.geom_xpos[robot.feet[0], :2]
    mujoco.mj_forward(model, data)


@pytest.mark.parametrize('x,y,yaw', [
    (-.12, 0., 0.), (-.018, 0., 0.), (.08, 0., 0.),
    (.4, .18, 0.), (.8, -.18, 0.), (1.2, 0., 0.),
    (.4, 0., math.pi), (.1, 0., math.pi/2), (.8, 0., -math.pi/2),
    (.08, -.1, .7), (-.12, .7, 0.),
])
def test_per_foot_edges_match_independent_stair_geometry(x, y, yaw):
    robot, model, data, _ = stairs()
    place_front_left(robot, model, data, x, y, yaw)
    time, qpos, target = data.time, data.qpos.copy(), robot.target.copy()
    state = robot.state(model, data)
    forward = np.array([math.cos(yaw), math.sin(yaw)])
    left = np.array([-math.sin(yaw), math.cos(yaw)])
    for i, leg in enumerate(robot.legs):
        point = data.geom_xpos[robot.feet[i]]
        sensed = state['legs'][leg]
        dx, height = reference_edge(point, forward, .6)
        if dx is None:
            assert sensed['edge_ahead'] == {'dx': None, 'z_after': None}
        else:
            assert sensed['edge_ahead']['dx'] == pytest.approx(dx, abs=.0006)
            assert sensed['edge_ahead']['z_after'] == pytest.approx(height, abs=.0005)
        for side, direction in (('left', left), ('right', -left)):
            distance, _ = reference_edge(point, direction, .4, True)
            if distance is None:
                assert sensed['edge_side'][side] is None
            else:
                assert sensed['edge_side'][side] == pytest.approx(distance, abs=.0006)
    assert data.time == time
    np.testing.assert_array_equal(data.qpos, qpos)
    np.testing.assert_array_equal(robot.target, target)
    json.dumps(state, allow_nan=False)


def assert_landing(info, point):
    height, object_id = stair_surface(*point[:2])
    assert info['terrain_z'] == pytest.approx(height, abs=.0005)
    assert info['lands_on'] == object_id
    assert info['clearance'] == pytest.approx(point[2]-height, abs=.0005)


@pytest.mark.parametrize('point', [
    [-.12, .14, .14], [-.018, .14, .14], [.05, .14, .142],
    [.35, .14, .262], [.65, .14, .382], [.05, .26, .142],
    [.05, .14, .10], [2.5, 0., .1],
])
@pytest.mark.parametrize('frame', ['world', 'body'])
def test_ik_reports_landing_even_for_unreachable_targets(point, frame):
    from robotlab.robots.dog import rotation
    robot, model, data, ctx = stairs()
    time, qpos, qvel, target = data.time, data.qpos.copy(), data.qvel.copy(), robot.target.copy()
    pose = robot.pose(data)
    trunk = {'yaw': 30., 'z': .35}
    desired = pose.copy()
    desired[5], desired[2] = math.radians(30.), .35
    value = point if frame == 'world' else (rotation(desired).T @ (np.array(point)-desired[:3])).tolist()
    result = robot.ik(ctx, {'FL': value}, frame=frame, trunk=trunk)
    assert result['ok'], result
    assert_landing(result['legs']['FL'], point)
    for i, leg in enumerate(robot.legs[1:], start=1):
        assert_landing(result['legs'][leg], data.geom_xpos[robot.feet[i]])
    if point[0] > 2:
        assert not result['legs']['FL']['reachable']
    assert data.time == time
    np.testing.assert_array_equal(data.qpos, qpos)
    np.testing.assert_array_equal(data.qvel, qvel)
    np.testing.assert_array_equal(robot.target, target)
    json.dumps(result, allow_nan=False)


@pytest.mark.parametrize('tool', ['move_feet', 'sequence'])
@pytest.mark.parametrize('failure', [None, 'reach', 'duration', 'settle'])
def test_motion_preflight_reports_accepted_and_rejected_landings(tool, failure):
    robot, model, data, ctx = stairs()
    robot.wait(ctx, .5)
    point = data.geom_xpos[robot.feet[0]].copy()
    point[2] += .02
    if failure == 'reach':
        point = np.array([.35, .14, .20])
    time, qpos, target, mode = data.time, data.qpos.copy(), robot.target.copy(), robot.mode
    key = dict(type='feet', feet={'FL': point.tolist()}, duration=0 if failure == 'duration' else .002)
    timing = dict(settle=3 if failure == 'settle' else 0)
    if tool == 'move_feet':
        result = robot.move_feet(ctx, key['feet'], duration=key['duration'], **timing)
    else:
        result = robot.sequence(ctx, [key], **timing)
    assert result['ok'] == (failure is None), result
    assert len(result['preflight']) == 1
    report = result['preflight'][0]
    assert report['keyframe'] == 0
    assert set(report['legs']) == set(robot.legs)
    assert_landing(report['legs']['FL'], point)
    for leg, info in report['legs'].items():
        assert_landing(info, info['foot_world'])
    if failure:
        assert data.time == time
        np.testing.assert_array_equal(data.qpos, qpos)
        np.testing.assert_array_equal(robot.target, target)
        assert robot.mode == mode
        for info in result['details']:
            if info.get('leg') == 'FL':
                assert_landing(info, point)
    json.dumps(result, allow_nan=False)


def test_sequence_reports_all_targets_after_a_rejected_keyframe():
    robot, model, data, ctx = stairs()
    time, qpos = data.time, data.qpos.copy()
    points = [[-.12, .14, .14], [.05, .14, .10], [.35, .14, .262], [.65, .14, .382]]
    result = robot.sequence(ctx, [dict(type='feet', feet={'FL': point}, duration=.01) for point in points])
    assert not result['ok']
    assert len(result['preflight']) == len(points)
    for k, (report, point) in enumerate(zip(result['preflight'], points)):
        assert report['keyframe'] == k
        assert_landing(report['legs']['FL'], point)
    assert data.time == time
    np.testing.assert_array_equal(data.qpos, qpos)


def test_stopped_sequence_keeps_body_frame_and_omitted_target_preflight():
    from robotlab.robots.dog import rotation
    robot, model, data, ctx = stairs()
    robot.wait(ctx, .5)
    pose = robot.pose(data)
    desired = pose.copy()
    desired[5] += math.radians(2)
    point = np.array([-.28, .14, .06])
    body_point = rotation(desired).T @ (point-desired[:3])
    key = dict(type='feet', feet={'FL': body_point.tolist()}, frame='body',
               trunk={'yaw': math.degrees(desired[5])}, duration=.1)
    time = data.time
    result = robot.sequence(ctx, [key, dict(type='feet', feet={}, duration=.1)],
                            stop_if={'margin_below': 2})
    assert result['ok'] and result['stopped_at'] == 0, result
    assert data.time == time
    for report in result['preflight']:
        assert_landing(report['legs']['FL'], point)
        np.testing.assert_allclose(report['legs']['FL']['foot_world'], point, atol=.0005)
        for i, leg in enumerate(robot.legs[1:], start=1):
            np.testing.assert_allclose(report['legs'][leg]['foot_world'], data.geom_xpos[robot.feet[i]], atol=.0005)


def test_joint_keyframe_preflight_predicts_feet_for_following_omitted_targets():
    robot, model, data, ctx = stairs()
    robot.wait(ctx, .5)
    scratch = mujoco.MjData(model)
    scratch.qpos[:] = data.qpos
    scratch.qpos[robot.lq[1]] = math.radians(53)
    mujoco.mj_forward(model, scratch)
    time, qpos = data.time, data.qpos.copy()
    result = robot.sequence(ctx, [dict(type='joints', joints={'FL_thigh': 53}, duration=.1),
                                 dict(type='feet', feet={}, duration=.1)], stop_if={'margin_below': 2})
    assert result['ok'] and result['stopped_at'] == 0, result
    assert len(result['preflight']) == 2
    for report in result['preflight']:
        for i, leg in enumerate(robot.legs):
            point = scratch.geom_xpos[robot.feet[i]]
            np.testing.assert_allclose(report['legs'][leg]['foot_world'], point, atol=.0005)
            assert_landing(report['legs'][leg], point)
    assert data.time == time
    np.testing.assert_array_equal(data.qpos, qpos)


@pytest.mark.parametrize('x', [.05, .35, .65])
def test_accepted_stair_target_reports_surface_not_current_robot_pose(x):
    robot, model, data, ctx = stairs()
    place_front_left(robot, model, data, x, .14, 0.)
    height, _ = stair_surface(x, .14)
    data.qpos[robot.rq+2] = height+.27
    mujoco.mj_forward(model, data)
    point = [x, .14, height+.022]
    result = robot.move_feet(ctx, {'FL': point}, duration=.002, settle=0)
    assert result['ok'], result
    assert_landing(result['preflight'][0]['legs']['FL'], point)


@pytest.mark.parametrize('aerial', ['FL', 'FR', 'RL', 'RR'])
def test_three_contact_lift_matches_independent_stair_support_triangle(aerial):
    from tests.test_dog_lift import reference_geometry
    robot, model, data, ctx = stairs()
    robot.wait(ctx, .5)
    data.ncon = 0
    triangle = []
    for leg, foot in zip(robot.legs, robot.feet):
        if leg != aerial:
            contact = mujoco.MjContact()
            contact.geom1, contact.geom2 = foot, model.geom('floor').id
            mujoco.mj_addContact(model, data, contact)
            triangle.append(data.geom_xpos[foot].copy())
    time, qpos, target = data.time, data.qpos.copy(), robot.target.copy()
    state = robot.state(model, data)
    margin, _, _ = reference_geometry(triangle, data.subtree_com[robot.base])
    assert set(state['lift']) == set(robot.legs)
    assert set(state['support']) == set(robot.legs)-{aerial}
    assert state['lift'][aerial]['aerial'] is True
    assert state['lift'][aerial]['support_margin'] == pytest.approx(margin, abs=.0005)
    for leg in state['support']:
        assert state['lift'][leg] == {'margin': None}
    assert data.time == time
    np.testing.assert_array_equal(data.qpos, qpos)
    np.testing.assert_array_equal(robot.target, target)
    json.dumps(state, allow_nan=False)


def test_card_teaches_landing_edge_and_three_contact_sensing_in_both_languages():
    from robotlab.robots.dog import DogRobot
    notes = DogRobot().card()['notes']
    for marker, fields in (
        ('edge_ahead.dx measures', ('edge_ahead.dx', 'z_after', '0.6 m', 'null')),
        ('edge_side.left and', ('edge_side.left', 'edge_side.right', '0.4 m', 'null')),
        ('Before swinging', ('ik', 'lands_on', 'terrain_z', 'clearance', '0.022 m')),
        ('Use edge_ahead.dx', ('edge_ahead.dx+0.05 m', 'lands_on', 'terrain_z', 'ik')),
        ('With three contacts', ('aerial=true', 'support_margin', 'margin=null')),
        ('preflight per keyframe', ('preflight', 'keyframe', 'legs', 'foot_world', 'terrain_z', 'lands_on', 'clearance')),
    ):
        note = next(note for note in notes if marker in note)
        for translation in note.split(' / '):
            assert all(field in translation for field in fields), translation
    assert all(' / ' in note for note in notes)


def test_exact_three_centimeter_changes_are_not_reported_as_edges():
    from robotlab.robots.dog import DogRobot
    from robotlab.robots.dog_sensors import terrain_edge
    from robotlab.robots.testing import build
    robot = DogRobot()
    model, data = build(robot, [
        dict(id='low', shape='box', size=[.1, .2, .015], pos=[.2, 0., .015], dynamic=False),
        dict(id='high', shape='box', size=[.1, .2, .0305], pos=[.4, 0., .0305], dynamic=False),
    ])
    # The .03 m rise at x=.1 does not exceed the threshold; the .031 m
    # change at x=.3 is the first qualifying edge. Robot geometry is ignored.
    edge = terrain_edge(model, data, [0., 0.], [1., 0.], .6)
    assert edge['dx'] == pytest.approx(.3, abs=.0006)
    assert edge['z_after'] == pytest.approx(.061, abs=.0005)
    assert terrain_edge(model, data, [.2, 0.], [0., 1.], .4, True) == {'dx': None, 'z_after': None}
