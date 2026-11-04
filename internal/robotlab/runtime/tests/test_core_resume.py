import copy

import pytest

from robotlab.core.skills import Skills
from robotlab.protocol import RPCError
from robotlab.server import Runtime
from tests.test_core_sessions import manager, start


@pytest.mark.parametrize('status', ['stopped', 'succeeded', 'exhausted', 'failed'])
def test_resume_preserves_recordings_and_skills(tmp_path, environment, fake_modules, status):
    events = []
    s = manager(tmp_path, environment)
    s.event = lambda name, data: events.append((name, data))
    skills = Skills(s, tmp_path / 'skills')
    s.settings.set({'max_attempts': 2})
    sid = start(s)
    skills.write(sid, 'move', 'Move safely', 'Use the saved keyframes.')
    skills.use(sid, 'move')
    s.act(sid, 'push', {'dx': .1})
    s.reset(sid, 'Repeat carefully', no_skill_reason='Same controlled test')
    s.act(sid, 'push', {'dx': .1})
    s.stop(sid, 'agent')
    saved = s.get(sid)
    saved['status'] = status
    s.store.save(saved)
    old_attempts = copy.deepcopy(saved['attempts'])
    old_frames = [s.frames(sid, n) for n in (1, 2)]
    scene = (s.store.session_dir(sid) / 'scene.json').read_bytes()
    s.environments.save(dict(environment, name='Changed room', spawn={'x': 2}))
    s.settings.set({'max_attempts': 4})
    resumed = s.resume(sid, reason='Retry the previous simulation', conversation='chat-new')
    session = resumed['session']
    assert session['id'] == sid and len(s.list()) == 1
    assert session['status'] == 'live' and session['ended_at'] is None and session['outcome'] is None
    assert session['conversation'] == 'chat-new'
    assert session['attempt'] == 3 and session['max_attempts'] == 6
    assert session['attempts'][:2] == old_attempts
    assert session['attempts'][-1]['status'] == 'running'
    assert session['attempts'][-1]['frames'] == 1
    assert [s.frames(sid, n) for n in (1, 2)] == old_frames
    assert s.frames(sid, 3)['total'] == 1
    assert (s.store.session_dir(sid) / 'scene.json').read_bytes() == scene
    assert s.world.env['name'] == environment['name']
    assert s.state(sid)['state']['x'] == 0
    assert resumed['card']['id'] == 'dog' and resumed['skills'][0]['name'] == 'move'
    assert 'skill_use' in resumed['next']
    assert skills.use(sid, 'move')['instructions'] == 'Use the saved keyframes.'
    assert s.get(sid)['attempts'][-1]['skills_used'] == ['move']
    note = next(data['entry'] for name, data in events if name == 'log'
                and data['entry']['summary'].startswith('Session resumed'))
    assert note['kind'] == 'system' and note['attempt'] == 3
    assert any(name == 'session' and data['session']['attempt'] == 3 for name, data in events)
    assert s.list()[0]['live']
    s.stop(sid, 'agent')
    assert s.resume(sid)['session']['attempt'] == 4
    assert s.get(sid)['max_attempts'] == 7


def test_resume_conflict_names_live_session_without_mutation(tmp_path, environment, fake_modules):
    s = manager(tmp_path, environment)
    old = start(s)
    s.stop(old, 'agent')
    live = start(s)
    before = s.get(old)
    with s.action():
        for sid in (old, live):
            with pytest.raises(RPCError, match=f'Live session {live}.*session_stop') as error:
                s.resume(sid)
            assert error.value.code == 'conflict'
    assert s.get(old) == before
    assert s.get(live)['attempt'] == 1


def test_resume_dispatch_after_restart_and_invalid_session(tmp_path, environment, fake_modules):
    rt = Runtime(tmp_path / 'data', tmp_path / 'skills')
    rt.dispatch('env.save', {'environment': environment})
    sid = rt.dispatch('session.start', {'robot': 'dog', 'environment': 'test-room', 'task': 'Move',
                     'goal': {'type': 'robot_near', 'target': {'point': [1, 0]}, 'radius': .1},
                     'conversation': 'original'})['session']['id']
    rt.dispatch('skills.write', {'session': sid, 'name': 'move', 'description': 'Move', 'instructions': 'Saved'})
    rt.close()
    restarted = Runtime(tmp_path / 'data', tmp_path / 'skills')
    result = restarted.dispatch('session.resume', {'session': sid, 'conversation': 'caller'})
    assert result['session']['attempt'] == 2
    assert result['session']['conversation'] == 'caller'
    assert result['skills'][0]['name'] == 'move'
    restarted.close()
    with pytest.raises(RPCError, match='not found'):
        restarted.dispatch('session.resume', {'session': 'missing'})
    with pytest.raises(RPCError, match='reason must be text'):
        restarted.dispatch('session.resume', {'session': sid, 'reason': 12})
    (restarted.sessions.store.session_dir(sid) / 'environment.json').unlink()
    with pytest.raises(RPCError, match='saved session environment'):
        restarted.dispatch('session.resume', {'session': sid})
    assert restarted.sessions.get(sid)['status'] == 'stopped'


def test_sessions_filters_are_compact_and_newest_first(tmp_path, environment, fake_modules):
    rt = Runtime(tmp_path / 'data', tmp_path / 'skills')
    rt.dispatch('env.save', {'environment': environment})
    def create(name, task):
        result = rt.dispatch('session.start', {'robot': 'dog', 'environment': 'test-room',
                             'name': name, 'task': task,
                             'goal': {'type': 'robot_near', 'target': {'point': [1, 0]}, 'radius': .1}})
        rt.close()
        return result['session']['id']
    older = create('Box and shelf', 'Place the box')
    newer = create('Stairs', 'Climb to landing')
    rows = rt.dispatch('session.list', {})['sessions']
    assert [s['id'] for s in rows] == [newer, older]
    assert all('attempt' not in s and 'skill_events' not in s and s['attempts'] == 1 for s in rows)
    for filters, ids in [({'robot': 'arm'}, []), ({'robot': 'dog'}, [newer, older]),
                         ({'environment': 'ROOM'}, [newer, older]), ({'environment': 'missing'}, []),
                         ({'query': 'SHELF'}, [older]), ({'query': 'landing'}, [newer]),
                         ({'query': 'test-ROOM'}, [newer, older]),
                         ({'robot': 'dog', 'query': 'box', 'environment': 'test'}, [older])]:
        assert [s['id'] for s in rt.dispatch('session.list', filters)['sessions']] == ids
