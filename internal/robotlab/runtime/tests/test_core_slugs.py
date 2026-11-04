import re

import pytest

from robotlab.core.environments import Environments
from robotlab.core.skills import Skills
from robotlab.core.store import slugify
from tests.test_core_sessions import manager


@pytest.mark.parametrize('name, expected', [
    ('Habitación de prueba', 'habitacion-de-prueba'),
    ('Habitacio\u0301n de prueba', 'habitacion-de-prueba'),
    ('ÁÉÍÓÚ ü ñ Ç', 'aeiou-u-n-c'),
    ('  Already-a-slug! ', 'already-a-slug'),
    ('!!!', 'fallback'),
])
def test_accented_slugs(name, expected):
    assert slugify(name, 64, 'fallback') == expected


def test_session_and_skill_names(tmp_path, environment, fake_modules):
    sessions = manager(tmp_path, environment)
    sid = sessions.start('dog', 'test-room', 'Move',
                         {'type': 'robot_near', 'target': {'point': [1, 0]}, 'radius': .1},
                         name='Habitación de prueba')['session']['id']
    assert re.fullmatch('habitacion-de-prueba-[a-f0-9]{4}', sid)
    skills = Skills(sessions, tmp_path / 'skills')
    saved = skills.write(sid, 'Caminar en habitación', 'Walk', 'Walk safely')['skill']
    assert saved['name'] == 'caminar-en-habitacion'
    assert skills.read(sid, saved['name']) == saved
    assert skills.folder(sid, saved['name']).is_dir()


def test_environment_name_generates_id_without_mutating_input(tmp_path, environment):
    environment.pop('id')
    environment['name'] = 'Habitación de prueba'
    envs = Environments(tmp_path)
    saved = envs.save(environment, create=True)
    assert saved['id'] == 'habitacion-de-prueba'
    assert envs.get(saved['id']) == saved
    assert 'id' not in environment


@pytest.mark.parametrize('limit', [48, 60, 64])
def test_slug_limits_and_fallback(limit):
    result = slugify('á ' * 100, limit, 'fallback')
    assert re.fullmatch('[a-z0-9-]{1,' + str(limit) + '}', result)
    assert not result.endswith('-')
