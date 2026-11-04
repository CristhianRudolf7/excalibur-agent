"""Viewer and catalog reads stay available while the simulation owns an action."""
import base64
import threading
from concurrent.futures import ThreadPoolExecutor

import mujoco
import pytest

from robotlab.protocol import RPCError
from robotlab.server import Runtime
from robotlab.world.meshes import register_mesh


def test_all_reads_during_long_action(tmp_path, environment, fake_modules, monkeypatch):
    rt = Runtime(tmp_path / 'data', tmp_path / 'skills')
    rt.environments.save(environment)
    sid = rt.dispatch('session.start', {'robot': 'dog', 'environment': 'test-room', 'task': 'Move',
                      'goal': {'type': 'robot_near', 'target': {'point': [2, 0]}, 'radius': .1}})['session']['id']
    rt.dispatch('skills.write', {'session': sid, 'name': 'move', 'description': 'Move', 'instructions': 'Move slowly.'})
    observation = rt.dispatch('robot.look', {'session': sid})
    from robotlab.world import thumbnails
    monkeypatch.setattr(thumbnails, 'render_overview', lambda *_args, **_kwargs: base64.b64decode(observation['png']))
    image = rt.dispatch('session.log', {'session': sid})['entries'][-1]['image']
    model = mujoco.MjModel.from_xml_string('''<mujoco><asset><mesh name="tetra"
        vertex="0 0 0 1 0 0 0 1 0 0 0 1"/></asset><worldbody>
        <geom type="mesh" mesh="tetra"/></worldbody></mujoco>''')
    key = register_mesh(model, 0, 'dog')
    scene = rt.dispatch('session.scene', {'session': sid})
    # A reader must never export geometry from the live world again.
    monkeypatch.setattr(rt.sessions.world, 'scene', lambda: pytest.fail('live scene exported twice'))
    entered, release = threading.Event(), threading.Event()
    original = rt.sessions.world.robot.push

    def long_push(ctx, **args):
        ctx.step(.1)
        entered.set()
        assert release.wait(10)
        return original(ctx, **args)

    monkeypatch.setattr(rt.sessions.world.robot, 'push', long_push)
    with ThreadPoolExecutor(max_workers=1) as pool:
        action = pool.submit(rt.dispatch, 'robot.act', {'session': sid, 'tool': 'push', 'args': {'dx': .2}})
        try:
            assert entered.wait(5)
            routes = [
                ('status', {}), ('robots.list', {}), ('env.list', {}),
                ('env.get', {'id': 'test-room'}), ('env.thumbnail', {'id': 'test-room'}),
                ('env.scene', {'id': 'test-room'}), ('env.preview', {'id': 'test-room', 'robot': 'dog'}),
                ('session.list', {}), ('session.get', {'session': sid}),
                ('session.log', {'session': sid}), ('session.image', {'session': sid, 'path': image}),
                ('attempt.frames', {'session': sid, 'attempt': 1}),
                ('skills.list', {'session': sid}), ('skills.read', {'session': sid, 'name': 'move'}),
                ('settings.get', {}),
            ]
            for method, params in routes:
                assert isinstance(rt.dispatch(method, params), dict), method
            assert rt.dispatch('session.scene', {'session': sid}) == scene
            assert rt.dispatch('mesh.get', {'key': key})['faces'] == 4
            state = rt.dispatch('robot.state', {'session': sid})
            assert state['attempt']['status'] == 'running'
            assert state['attempt']['sim_time'] == pytest.approx(.1)
            state['state']['x'] = 999
            assert rt.dispatch('robot.state', {'session': sid})['state']['x'] != 999
            with pytest.raises(RPCError, match='already running'):
                rt.dispatch('robot.act', {'session': sid, 'tool': 'push', 'args': {'dx': 0}})
            assert not action.done()
        finally:
            release.set()
        result = action.result(timeout=5)
        assert result['ok']
    assert rt.dispatch('robot.state', {'session': sid})['state'] == result['state']
    assert result['state']['x'] > .19
    rt.close()


def test_scene_cache_survives_restart_and_backfills_old_recordings(tmp_path, environment, fake_modules, monkeypatch):
    rt = Runtime(tmp_path / 'data', tmp_path / 'skills')
    rt.environments.save(environment)
    sid = rt.dispatch('session.start', {'robot': 'dog', 'environment': 'test-room', 'task': 'Move',
                      'goal': {'type': 'robot_near', 'target': {'point': [2, 0]}, 'radius': .1}})['session']['id']
    path = rt.sessions.store.session_dir(sid) / 'scene.json'
    assert path.is_file()
    expected = rt.dispatch('session.scene', {'session': sid})
    rt.close()
    restarted = Runtime(tmp_path / 'data', tmp_path / 'skills')
    from robotlab.core import session
    build = session.build
    monkeypatch.setattr(session, 'build', lambda *_: pytest.fail('cached scene rebuilt'))
    assert restarted.dispatch('session.scene', {'session': sid}) == expected
    path.unlink()
    monkeypatch.setattr(session, 'build', build)
    assert restarted.dispatch('session.scene', {'session': sid}) == expected
    assert path.is_file()
    restarted.close()
