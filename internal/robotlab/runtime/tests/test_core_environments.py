import sys
from types import ModuleType

import pytest
from robotlab.core.environments import Environments
from robotlab.protocol import RPCError


def test_environment_crud_and_preview(tmp_path, fake_modules, environment, monkeypatch):
    thumbnails = ModuleType('robotlab.world.thumbnails')
    calls = []
    thumbnails.render_overview = lambda world, **kw: calls.append(kw) or b'png'
    monkeypatch.setitem(sys.modules, thumbnails.__name__, thumbnails)
    envs = Environments(tmp_path)
    defaults = envs.list()
    saved = envs.save(environment, create=True)
    # A thumbnail cached before the lighting change must not be served.
    (envs.root / 'test-room.png').write_bytes(b'old dark thumbnail')
    assert saved['updated_at']
    assert envs.get('test-room')['name'] == 'Test room'
    assert next(e for e in envs.list() if e['id'] == 'test-room')['objects'] == 1
    with pytest.raises(RPCError, match='already exists'):
        envs.save(environment, create=True)
    assert envs.preview('test-room', cached=True) == 'cG5n'
    assert envs.preview('test-room', cached=True) == 'cG5n'
    assert len(calls) == 1
    assert envs.scene('test-room')['version'] == 1
    envs.save(dict(environment, name='Changed'))
    envs.preview('test-room', cached=True)
    assert len(calls) == 2
    envs.in_use = lambda _: True
    with pytest.raises(RPCError, match='live session'):
        envs.delete('test-room')
    envs.in_use = lambda _: False
    envs.delete('test-room')
    assert envs.list() == defaults
    with pytest.raises(RPCError):
        envs.save({'kind': 'invalid'})


def test_defaults_copied_once_and_deletion_survives_restart(tmp_path, environment, fake_modules):
    import json
    defaults = tmp_path / 'defaults'
    defaults.mkdir()
    (defaults / 'test-room.json').write_text(json.dumps(environment))
    data = tmp_path / 'data'
    envs = Environments(data, defaults=defaults)
    assert len(envs.list()) == 1
    envs.save(dict(environment, name='Edited'))
    assert Environments(data, defaults=defaults).get('test-room')['name'] == 'Edited'
    envs.delete('test-room')
    assert Environments(data, defaults=defaults).list() == []


def test_new_builtins_install_alongside_existing_user_copies(tmp_path):
    import json
    root = tmp_path / 'environments'
    root.mkdir()
    (root / '.defaults-v1').touch()
    old = {'id': 'previous-room', 'name': 'My edited room', 'kind': 'room'}
    (root / 'previous-room.json').write_text(json.dumps(old))
    envs = Environments(tmp_path)
    assert {e['id'] for e in envs.list()} == {'previous-room', 'escalera-estrecha', 'estante-pequeno'}
    assert json.loads((root / 'previous-room.json').read_text()) == old
    envs.delete('escalera-estrecha')
    assert {e['id'] for e in Environments(tmp_path).list()} == {'previous-room', 'estante-pequeno'}


def test_legacy_source_loads_as_text_without_rewriting_copy(tmp_path, environment):
    import json
    envs = Environments(tmp_path)
    path = envs.root / 'test-room.json'
    legacy = dict(environment, source={'type': 'video', 'notes': 'Original description'})
    path.write_text(json.dumps(legacy))
    assert envs.get('test-room')['source'] == {'type': 'text', 'notes': 'Original description'}
    assert next(e for e in envs.list() if e['id'] == 'test-room')['source_type'] == 'text'
    assert json.loads(path.read_text()) == legacy
