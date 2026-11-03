"""Physics, protocol and headless image integration coverage."""
import base64
import io
import json
import os
from pathlib import Path
import queue
import signal
import subprocess
import sys
import threading
import time

from PIL import Image
import pytest
from tests.test_core_fixture import Tiny
from robotlab.protocol import MAX_LINE, RPCError, Writer, read_line
from robotlab.scene import export_scene, body_poses
from robotlab.sessions import Sessions


def wait_for(predicate, timeout=5):
    """Wait for observable work without assuming timely thread scheduling."""
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(.01)
    pytest.fail('session did not make progress before deadline')


@pytest.fixture
def child_env(monkeypatch):
    root = Path(__file__).resolve().parents[1]
    monkeypatch.setenv('PYTHONPATH', os.pathsep.join([str(root), str(root / 'tests')]))
    monkeypatch.setenv('EXCALIBUR_ROBOTLAB_SCENARIO_MODULES', 'tests.test_core_fixture')
    return dict(os.environ)


def test_protocol_bounds_and_thread_safe_writer():
    stream = io.BytesIO(b'{"x":1}\n' + b'x' * (MAX_LINE + 2) + b'\n{"next":true}\n')
    assert read_line(stream) == {'x': 1}
    with pytest.raises(RPCError, match='8 MiB'):
        read_line(stream)
    assert read_line(stream) == {'next': True}
    assert read_line(stream) is None
    for raw in (b'[]\n', b'bad\n', b'\xff\n'):
        with pytest.raises(RPCError):
            read_line(io.BytesIO(raw))
    output = io.StringIO()
    writer = Writer(output)
    threads = [threading.Thread(target=lambda: [writer.event('test', {'n': n}) for n in range(30)]) for _ in range(4)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()
    assert len([json.loads(line) for line in output.getvalue().splitlines()]) == 120


def test_scene_and_marks():
    from robotlab.marks import snapshot
    env = Tiny().make_env()
    env.reset(seed=1)
    try:
        scene = export_scene(env)
        assert scene['scenario'] == Tiny.id
        assert len(scene['bodies']) == 3
        assert scene['bodies'][1]['parent'] == 0
        assert scene['geoms'][0]['size'][0] >= 5
        assert scene['actuators'][0]['name'] == 'motor'
        assert len(body_poses(env)[0]) == 7
        plain = snapshot(env, camera='front', width=320, height=240)
        marked = snapshot(env, camera='front', width=320, height=240, marks=True)
        assert len(marked['marks']) == 2
        assert marked['image_png_b64'] != plain['image_png_b64']
        image = Image.open(io.BytesIO(base64.b64decode(marked['image_png_b64'])))
        assert image.size == (320, 240)
        for mark in marked['marks']:
            x0, y0, x1, y1 = mark['bbox']
            assert 0 <= x0 < x1 <= 320
            assert 0 <= y0 < y1 <= 240
            assert len(mark['world']) == 3
        with pytest.raises(ValueError, match='camera'):
            snapshot(env, camera='missing')
        with pytest.raises(ValueError):
            snapshot(env, width=99999)
    finally:
        env.close()


def test_session_timing_decay_pause_reset_and_limit(tmp_path):
    events = []
    manager = Sessions(lambda name, data: events.append((time.monotonic(), name, data)), tmp_path)
    try:
        opened = manager.open(Tiny.id)
        session = manager.get(opened['session'])
        started = time.monotonic()
        session.control('manual', {'motor': 1})
        with session.lock:
            assert session._action(session.last_input + .1)[0] == 1
            assert session._action(session.last_input + .51)[0] == 0
        time.sleep(.55)
        wait_for(lambda: session.step >= 40)
        with session.lock:
            assert session.env.data.ctrl[0] == 0
            # Loaded hosts may run slower; physics must never run ahead of real time.
            assert session.step <= (time.monotonic() - started) / session.env.dt + 4
            assert session.episode >= 1
        frames = [event for event in events if event[1] == 'frame']
        assert len(frames) >= 2
        assert all(b[0] - a[0] >= 1 / 30 - 0.002 for a, b in zip(frames, frames[1:]))
        session.control('paused')
        count = session.step
        time.sleep(0.08)
        assert session.step == count
        assert session.reset(seed=3)['scene']['scenario'] == Tiny.id
        assert session.set_params({'timestep': 0.02})['scene']['timestep'] == 0.02
        others = [manager.open(Tiny.id)['session'] for _ in range(3)]
        manager.items[others[0]].last_control = time.monotonic() - 300
        fifth = manager.open(Tiny.id)['session']
        assert len(manager.items) == 4 and others[0] not in manager.items and fifth in manager.items
        session.last_control = time.monotonic() - 601
        assert session.stop.wait(2)
        assert manager.open(Tiny.id)['session']
    finally:
        manager.shutdown()
    assert not any(thread.name.startswith('ses_') for thread in threading.enumerate())


def test_skill_frames_cancel_and_pose(tmp_path):
    events = []
    manager = Sessions(lambda name, data: events.append((name, data)), tmp_path)
    try:
        session = manager.get(manager.open(Tiny.id)['session'])
        result = session.skill('home', {'steps': 8})
        assert result == {'ok': True, 'detail': 'skill complete', 'frames': 8}
        assert any(name == 'frame' and data['mode'] == 'skill' for name, data in events)
        result = session.skill('home', {'steps': 1000, 'timeout': 0.05})
        assert result['ok'] is False and 'timeout' in result['detail']
        def pose_applied():
            session.control('pose', pose={'value': 0.5})
            with session.lock:
                return session.env.data.ctrl[0] == 0.5
        wait_for(pose_applied)
    finally:
        manager.shutdown()


@pytest.mark.parametrize('terminate', [False, True])
def test_server_round_trip_and_shutdown(tmp_path, child_env, terminate):
    process = subprocess.Popen([sys.executable, '-m', 'robotlab.server', '--data', str(tmp_path)],
                               stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=child_env)
    messages = queue.Queue()
    def read():
        for line in process.stdout:
            messages.put(json.loads(line))
    reader = threading.Thread(target=read, daemon=True)
    reader.start()
    try:
        ready = messages.get(timeout=8)
        assert ready['event'] == 'ready' and ready['data']['scenarios'] >= 1
        def call(number, method, params=None):
            process.stdin.write(json.dumps({'id': number, 'method': method, 'params': params or {}}).encode() + b'\n')
            process.stdin.flush()
            end = time.monotonic() + 4
            while time.monotonic() < end:
                message = messages.get(timeout=4)
                if message.get('id') == number:
                    return message
            pytest.fail('no response')
        assert call(1, 'ping')['result'] == {'ok': True}
        assert call(2, 'bad')['error']['code'] == 'invalid_params'
        assert call(3, 'scenarios.list')['result']['scenarios'][0]['id'] == Tiny.id
        opened = call(4, 'session.open', {'scenario': Tiny.id})['result']
        assert opened['scene']['geoms']
        assert call(5, 'session.control', {'session': opened['session'], 'mode': 'manual', 'inputs': {'motor': 0.3}})['result']['mode'] == 'manual'
        session_params = {'session': opened['session']}
        default = call(6, 'teleop.get_mapping', session_params)['result']
        assert default['source'] == 'default' and 'def map_pose' in default['code']
        custom = 'def map_pose(pose, state):\n return {"motor": 0.25}\n'
        assert call(7, 'teleop.set_mapping', {**session_params, 'code': custom})['result']['ok']
        assert call(8, 'teleop.get_mapping', session_params)['result'] == {'source': 'custom', 'code': custom}
        assert call(9, 'teleop.set_mapping', session_params)['result']['ok']
        assert call(10, 'teleop.get_mapping', session_params)['result'] == default
        if terminate:
            process.send_signal(signal.SIGTERM)
        else:
            process.stdin.close()
        process.wait(timeout=5)
        assert process.returncode == 0, process.stderr.read().decode()
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        reader.join(timeout=2)
        process.stdout.close()
        process.stderr.close()


def test_server_switch_and_dispatch_surface(tmp_path, monkeypatch):
    from robotlab.server import Server
    server = Server(tmp_path, lambda *args: None)
    try:
        assert set(server.methods) == set("""ping scenarios.list session.open session.close
            session.reset session.set_params session.control session.snapshot session.skill
            session.load_policy experiment.create experiment.list experiment.get
            experiment.write_code experiment.start experiment.stop experiment.metrics
            teleop.set_mapping teleop.get_mapping""".split())
        monkeypatch.setenv('EXCALIBUR_ROBOTLAB', 'off')
        assert server.call('ping', {}) == {'ok': True}
        with pytest.raises(RPCError) as error:
            server.call('scenarios.list', {})
        assert error.value.code == 'unavailable'
        monkeypatch.delenv('EXCALIBUR_ROBOTLAB')
        assert server.call('scenarios.list', {})['scenarios']
        with pytest.raises(RPCError) as error:
            server.call('ping', [])
        assert error.value.code == 'invalid_params'
        with pytest.raises(RPCError) as error:
            server.call('session.close', {'session': 'missing'})
        assert error.value.code == 'not_found'
    finally:
        server.close()


def test_mapping_source_hook_and_failed_edit(tmp_path, monkeypatch):
    from robotlab.scenario import Scenario

    assert Scenario().pose_mapping_source() is None
    source = 'def map_pose(pose, state):\n return {"motor": pose.get("value", 0)}\n'
    monkeypatch.setattr(Tiny, 'pose_mapping_source', lambda self: source)
    manager = Sessions(lambda *args: None, tmp_path)
    try:
        session = manager.get(manager.open(Tiny.id)['session'])
        assert session.get_mapping() == {'source': 'default', 'code': source}
        assert session.set_mapping('broken (')['ok'] is False
        assert session.get_mapping() == {'source': 'default', 'code': source}
        assert session.set_mapping(None) == {'ok': True}
        assert session.get_mapping()['code'] == source
    finally:
        manager.shutdown()
