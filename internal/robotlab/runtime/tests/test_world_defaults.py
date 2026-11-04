import json
from pathlib import Path

import mujoco
import numpy as np
import pytest

from robotlab import assets
from robotlab.world.builder import build_world
from robotlab.world.schema import normalize, validate
from robotlab.world.thumbnails import render_overview
from tests.test_world_builder import TinyRobot

DEFAULTS = Path(__file__).parents[1] / 'robotlab/world/defaults'


@pytest.mark.parametrize('name', ['escalera-estrecha', 'estante-pequeno'])
@pytest.mark.parametrize('with_robot', [False, True])
def test_default_build_scene_poses_and_thumbnail(name, with_robot, tmp_path, monkeypatch):
    monkeypatch.setattr(assets, '_data_dir', tmp_path)
    env = json.loads((DEFAULTS / f'{name}.json').read_text())
    assert validate(env) == []
    robot = TinyRobot(env['kind']) if with_robot else None
    world = build_world(env, robot, '')
    scene = world.scene()
    assert scene['environment'] == name
    assert len(world.poses()) == 7 * len(scene['bodies'])
    assert len(scene['objects']) == len(env['objects'])
    assert render_overview(world).startswith(b'\x89PNG')
    if with_robot:
        assert robot.calls == ['attach', 'bind', 'reset']
    # Initial objects stay above their support during a short physics run.
    for _ in range(50):
        mujoco.mj_step(world.model, world.data)
    for oid in world.object_ids():
        assert world.data.xpos[world.object_body(oid), 2] >= 0


def test_defaults_have_required_layout_and_objects():
    envs = {p.stem: normalize(json.loads(p.read_text())) for p in DEFAULTS.glob('*.json')}
    assert set(envs) == {'escalera-estrecha', 'estante-pequeno'}
    room = envs['escalera-estrecha']
    assert room['kind'] == 'room'
    assert room['spawn'] == {'x': -.5, 'y': 0, 'yaw': 0}
    assert [o['id'] for o in room['objects']] == ['escalon-1', 'escalon-2', 'descanso']
    assert [o['size'][2] for o in room['objects']] == [.12, .24, .36]
    assert all(not o['dynamic'] and o['size'][1] == .45 for o in room['objects'])
    table = envs['estante-pequeno']
    assert table['kind'] == 'table'
    box, shelf = table['objects']
    assert box['id'] == 'caja' and box['size'] == [.07]*3 and box['dynamic']
    assert shelf['id'] == 'estante' and shelf['size'] == [.05, .05, .4] and not shelf['dynamic']
    assert box['pos'][2] == .785
    assert shelf['pos'][2] == .95
    assert all(e['source']['type'] == 'text' for e in envs.values())
