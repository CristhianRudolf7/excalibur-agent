import json
import threading
from concurrent.futures import ThreadPoolExecutor

from robotlab.core.skills import Skills
from tests.test_core_sessions import manager, start


def test_markdown_versions_failure_evidence_and_copy(tmp_path, environment, fake_modules):
    s = manager(tmp_path, environment)
    skills = Skills(s, tmp_path / 'skills')
    sid = start(s)
    s.act(sid, 'push', {'dx': 0, 'fail': True})
    first = skills.write(sid, 'move', 'Move safely', 'Use small keyframes.', failures=[1])
    assert first['accepted'] is True
    assert set(first) == {'skill', 'accepted'}
    folder = skills.folder(sid, 'move')
    assert (folder / 'fallos' / f'{sid}-1/image.png').is_file()
    initial = (folder / 'versions/v1/SKILL.md').read_text()
    assert initial.startswith('---\nname: "move"')
    second = skills.write(sid, 'move', 'Improve', 'Exact successful keyframes: []')
    assert second['accepted'] and second['skill']['version'] == 2
    assert all(v['accepted'] for v in second['skill']['versions'])
    assert (folder / 'versions/v1/SKILL.md').read_text() == initial
    assert {p.suffix for p in (folder / 'versions/v2').iterdir()} == {'.md', '.json'}
    s.reset(sid, 'Use updated instructions')
    skills.use(sid, 'move')
    s.stop(sid, 'agent')
    assert skills.read(sid, 'move')['uses'] == 1
    assert skills.read(sid, 'move')['success_rate'] == 0
    next_session = start(s)
    assert skills.copy(sid, next_session) == {'copied': ['move']}
    assert skills.read(next_session, 'move')['instructions'] == second['skill']['instructions']
    assert s.get(next_session)['skill_events'][-1]['kind'] == 'copied'


def test_success_accounting_is_idempotent_and_version_pinned(tmp_path, environment, fake_modules):
    s = manager(tmp_path, environment)
    skills = Skills(s, tmp_path / 'skills')
    sid = start(s)
    skills.write(sid, 'move', 'Move', 'First version')
    skills.use(sid, 'move')
    skills.write(sid, 'move', 'Move', 'Second version')
    assert s.act(sid, 'push', {'dx': 1.2})['goal']['met']
    skills.attempt_end(s.get(sid), s.get(sid)['attempts'][0])
    skill = skills.read(sid, 'move')
    assert skill['uses'] == 0
    assert skill['versions'][0]['uses'] == 1
    assert skill['versions'][0]['success_rate'] == 1


def test_start_copies_markdown_skills(tmp_path, environment, fake_modules):
    s = manager(tmp_path, environment)
    skills = Skills(s, tmp_path / 'skills')
    sid = start(s)
    skills.write(sid, 'move', 'Move', 'Reference keyframes')
    s.stop(sid, 'agent')
    copied = s.start('dog', 'test-room', 'Try a copy',
                     {'type': 'robot_near', 'target': {'point': [1, 0]}, 'radius': .1},
                     copy_skills_from=sid)
    assert copied['skills'][0]['name'] == 'move'


def test_stop_during_evidence_copy_preserves_accounting(tmp_path, environment, fake_modules, monkeypatch):
    from robotlab.core import skills as module
    s = manager(tmp_path, environment)
    skills = Skills(s, tmp_path / 'skills')
    sid = start(s)
    s.act(sid, 'push', {'dx': 0, 'fail': True})
    skills.write(sid, 'move', 'Move', 'First', failures=[1])
    s.reset(sid, 'Try again')
    skills.use(sid, 'move')
    entered, release = threading.Event(), threading.Event()
    original = module.shutil.copytree
    def copy(*args, **kwargs):
        entered.set()
        assert release.wait(5)
        return original(*args, **kwargs)
    monkeypatch.setattr(module.shutil, 'copytree', copy)
    with ThreadPoolExecutor(max_workers=1) as pool:
        writing = pool.submit(skills.write, sid, 'move', 'Move', 'Improved', failures=[1])
        try:
            assert entered.wait(5)
            s.stop(sid, 'user')
        finally:
            release.set()
        assert writing.result(timeout=5)['accepted']
    previous = skills.read(sid, 'move')['versions'][0]
    assert previous['uses'] == 1 and previous['success_rate'] == 0


def test_legacy_skill_metadata_is_projected_to_markdown_shape(tmp_path, environment, fake_modules):
    s = manager(tmp_path, environment)
    skills = Skills(s, tmp_path / 'skills')
    sid = start(s)
    original = skills.write(sid, 'move', 'Move', 'Instructions')['skill']
    folder = skills.folder(sid, 'move')
    raw = dict(original, legacy_payload='discard')
    raw['versions'][0]['legacy_payload'] = 'discard'
    (folder / 'meta.json').write_text(json.dumps(raw))
    (folder / 'skill.py').write_text('# Earlier executable attachment')
    assert 'legacy_payload' not in skills.read(sid, 'move')
    assert 'legacy_payload' not in skills.read(sid, 'move')['versions'][0]
    skills.write(sid, 'move', 'Improved', 'Updated markdown')
    assert not (folder / 'skill.py').exists()
    s.stop(sid, 'agent')
    target = start(s)
    (folder / 'versions/v1/skill.py').write_text('# Earlier version attachment')
    skills.copy(sid, target)
    assert not list(skills.folder(target, 'move').rglob('skill.py'))
