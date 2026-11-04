import threading
import time

import pytest
from robotlab.core.environments import Environments
from robotlab.core.session import Sessions
from robotlab.core.settings import Settings
from robotlab.protocol import RPCError


def manager(tmp_path, environment):
    settings = Settings(tmp_path)
    environments = Environments(tmp_path)
    environments.save(environment)
    return Sessions(tmp_path, settings, environments)


def start(s, goal=None):
    return s.start('dog', 'test-room', 'Move', goal or {'type': 'robot_near', 'target': {'point': [1, 0]}, 'radius': .1})['session']['id']


def test_lifecycle_and_replay(tmp_path, environment, fake_modules):
    s = manager(tmp_path, environment)
    sid = start(s)
    with pytest.raises(RPCError, match='live session'):
        start(s)
    result = s.act(sid, 'push', {'dx': 1})
    assert result['goal']['met'], result
    assert result['attempt']['status'] == 'succeeded'
    assert s.get(sid)['status'] == 'succeeded'
    assert s.list()[0]['live'] is False
    frames = s.frames(sid, 1)
    assert 18 <= frames['total'] <= 20
    assert frames['frames'][0]['seq'] == 1
    assert s.log(sid)['next'] == 2
    assert s.store.load(sid)['status'] == 'succeeded'
    with pytest.raises(RPCError):
        s.act(sid, 'push', {'dx': 1})


def test_failure_reset_limit_and_restart(tmp_path, environment, fake_modules):
    s = manager(tmp_path, environment)
    s.settings.set({'max_attempts': 2})
    sid = start(s)
    result = s.act(sid, 'push', {'dx': 0, 'fail': True})
    assert result['attempt']['status'] == 'failed'
    folder = s.store.session_dir(sid) / 'attempts/1/failure'
    assert {p.name for p in folder.iterdir()} == {'state.json', 'image.png', 'image_fall.png', 'code.py'}
    with pytest.raises(RPCError, match='attempt 1 ended'):
        s.act(sid, 'push', {'dx': 1})
    assert s.reset(sid, 'Try again', no_skill_reason='Repeat the same controlled failure test')['attempt'] == 2
    s.act(sid, 'push', {'dx': 0, 'unsafe': True})
    with pytest.raises(RPCError, match='exhausted'):
        s.reset(sid, 'No attempts remain')
    assert s.get(sid)['status'] == 'exhausted'
    sid = start(s)
    restarted = manager(tmp_path, environment)
    assert restarted.get(sid)['status'] == 'stopped'
    assert restarted.get(sid)['attempts'][0]['reason'] == 'runtime restarted'


def test_stop_interrupts_and_reads_stay_responsive(tmp_path, environment, fake_modules, monkeypatch):
    monkeypatch.delenv('EXCALIBUR_ROBOTLAB_FAST')
    s = manager(tmp_path, environment)
    sid = start(s)
    result, errors = [], []
    def act():
        try:
            result.append(s.act(sid, 'push', {'dx': 0, 'duration': 30}))
        except Exception as e:
            errors.append(e)
    worker = threading.Thread(target=act)
    worker.start()
    try:
        deadline = time.monotonic() + 2
        while not s.action_lock.locked() and time.monotonic() < deadline:
            time.sleep(.001)
        with pytest.raises(RPCError, match='already running'):
            s.act(sid, 'push', {'dx': 0})
        assert s.get(sid)['status'] == 'live'
        s.frames(sid, 1)
        s.log(sid)
        s.stop(sid, 'user')
    finally:
        worker.join(timeout=3)
    assert not worker.is_alive()
    assert errors == []
    assert result[0]['attempt']['status'] == 'stopped'
    assert s.get(sid)['status'] == 'stopped'


def test_look_logs_image_and_invalid_args(tmp_path, environment, fake_modules):
    s = manager(tmp_path, environment)
    sid = start(s)
    assert s.look(sid)['visible']
    entry = s.log(sid)['entries'][-1]
    assert (s.store.session_dir(sid) / entry['image']).exists()
    with pytest.raises(RPCError, match='required'):
        s.act(sid, 'push', {})
    assert s.get(sid)['attempts'][0]['status'] == 'running'


def test_restart_recovers_inflight_recording(tmp_path, environment, fake_modules):
    s = manager(tmp_path, environment)
    sid = start(s)
    # Simulate a crash before the action completion persisted session metadata.
    s.ctx.step(.5)
    assert s.store.load(sid)['attempts'][0]['frames'] == 0
    restarted = manager(tmp_path, environment)
    recovered = restarted.get(sid)['attempts'][0]
    assert recovered['status'] == 'stopped'
    assert recovered['frames'] == 10
    assert recovered['sim_time'] == .5
