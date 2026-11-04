from copy import deepcopy

import pytest

from robotlab.world.schema import normalize, validate


def env_with(**fields):
    return {'id': 'test-room', 'objects': [{'id': 'ball', 'shape': 'sphere', 'size': [0.2], 'pos': [1, 1]}], **fields}


def test_normalize_is_independent_and_fills_defaults():
    env = env_with()
    original = deepcopy(env)
    result = normalize(env)
    assert env == original
    assert result['objects'][0]['pos'] == [1, 1, 0.1]
    assert result['objects'][0]['mass'] == pytest.approx(2.094395102)
    assert result['spawn'] == {'x': 0, 'y': 0, 'yaw': 0}
    assert normalize(result) == result
    table = normalize(env_with(kind='table', objects=[{'id': 'cube', 'shape': 'box', 'size': [.05] * 3, 'pos': [0, 0]}]))
    assert table['size'] == [1.2, .8]
    assert table['spawn']['x'] == -.75
    assert table['objects'][0]['pos'][2] == .775


@pytest.mark.parametrize('field,value,expected', [
    ('shape', 'pyramid', 'shape'), ('size', [-1], 'size'), ('size', [float('nan')], 'size'),
    ('pos', [9, 0], 'pos.x'), ('pos', [0], 'pos'), ('pos', [1, 1, -.1], 'pos.z'),
    ('color', '#xyz123', 'color'),
    ('mass', 0, 'mass'), ('yaw', float('inf'), 'yaw'), ('dynamic', 1, 'dynamic'),
    ('tags', 'tag', 'tags'), ('compound', True, 'compound'),
])
def test_object_errors_name_id_and_field(field, value, expected):
    env = env_with()
    env['objects'][0][field] = value
    assert any(f"object 'ball'.{expected}:" in err for err in validate(env))
    with pytest.raises(ValueError, match='ball'):
        normalize(env)


def test_limits_duplicates_rotation_and_fixed_spawn():
    env = env_with()
    obj = env['objects'][0]
    env['objects'] = [obj] * 201
    assert any('at most 200' in err for err in validate(env))
    assert any('unique' in err for err in validate(env))
    obj = {'id': 'desk', 'shape': 'box', 'size': [2, .5, .6], 'pos': [0, 0], 'dynamic': False, 'yaw': 90}
    errors = validate(env_with(objects=[obj]))
    assert any("object 'desk'.pos: spawn" in err for err in errors)
    obj['pos'] = [2, 0]
    assert validate(env_with(objects=[obj])) == []
    obj['yaw'] = 0
    assert any('pos.x' in err for err in validate(env_with(objects=[obj])))
    for size in ([.9, 4], [31, 4], [True, 4]):
        assert any('size:' in err for err in validate(env_with(size=size)))


def test_malformed_documents_return_errors_without_crashing():
    for env in (None, [], {'id': []}, env_with(objects=[None]), env_with(walls=[None]),
                env_with(spawn={'x': 'bad'}), env_with(source=[]), env_with(created_at=[]),
                env_with(kind=[]), env_with(objects=[{'id': [], 'shape': []}])):
        assert validate(env)
    assert validate(env_with(version=True))
    assert validate(env_with(spawn={'x': 10**400}))
    assert validate(env_with(created_at='2026-01-01T00:00:00Z')) == []


def test_walls_and_capsule_dimensions():
    env = env_with(walls=[{'from': [0, 0], 'to': [0, 0], 'height': -1, 'thickness': 0, 'color': 'red'}])
    errors = validate(env)
    for field in ('to', 'height', 'thickness', 'color'):
        assert any(f'walls[0].{field}:' in err for err in errors)
    assert validate(env_with(objects=[{'id': 'pill', 'shape': 'capsule', 'size': [.2, .1], 'pos': [1, 1]}]))


def test_explicit_table_height_cannot_put_objects_below_top():
    obj = {'id': 'cube', 'shape': 'box', 'size': [.05] * 3, 'pos': [0, 0, .7]}
    assert any("object 'cube'.pos.z:" in err for err in validate(env_with(kind='table', objects=[obj])))
    obj['pos'][2] = .775
    assert validate(env_with(kind='table', objects=[obj])) == []
