"""Failure evidence and the required reflect/write/reset training loop."""
import json
import threading
from concurrent.futures import ThreadPoolExecutor

import numpy as np
import pytest

from robotlab.core.skills import Skills
from robotlab.core.store import now
from robotlab.protocol import RPCError
from robotlab.server import Runtime
from tests.test_core_sessions import manager, start


MESSAGE = ('Before the next attempt, write or improve a skill from this failure: '
           'skill_write(name, description, instructions, failures=[N]). '
           'Pass no_skill_reason only if nothing new was learned.')


def assert_gate(s, sid):
    with pytest.raises(RPCError) as error:
        s.reset(sid, 'Reflect on the failed attempt')
    assert error.value.code == 'conflict'
    assert str(error.value) == MESSAGE


def evidence(s, sid, n=1):
    folder = s.store.session_dir(sid) / f'attempts/{n}/failure'
    assert {p.name for p in folder.iterdir()} == {'state.json', 'image.png', 'image_fall.png', 'code.py'}
    for image in ('image.png', 'image_fall.png'):
        assert (folder / image).read_bytes().startswith(b'\x89PNG')
    state = json.loads((folder / 'state.json').read_text())
    assert state['time'] == pytest.approx(max(0, state['final']['time'] - 1.5), abs=.026)
    assert state['env']['id'] == 'test-room'
    assert state['robot'] == 'dog'
    return folder, state


def test_failed_reset_requires_learning_and_override_is_per_attempt(tmp_path, environment, fake_modules):
    rt = Runtime(tmp_path / 'data', tmp_path / 'skills')
    rt.environments.save(environment)
    started = rt.dispatch('session.start', {'robot': 'dog', 'environment': 'test-room', 'task': 'Move',
                          'goal': {'type': 'robot_near', 'target': {'point': [2, 0]}, 'radius': .1}})
    sid = started['session']['id']
    assert 'skill_use' in started['next'] and 'before acting' in started['next']
    failed = rt.dispatch('robot.act', {'session': sid, 'tool': 'push', 'args': {'dx': 0, 'fail': True}})
    assert failed['next'] == ('Attempt 1 failed (fell). Reflect, write or improve a markdown skill with the lesson and exact keyframes that worked using '
                              'skill_write(..., failures=[1]), then attempt_reset.')
    s = rt.sessions
    assert_gate(s, sid)
    assert s.get(sid)['attempt'] == 1
    assert s.store.load(sid)['attempts'][0]['status'] == 'failed'
    result = rt.dispatch('attempt.reset', {'session': sid, 'reflection': 'Retry unchanged',
                                         'no_skill_reason': 'Nothing new was learned from this repeated setup.'})
    assert result['attempt'] == 2
    assert any(e['kind'] == 'reflection' and e['attempt'] == 1 and
               'Nothing new was learned' in e['summary'] for e in s.log(sid)['entries'])
    s.act(sid, 'push', {'dx': 0, 'unsafe': True})
    assert_gate(s, sid)
    rt.dispatch('skills.write', {'session': sid, 'name': 'safe-move', 'description': 'Move safely',
                                'instructions': 'Avoid unsafe moves.', 'failures': [2]})
    assert rt.dispatch('attempt.reset', {'session': sid, 'reflection': 'Use the new instructions'})['attempt'] == 3
    rt.close()


@pytest.mark.parametrize('reason', ['', '   ', 1, False, [], {}])
def test_invalid_override_does_not_end_attempt(tmp_path, environment, fake_modules, reason):
    s = manager(tmp_path, environment)
    sid = start(s)
    with pytest.raises(RPCError, match='no_skill_reason') as error:
        s.reset(sid, 'Try again', no_skill_reason=reason)
    assert error.value.code == 'invalid_params'
    assert s.get(sid)['attempts'][0]['status'] == 'running'


def test_abandoned_attempt_keeps_prefailure_evidence_for_skill(tmp_path, environment, fake_modules):
    s = manager(tmp_path, environment)
    skills = Skills(s, tmp_path / 'skills')
    sid = start(s)
    args = {'dx': .1, 'duration': 3}
    s.act(sid, 'push', args)
    final = s.world.data.qpos.copy()
    assert_gate(s, sid)
    assert s.get(sid)['attempts'][0]['status'] == 'abandoned'
    assert s.store.load(sid)['attempts'][0]['status'] == 'abandoned'
    folder, snapshot = evidence(s, sid)
    assert snapshot['last_call'] == {'tool': 'push', 'args': args}
    assert 'robot.push' in (folder / 'code.py').read_text()
    np.testing.assert_array_equal(s.world.data.qpos, final)
    result = skills.write(sid, 'short-move', 'Move briefly', 'Use a shorter move.',
                          failures=[1])
    assert result['accepted'] and result['skill']['failures'] == 1
    copied = skills.folder(sid, 'short-move') / 'fallos' / f'{sid}-1'
    assert json.loads((copied / 'state.json').read_text()) == snapshot
    assert s.reset(sid, 'Use shorter moves')['attempt'] == 2


