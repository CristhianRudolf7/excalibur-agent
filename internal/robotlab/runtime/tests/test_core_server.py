import json
import os
import queue
import subprocess
import threading
import time
from pathlib import Path

import pytest
from robotlab.server import Runtime
from robotlab.protocol import RPCError, Writer, read_line
from io import BytesIO, StringIO


def test_module_startup_is_lazy_and_native_stdout_is_diagnostic(tmp_path):
    hook = tmp_path / 'hook'
    hook.mkdir()
    (hook / 'sitecustomize.py').write_text('''import atexit, os, sys
class NoNativeImports:
    def find_spec(self, fullname, path=None, target=None):
        if fullname.split('.')[0] in ('mujoco', 'numpy', 'cv2'):
            raise RuntimeError('eager native import: ' + fullname)
sys.meta_path.insert(0, NoNativeImports())
atexit.register(lambda: os.write(1, b'native diagnostic\\n'))
''')
    env = dict(os.environ, PYTHONPATH=os.pathsep.join([str(hook), str(Path.cwd())]))
    started = time.monotonic()
    result = subprocess.run([os.sys.executable, '-m', 'robotlab.server', '--data', str(tmp_path / 'data')],
                            input='{"id":1,"method":"status","params":{}}\n',
                            capture_output=True, text=True, env=env, timeout=10)
    assert result.returncode == 0, result.stderr
    lines = [json.loads(line) for line in result.stdout.splitlines()]
    assert lines[0]['event'] == 'ready'
    assert lines[1]['result']['environments'] == 2
    assert 'native diagnostic' in result.stderr
    assert time.monotonic() - started < 10


def test_runtime_routes(tmp_path, environment, fake_modules):
    rt = Runtime(tmp_path, tmp_path / 'skills')
    assert rt.dispatch('status', {})['live_session'] is None
    rt.dispatch('env.save', {'environment': environment, 'create': True})
    sid = rt.dispatch('session.start', {'robot': 'dog', 'environment': 'test-room', 'task': 'Move',
                     'goal': {'type': 'robot_near', 'target': {'point': [1, 0]}, 'radius': .1}})['session']['id']
    assert rt.dispatch('status', {})['live_session'] == sid
    assert rt.dispatch('robot.act', {'session': sid, 'tool': 'push', 'args': {'dx': 1.2}})['goal']['met']
    assert rt.dispatch('attempt.frames', {'session': sid, 'attempt': 1, 'from': 1, 'count': 2})['frames'][0]['seq'] == 2
    assert rt.dispatch('session.list', {})['sessions'][0]['status'] == 'succeeded'
    with pytest.raises(RPCError):
        rt.dispatch('session.start', {})
    with pytest.raises(RPCError):
        rt.dispatch('missing', {})


def test_protocol_framing():
    assert read_line(BytesIO(b'{"id":1}\n')) == {'id': 1}
    for bad in [b'[]\n', b'{"a":NaN}\n', b'!\n']:
        with pytest.raises(RPCError):
            read_line(BytesIO(bad))
    out = StringIO()
    Writer(out).event('frame', {'t': .05})
    assert json.loads(out.getvalue()) == {'event': 'frame', 'data': {'t': .05}}


