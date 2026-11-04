import json

import mujoco
import numpy as np
import pytest

from robotlab import assets
from robotlab.world.builder import World, build_world
from robotlab.world.meshes import mesh_blob


@pytest.fixture(autouse=True)
def cache(tmp_path, monkeypatch):
    monkeypatch.setattr(assets, '_data_dir', tmp_path)


def test_visible_body_order_material_local_frames_and_mesh_keys():
    spec = mujoco.MjSpec.from_string('''<mujoco><asset>
      <material name="green" rgba=".1 .7 .2 .8"/>
      <mesh name="tetra" vertex="0 0 0 .1 0 0 0 .1 0 0 0 .1"/>
      </asset><worldbody>
      <geom name="floor" type="plane" size="2 2 .1"/>
      <body name="robot/hidden"><geom size=".1" group="3"/></body>
      <body name="robot/invisible"><geom size=".1" rgba="1 0 0 0"/></body>
      <body name="robot/base" pos="1 2 3" euler="0 0 90">
        <geom type="box" size=".1 .2 .3" pos=".1 .2 .3" quat=".70710678 .70710678 0 0" material="green" rgba="1 0 0 0"/>
        <geom type="mesh" mesh="tetra" group="2"/>
        <geom size="1" group="3"/>
        <body name="robot/joint-only" pos="0 0 1">
          <body name="robot/tip"><geom size=".1"/></body>
        </body>
        <camera name="robot/head" pos=".5 0 .2" euler="90 0 0"/>
      </body></worldbody></mujoco>''')
    model = spec.compile()
    data = mujoco.MjData(model)
    mujoco.mj_forward(model, data)
    world = World(model, data, {'id': 'test', 'objects': []}, type('Robot', (), {'id': 'dog'})())
    scene = world.scene()
    assert [body['name'] for body in scene['bodies']] == ['world', 'robot/base', 'robot/tip']
    assert len(world.poses()) == 7 * len(scene['bodies'])
    np.testing.assert_allclose(world.poses()[7:14], np.r_[data.xpos[3], data.xquat[3]].round(4))
    geom, mesh = scene['bodies'][1]['geoms']
    np.testing.assert_allclose(geom['rgba'], [.1, .7, .2, .8])
    np.testing.assert_allclose(geom['pos'], [.1, .2, .3])
    np.testing.assert_allclose(geom['quat'], [2**-.5, 2**-.5, 0, 0])
    assert json.loads(mesh_blob(mesh['mesh']))['faces'] == 4
    assert mesh['scale'] == [1, 1, 1]
    assert scene['robot'] == 'dog'
    assert scene['environment'] == 'test'
    assert scene['cameras'] == [{'name': 'robot/head', 'fovy': 45.0}]
    pose = world.camera_pose('robot/head')
    cid = model.camera('robot/head').id
    np.testing.assert_allclose(pose[:3], data.cam_xpos[cid])
    matrix = np.empty(9)
    mujoco.mju_quat2Mat(matrix, np.array(pose[3:]))
    np.testing.assert_allclose(matrix, data.cam_xmat[cid], atol=1e-14)
    scene['bodies'].clear()
    assert len(world.scene()['bodies']) == 3


def test_objects_reference_scene_indices_and_pose_updates():
    world = build_world({'id': 'test', 'objects': [
        {'id': 'ball', 'shape': 'sphere', 'size': [.2], 'pos': [1, 1]},
        {'id': 'desk', 'shape': 'box', 'size': [.2, .4, .6], 'pos': [-1, -1], 'dynamic': False},
    ]}, None, '')
    scene = world.scene()
    assert scene['robot'] is None
    for obj in scene['objects']:
        assert scene['bodies'][obj['body']]['name'] == 'obj/' + obj['id']
        pose = world.poses()[obj['body'] * 7:obj['body'] * 7 + 7]
        np.testing.assert_allclose(pose[:3], world.data.xpos[world.object_body(obj['id'])])
    assert all(g['pos'] for body in scene['bodies'] for g in body['geoms'])
    assert len(scene['bodies'][0]['geoms']) == 5  # floor and four automatic walls
    assert scene['bounds']['min'][0] <= -2.5
    assert scene['bounds']['max'][2] == 2.5
    before = world.poses()
    world.data.qpos[0] = 1.123456
    mujoco.mj_forward(world.model, world.data)
    assert before != world.poses()
    assert world.poses()[7] == 1.1235