def test_abandon_before_first_action_and_limit_exhaustion(tmp_path, environment, fake_modules):
    s = manager(tmp_path, environment)
    s.settings.set({'max_attempts': 2})
    sid = start(s)
    assert s.reset(sid, 'Retry setup', no_skill_reason='No action has run yet')['attempt'] == 2
    folder, snapshot = evidence(s, sid)
    assert snapshot['last_call'] == {'tool': None, 'args': {}}
    assert snapshot['time'] == 0
    assert (folder / 'code.py').read_text().startswith('# No tool or skill')
    with pytest.raises(RPCError, match='session exhausted'):
        s.reset(sid, 'No more attempts')
    assert s.get(sid)['status'] == 'exhausted'
    evidence(s, sid, 2)




@pytest.mark.parametrize('kind,allowed', [('created', True), ('improved', True), ('rejected', False),
                                          ('used', False), ('copied', False)])
@pytest.mark.parametrize('after_end', [False, True])
def test_only_learning_events_satisfy_gate(tmp_path, environment, fake_modules, kind, allowed, after_end):
    s = manager(tmp_path, environment)
    sid = start(s)
    event = {'t': now(), 'attempt': 1, 'kind': kind, 'skill': 'move', 'version': 1}
    if not after_end:
        s.live['skill_events'].append(event)
    s.act(sid, 'push', {'dx': 0, 'fail': True})
    if after_end:
        s.live['skill_events'].append(dict(event, t=now()))
    if allowed:
        assert s.reset(sid, 'Apply what was learned')['attempt'] == 2
    else:
        assert_gate(s, sid)


def test_older_learning_event_does_not_unlock_new_attempt(tmp_path, environment, fake_modules):
    s = manager(tmp_path, environment)
    skills = Skills(s, tmp_path / 'skills')
    sid = start(s)
    skills.write(sid, 'move', 'Move', 'Move slowly.')
    assert s.reset(sid, 'Change the setup')['attempt'] == 2
    skills.use(sid, 'move')
    s.act(sid, 'push', {'dx': 0, 'fail': True})
    assert_gate(s, sid)
    skills.write(sid, 'move', 'Move', 'Avoid the observed failure.', failures=[2])
    assert s.live['skill_events'][-1]['kind'] == 'improved'
    assert s.reset(sid, 'Use the improvement')['attempt'] == 3




def test_stop_during_abandonment_evidence_prevents_new_attempt(tmp_path, environment, fake_modules, monkeypatch):
    s = manager(tmp_path, environment)
    sid = start(s)
    entered, release = threading.Event(), threading.Event()
    capture = s._failure

    def failure(*args):
        entered.set()
        assert release.wait(5)
        capture(*args)

    monkeypatch.setattr(s, '_failure', failure)
    with ThreadPoolExecutor(max_workers=1) as pool:
        reset = pool.submit(s.reset, sid, 'Try again', no_skill_reason='Nothing new was learned')
        try:
            assert entered.wait(5)
            s.stop(sid, 'user')
        finally:
            release.set()
        with pytest.raises(RPCError, match='session is not live'):
            reset.result(timeout=5)
    assert s.get(sid)['status'] == 'stopped'
    assert s.store.load(sid)['status'] == 'stopped'
    assert len(s.get(sid)['attempts']) == 1


def test_direct_action_timeout_keeps_attempt_running(tmp_path, environment, fake_modules):
    s = manager(tmp_path, environment)
    sid = start(s)
    result = s.act(sid, 'push', {'dx': 0, 'duration': 33})
    assert result['ok'] is False
    assert result['attempt'] == {'n': 1, 'status': 'running', 'reason': ''}
    assert 'next' not in result
    recorded = s.frames(sid, 1)
    last = s.frames(sid, 1, start=recorded['total'] - 1, count=1)['frames'][0]
    assert last['status'] == 'running'
    follow = s.act(sid, 'push', {'dx': 0, 'duration': 1})
    assert follow['attempt']['status'] == 'running'