def test_subprocess_smoke_and_responsiveness(tmp_path, environment):
    # Load only test-owned fakes before starting the actual module entry point.
    hook = tmp_path / 'hook'
    hook.mkdir()
    (hook / 'sitecustomize.py').write_text('from tests.fake_robot import install\ninstall()\n')
    env = dict(os.environ, PYTHONPATH=os.pathsep.join([str(hook), str(Path.cwd())]),
               EXCALIBUR_ROBOTLAB_FAST='0', EXCALIBUR_ROBOTLAB_MENAGERIE='/unused')
    data = tmp_path / 'data'
    (data / 'environments').mkdir(parents=True)
    (data / 'environments/test-room.json').write_text(json.dumps(environment))
    process = subprocess.Popen([os.sys.executable, '-m', 'robotlab.server', '--data', str(data), '--skills', str(tmp_path / 'skills')],
                               stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env)
    messages = queue.Queue()
    reader = threading.Thread(target=lambda: [messages.put(json.loads(line)) for line in process.stdout], daemon=True)
    reader.start()
    nextid = 0
    def send(method, params):
        nonlocal nextid
        nextid += 1
        process.stdin.write(json.dumps({'id': nextid, 'method': method, 'params': params}) + '\n')
        process.stdin.flush()
        return nextid
    buffered = []
    def receive(id=None, event=None, timeout=4):
        deadline = time.monotonic() + timeout
        while True:
            for i, msg in enumerate(buffered):
                if (id is not None and msg.get('id') == id) or (event and msg.get('event') == event):
                    return buffered.pop(i)
            buffered.append(messages.get(timeout=max(.01, deadline-time.monotonic())))
    try:
        # This test hook deliberately imports native physics before main;
        # real startup without it is checked independently above.
        assert receive(event='ready', timeout=20)['data']['version'] == '2.0.0'
        assert receive(id=send([], {}))['error']['code'] == 'invalid_params'
        assert receive(id=send('status', {}))['result']['live_session'] is None
        started = receive(id=send('session.start', {'robot': 'dog', 'environment': 'test-room', 'task': 'Move',
                            'goal': {'type': 'robot_near', 'target': {'point': [1, 0]}, 'radius': .1}}))
        sid = started['result']['session']['id']
        act = send('robot.act', {'session': sid, 'tool': 'push', 'args': {'dx': 0, 'duration': 30}})
        receive(event='frame')
        assert receive(id=send('robot.act', {'session': sid, 'tool': 'push', 'args': {'dx': 0}}))['error']['code'] == 'conflict'
        before = time.monotonic()
        for method, params in [('status', {}), ('session.get', {'session': sid}), ('session.log', {'session': sid}),
                               ('session.scene', {'session': sid}), ('robot.state', {'session': sid}),
                               ('attempt.frames', {'session': sid, 'attempt': 1, 'from': 0, 'count': 20})]:
            assert 'result' in receive(id=send(method, params), timeout=1)
        assert time.monotonic() - before < 1
        assert receive(id=send('session.stop', {'session': sid, 'by': 'user'}))['result']['session']['status'] == 'stopped'
        assert receive(id=act)['result']['attempt']['status'] == 'stopped'
    finally:
        process.stdin.close()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()
        reader.join(timeout=2)
        stderr = process.stderr.read()
        process.stdout.close()
        process.stderr.close()
    assert process.returncode == 0, stderr


def test_session_image_allowlist(tmp_path, environment, fake_modules):
    import base64

    rt = Runtime(tmp_path / 'data', tmp_path / 'skills')
    rt.dispatch('env.save', {'environment': environment, 'create': True})
    sid = rt.dispatch('session.start', {'robot': 'dog', 'environment': 'test-room', 'task': 'Observe',
                      'goal': {'type': 'robot_near', 'target': {'point': [2, 0]}, 'radius': .1}})['session']['id']
    try:
        observation = rt.dispatch('robot.look', {'session': sid})
        path = rt.dispatch('session.log', {'session': sid})['entries'][-1]['image']
        assert rt.dispatch('session.image', {'session': sid, 'path': path}) == {'png': observation['png']}
        rt.dispatch('robot.act', {'session': sid, 'tool': 'push', 'args': {'dx': 0, 'fail': True}})
        folder = rt.sessions.store.session_dir(sid)
        failure = 'attempts/1/failure/image.png'
        assert base64.b64decode(rt.dispatch('session.image', {'session': sid, 'path': failure})['png']) == (folder / failure).read_bytes()
        for bad in [None, 1, '', '/attempts/1/looks/1.png', '../image.png',
                    'attempts/1/looks/../failure/image.png', 'attempts/1/looks/a/b.png',
                    'attempts/0/looks/1.png', 'attempts/1/looks/a\\b.png',
                    'attempts/1/failure/state.json', 'attempts/1/failure/image_fall.png',
                    'attempts/1/looks/1.jpg']:
            with pytest.raises(RPCError) as error:
                rt.dispatch('session.image', {'session': sid, 'path': bad})
            assert error.value.code == 'invalid_params'
        with pytest.raises(RPCError) as error:
            rt.dispatch('session.image', {'session': sid, 'path': 'attempts/99/looks/1.png'})
        assert error.value.code == 'not_found'
        outside = tmp_path / 'private.png'
        outside.write_bytes(b'private')
        (folder / 'attempts/1/looks/link.png').symlink_to(outside)
        with pytest.raises(RPCError) as error:
            rt.dispatch('session.image', {'session': sid, 'path': 'attempts/1/looks/link.png'})
        assert error.value.code == 'invalid_params'
        with pytest.raises(RPCError) as error:
            rt.dispatch('session.image', {'session': 'missing', 'path': path})
        assert error.value.code == 'not_found'
        rt.dispatch('session.stop', {'session': sid, 'by': 'user'})
        assert rt.dispatch('session.image', {'session': sid, 'path': path})['png'] == observation['png']
    finally:
        rt.close()
