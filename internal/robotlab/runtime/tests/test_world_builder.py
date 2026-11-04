import math

import mujoco
import numpy as np
import pytest

from robotlab.world.builder import build_world


class TinyRobot:
    id = 'dog'
    world_kind = 'room'

    def __init__(self, kind='room'):
        self.world_kind = kind
        self.calls = []

    def attach(self, spec, spawn, menagerie):
        self.calls.append('attach')
        self.spawn, self.menagerie = spawn, menagerie
        body = spec.worldbody.add_body(name='robot/base', pos=[spawn.x, spawn.y, .1])
        body.add_freejoint()
        body.add_geom(type=mujoco.mjtGeom.mjGEOM_BOX, size=[.1] * 3, mass=1)
        body.add_camera(name='robot/head', pos=[0, 0, .2])

    def bind(self, model, data):
        self.calls.append('bind')
        self.qadr = model.jnt_qposadr[model.body('robot/base').jntadr[0]]

    def reset(self, model, data):
        self.calls.append('reset')
        data.qpos[self.qadr + 2] = .2


def test_builder_geometry_full_sizes_and_lifecycle():
    robot = TinyRobot()
    env = {'id': 'sizes', 'spawn': {'x': -1, 'yaw': 90}, 'objects': [
        {'id': 'box', 'shape': 'box', 'size': [.2, .4, .6], 'pos': [.5, .5], 'yaw': 90, 'mass': 2},
        {'id': 'ball', 'shape': 'sphere', 'size': [.4], 'pos': [1, 1], 'dynamic': False},
        {'id': 'cylinder', 'shape': 'cylinder', 'size': [.4, .6], 'pos': [-1, 1]},
        {'id': 'pill', 'shape': 'capsule', 'size': [.4, .6], 'pos': [-1, -1]},
    ], 'walls': [{'from': [-2, -1], 'to': [-2, 1]}]}
    world = build_world(env, robot, 'fake-menagerie')
    model, data = world.model, world.data
    assert robot.calls == ['attach', 'bind', 'reset']
    assert robot.spawn.yaw == math.pi / 2
    assert robot.menagerie == 'fake-menagerie'
    assert data.xpos[model.body('robot/base').id, 2] == .2
    assert world.object_ids() == ['box', 'ball', 'cylinder', 'pill']
    assert model.body(world.object_body('box')).name == 'obj/box'
    assert model.body('obj/box').mass == 2
    assert model.body('obj/box').jntnum == 1
    assert model.body('obj/ball').jntnum == 0
    np.testing.assert_allclose(model.geom('obj/box').size, [.1, .2, .3])
    np.testing.assert_allclose(model.geom('obj/ball').size, [.2, 0, 0])
    np.testing.assert_allclose(model.geom('obj/cylinder').size, [.2, .3, 0])
    np.testing.assert_allclose(model.geom('obj/pill').size, [.2, .1, 0])
    np.testing.assert_allclose(data.xpos[world.object_body('box')], [.5, .5, .3])
    np.testing.assert_allclose(data.xquat[world.object_body('box')], [2**-.5, 0, 0, 2**-.5])
    assert model.geom('floor').type == mujoco.mjtGeom.mjGEOM_PLANE
    assert model.geom('wall/4').size[1] == .05
    assert model.ncam == 2
    assert len(world.camera_pose('overview')) == 7
    with pytest.raises(KeyError):
        world.object_body('unknown')
    with pytest.raises(KeyError):
        world.camera_pose('unknown')
    info = world.object_info('box')
    info['size'][0] = 999
    assert world.object_info('box')['size'][0] == .2


def test_table_top_spawn_and_open_bin():
    robot = TinyRobot('table')
    world = build_world({'id': 'table-test', 'kind': 'table', 'objects': [
        {'id': 'bin', 'shape': 'box', 'size': [.3, .2, .1], 'pos': [.2, 0], 'compound': True, 'dynamic': False},
        {'id': 'cube', 'shape': 'box', 'size': [.05] * 3, 'pos': [-.2, 0]},
    ]}, robot, '')
    model = world.model
    assert robot.spawn.x == -.75
    assert model.geom('table/top').pos[2] + model.geom('table/top').size[2] == .75
    assert model.body('table').geomnum == 5
    assert model.body('obj/bin').geomnum == 5
    assert model.body('obj/bin').jntnum == 0
    assert world.data.xpos[world.object_body('cube'), 2] == .775
    assert not any(model.geom(i).name.startswith('wall/') for i in range(model.ngeom))
    # The bin's center above its base is empty, and every part is on one body.
    assert model.geom('obj/bin').pos[2] < 0
    assert model.geom('obj/bin/left').pos[0] < 0
    with pytest.raises(ValueError, match='requires environment kind'):
        build_world({'id': 'room'}, robot, '')


def test_preview_without_robot_and_no_walls():
    world = build_world({'id': 'empty', 'wall_height': 0}, None, '')
    assert world.robot is None
    assert world.model.ngeom == 1
    assert world.model.nlight == 3
