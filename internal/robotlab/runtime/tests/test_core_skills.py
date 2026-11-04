from pathlib import Path

from robotlab.core.skills import Skills
from tests.test_core_sessions import manager, start

GOOD = 'def run(robot, **args):\n    return robot.push(dx=.02)\n'
BAD = 'def run(robot, **args):\n    return robot.push(dx=0, fail=True)\n'


def test_skill_regression_accept_reject_and_copy(tmp_path, environment, fake_modules):
    s = manager(tmp_path, environment)
    skills = Skills(s, tmp_path / 'skills')
    sid = start(s)
    s.act(sid, 'push', {'dx': 0, 'fail': True})
    first = skills.write(sid, 'move', 'Move safely', 'Use a short push', code=GOOD, failures=[1])
    assert first['accepted']
    assert first['regression']['total'] == 1
    assert first['regression']['new_passed'] == 1
    folder = skills.folder(sid, 'move')
    assert (folder / 'fallos' / f'{sid}-1/image.png').is_file()
    assert (folder / 'SKILL.md').read_text().startswith('---\nname: "move"')
    second = skills.write(sid, 'move', 'Unsafe change', 'Too much', code=BAD)
    assert not second['accepted']
    assert second['regression']['old_passed'] == 1
    assert second['regression']['new_passed'] == 0
    assert skills.read(sid, 'move')['version'] == 1
    assert skills.read(sid, 'move')['versions'][-1]['accepted'] is False
    assert (folder / 'skill.py').read_text() == GOOD
    third = skills.write(sid, 'move', 'Safe change', 'Try again', code=GOOD)
    assert third['accepted'] and third['skill']['version'] == 3
    s.reset(sid, 'Use the safe skill')
    assert skills.run(sid, 'move')['ok']
    s.stop(sid, 'agent')
    assert skills.read(sid, 'move')['uses'] == 1
    assert skills.read(sid, 'move')['success_rate'] == 0
    next_session = start(s)
    assert skills.copy(sid, next_session) == {'copied': ['move']}
    assert skills.read(next_session, 'move')['code'] == GOOD
    assert s.get(next_session)['skill_events'][-1]['kind'] == 'copied'


def test_skill_success_accounted_once_and_version_pinned(tmp_path, environment, fake_modules):
    s = manager(tmp_path, environment)
    skills = Skills(s, tmp_path / 'skills')
    sid = start(s)
    skills.write(sid, 'move', 'Move', 'Move', code='def run(robot):\n    return robot.push(dx=1.2)')
    result = skills.run(sid, 'move')
    assert result['goal']['met']
    assert result['skill'] == 'move'
    assert skills.read(sid, 'move')['success_rate'] == 1
    skills.attempt_end(s.get(sid), s.get(sid)['attempts'][0])
    assert skills.read(sid, 'move')['uses'] == 1


def test_start_copies_skills_and_skill_errors_capture_evidence(tmp_path, environment, fake_modules):
    s = manager(tmp_path, environment)
    skills = Skills(s, tmp_path / 'skills')
    sid = start(s)
    skills.write(sid, 'broken', 'Broken', 'Demonstrate a runtime error', code='def run(robot):\n    return 1 / 0')
    result = skills.run(sid, 'broken')
    assert result['attempt']['status'] == 'failed'
    assert (s.store.session_dir(sid) / 'attempts/1/failure/code.py').read_text().startswith('def run')
    s.stop(sid, 'agent')
    copied = s.start('dog', 'test-room', 'Try a copy', {'type': 'robot_near', 'target': {'point': [1, 0]}, 'radius': .1}, copy_skills_from=sid)
    assert copied['skills'][0]['name'] == 'broken'


def test_version_accounting_survives_stop_during_regression(tmp_path, environment, fake_modules, monkeypatch):
    import threading
    from robotlab.core import skillrun
    s = manager(tmp_path, environment)
    skills = Skills(s, tmp_path / 'skills')
    sid = start(s)
    s.act(sid, 'push', {'dx': 0, 'fail': True})
    skills.write(sid, 'move', 'Move', 'Move', code=GOOD, failures=[1])
    s.reset(sid, 'Use the skill')
    skills.use(sid, 'move')
    entered, finish = threading.Event(), threading.Event()
    def regression(*_):
        entered.set()
        assert finish.wait(3)
        return {'passed': True, 'detail': 'fake regression'}
    monkeypatch.setattr(skillrun, 'regression', regression)
    results, errors = [], []
    def write():
        try:
            results.append(skills.write(sid, 'move', 'Improved', 'Improved', code=GOOD))
        except Exception as exc:
            errors.append(exc)
    worker = threading.Thread(target=write)
    worker.start()
    try:
        assert entered.wait(3)
        s.stop(sid, 'user')
        assert s.get(sid)['status'] == 'stopped'
    finally:
        finish.set()
        worker.join(3)
    assert not worker.is_alive()
    assert errors == []
    previous = skills.read(sid, 'move')['versions'][0]
    assert previous['uses'] == 1
    assert previous['success_rate'] == 0
