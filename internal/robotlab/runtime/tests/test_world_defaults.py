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


@pytest.mark.parametrize('name', ['mi-cuarto', 'mesa-figuras', 'pasillo'])
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
    room = envs['mi-cuarto']
    ball = next(o for o in room['objects'] if o['id'] == 'pelota')
    spawn = room['spawn']
    assert np.linalg.norm(np.array(ball['pos'][:2]) - [spawn['x'], spawn['y']]) >= 2
    assert room['size'] == [5, 4]
    assert len(room['objects']) == 6
    table = envs['mesa-figuras']
    for oid in ('cubo-rojo', 'cubo-azul', 'cubo-verde'):
        cube = next(o for o in table['objects'] if o['id'] == oid)
        assert cube['size'] == [.05] * 3
        assert cube['pos'][2] == .775
    box = next(o for o in table['objects'] if o['id'] == 'caja')
    assert box['compound'] and not box['dynamic']
    assert envs['pasillo']['size'] == [8, 2]
    assert len({e['floor_color'] for e in envs.values()}) == 3
