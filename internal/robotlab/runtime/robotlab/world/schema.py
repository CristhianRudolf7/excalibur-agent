"""Validation and defaults for version-one environment documents (meters/degrees)."""
from __future__ import annotations

from copy import deepcopy
import math
import re
from datetime import datetime

_SLUG = re.compile(r'[a-z0-9-]{1,64}\Z')
_COLOR = re.compile(r'#[0-9a-fA-F]{6}\Z')
_SHAPES = {'sphere': 1, 'box': 3, 'cylinder': 2, 'capsule': 2}


def _number(value):
    if not isinstance(value, (int, float)) or isinstance(value, bool):
        return False
    try:
        return math.isfinite(value)
    except OverflowError:
        return False


def _vector(value, lengths):
    return isinstance(value, list) and len(value) in lengths and all(_number(v) for v in value)


def _color(value):
    return isinstance(value, str) and _COLOR.fullmatch(value) is not None


def half_extents(obj):
    """Axis-aligned local half extents, with capsule height including both caps."""
    size, shape = obj['size'], obj['shape']
    if shape == 'box':
        return [s / 2 for s in size]
    if shape == 'sphere':
        return [size[0] / 2] * 3
    return [size[0] / 2, size[0] / 2, size[1] / 2]


def _volume(obj):
    size, shape = obj['size'], obj['shape']
    if shape == 'box':
        return math.prod(size)
    radius = size[0] / 2
    if shape == 'sphere':
        return 4 * math.pi * radius**3 / 3
    if shape == 'capsule':
        return math.pi * radius**2 * (size[1] - size[0]) + 4 * math.pi * radius**3 / 3
    return math.pi * radius**2 * size[1]


def validate(env: dict) -> list[str]:
    """Return all actionable field errors without mutating the input."""
    if not isinstance(env, dict):
        return ['environment: expected an object']
    errors = []

    def error(path, detail):
        errors.append(f'{path}: {detail}')

    if not _number(env.get('version', 1)) or env.get('version', 1) != 1:
        error('version', 'must be 1')
    if not isinstance(env.get('id'), str) or not _SLUG.fullmatch(env['id']):
        error('id', 'must match [a-z0-9-]{1,64}')
    for field in ('name', 'description'):
        if field in env and not isinstance(env[field], str):
            error(field, 'must be a string')
    kind = env.get('kind', 'room')
    if kind not in ('room', 'table'):
        error('kind', 'must be room or table')
    size = env.get('size', [1.2, 0.8] if kind == 'table' else [5, 4])
    size_ok = _vector(size, (2,))
    if not size_ok or any(s > 30 or (s < 1 if kind != 'table' else s <= 0) for s in size):
        error('size', 'must contain two sides in 1–30 m' if kind != 'table' else 'must contain two positive sides no greater than 30 m')
        size_ok = False
    height = env.get('wall_height', 2.5 if kind == 'room' else 0)
    if not _number(height) or height < 0:
        error('wall_height', 'must be a finite nonnegative height')
    for field in ('floor_color', 'wall_color'):
        if field in env and not _color(env[field]):
            error(field, 'must be #rrggbb')
    spawn = env.get('spawn', {})
    spawn_ok = isinstance(spawn, dict)
    if not spawn_ok:
        error('spawn', 'must be an object')
    else:
        spawn = {'x': -0.75 if kind == 'table' else 0, 'y': 0, 'yaw': 0, **spawn}
        for field in ('x', 'y', 'yaw'):
            if not _number(spawn[field]):
                error(f'spawn.{field}', 'must be a finite number')
                spawn_ok = False
        if spawn_ok and size_ok and kind == 'room':
            for axis, dim in zip(('x', 'y'), size):
                if abs(spawn[axis]) > dim / 2:
                    error(f'spawn.{axis}', 'must be inside the room')
    walls = env.get('walls', [])
    if not isinstance(walls, list):
        error('walls', 'must be a list')
    else:
        for i, wall in enumerate(walls):
            path = f'walls[{i}]'
            if not isinstance(wall, dict):
                error(path, 'must be an object')
                continue
            for field in ('from', 'to'):
                if not _vector(wall.get(field), (2,)):
                    error(f'{path}.{field}', 'must contain two finite coordinates')
            if _vector(wall.get('from'), (2,)) and wall.get('from') == wall.get('to'):
                error(f'{path}.to', 'must differ from from')
            for field, default in (('height', 2.5), ('thickness', 0.1)):
                value = wall.get(field, default)
                if not _number(value) or value <= 0:
                    error(f'{path}.{field}', 'must be a finite positive number')
            if 'color' in wall and not _color(wall['color']):
                error(f'{path}.color', 'must be #rrggbb')
    objects = env.get('objects', [])
    if not isinstance(objects, list):
        error('objects', 'must be a list')
        objects = []
    if len(objects) > 200:
        error('objects', 'must contain at most 200 objects')
    seen = set()
    for i, obj in enumerate(objects):
        if not isinstance(obj, dict):
            error(f'objects[{i}]', 'must be an object')
            continue
        oid = obj.get('id')
        path = f'object {oid!r}' if isinstance(oid, str) else f'objects[{i}]'
        if not isinstance(oid, str) or not _SLUG.fullmatch(oid):
            error(f'{path}.id', 'must match [a-z0-9-]{1,64}')
        elif oid in seen:
            error(f'{path}.id', 'must be unique')
        else:
            seen.add(oid)
        if 'name' in obj and not isinstance(obj['name'], str):
            error(f'{path}.name', 'must be a string')
        shape = obj.get('shape')
        shape_ok = isinstance(shape, str) and shape in _SHAPES
        if not shape_ok:
            error(f'{path}.shape', 'must be sphere, box, cylinder or capsule')
        dims = obj.get('size')
        dims_ok = shape_ok and _vector(dims, (_SHAPES[shape],)) and all(d > 0 for d in dims)
        if not dims_ok:
            error(f'{path}.size', 'must contain positive full dimensions: sphere [d], box [x,y,z], cylinder/capsule [d,h]')
        elif shape == 'capsule' and dims[1] < dims[0]:
            error(f'{path}.size[1]', 'capsule full height must be at least its diameter')
        pos = obj.get('pos')
        pos_ok = _vector(pos, (2, 3))
        if not pos_ok:
            error(f'{path}.pos', 'must contain [x,y] or [x,y,z] finite center coordinates')
        yaw = obj.get('yaw', 0)
        yaw_ok = _number(yaw)
        if not yaw_ok:
            error(f'{path}.yaw', 'must be finite degrees')
        if not _color(obj.get('color', '#e58d49')):
            error(f'{path}.color', 'must be #rrggbb')
        dynamic = obj.get('dynamic', True)
        if not isinstance(dynamic, bool):
            error(f'{path}.dynamic', 'must be a boolean')
        if 'mass' in obj and (not _number(obj['mass']) or obj['mass'] <= 0):
            error(f'{path}.mass', 'must be finite and positive (kg)')
        if not isinstance(obj.get('compound', False), bool) or (obj.get('compound') and shape != 'box'):
            error(f'{path}.compound', 'must be a boolean and is only supported for box')
        tags = obj.get('tags', [])
        if not isinstance(tags, list) or not all(isinstance(tag, str) for tag in tags):
            error(f'{path}.tags', 'must be a list of strings')
        if dims_ok and pos_ok and yaw_ok:
            hx, hy, hz = half_extents(obj)
            support = .75 if kind == 'table' else 0
            if len(pos) == 3 and pos[2] - hz < support - 1e-9:
                error(f'{path}.pos.z', 'object bottom must be on or above the table top' if kind == 'table' else 'object bottom must be on or above the floor')
            angle = math.radians(yaw)
            c, s = math.cos(angle), math.sin(angle)
            ex, ey = (hx, hy) if shape != 'box' else (abs(c) * hx + abs(s) * hy, abs(s) * hx + abs(c) * hy)
            if size_ok:
                for axis, extent, side, p in zip(('x', 'y'), (ex, ey), size, pos):
                    if abs(p) + extent > side / 2 + 1e-9:
                        error(f'{path}.pos.{axis}', 'object footprint must be inside the room' if kind != 'table' else 'object footprint must be on the table')
            if spawn_ok and dynamic is False:
                dx, dy = spawn['x'] - pos[0], spawn['y'] - pos[1]
                lx, ly = c * dx + s * dy, -s * dx + c * dy
                inside = abs(lx) < hx and abs(ly) < hy if shape == 'box' else dx * dx + dy * dy < hx * hx
                if inside:
                    error(f'{path}.pos', f'spawn is inside fixed object {oid!r}; move the object or spawn')
    source = env.get('source', {'type': 'text'})
    if not isinstance(source, dict):
        error('source', 'must be an object')
    else:
        if source.get('type', 'text') != 'text':
            error('source.type', 'must be text')
        for field in ('notes',):
            if field in source and not isinstance(source[field], str):
                error(f'source.{field}', 'must be a string')
    for field in ('created_at', 'updated_at'):
        if field in env:
            try:
                value = env[field]
                if not isinstance(value, str) or not re.fullmatch(r'\d{4}-\d\d-\d\d[Tt]\d\d:\d\d:\d\d(?:\.\d+)?(?:[Zz]|[+-]\d\d:\d\d)', value):
                    raise ValueError()
                datetime.fromisoformat(value.upper().replace('Z', '+00:00'))
            except ValueError:
                error(field, 'must be an RFC3339 timestamp with timezone')
    return errors


def normalize(env: dict) -> dict:
    """Validate, then return an independent document with all geometry defaults."""
    env = deepcopy(env)
    if isinstance(env, dict) and isinstance(env.get('source'), dict):
        if env['source'].get('type') in ('image', 'video'):
            env['source'] = {'type': 'text', 'notes': env['source'].get('notes', '')}
    errors = validate(env)
    if errors:
        raise ValueError('; '.join(errors))
    out = deepcopy(env)
    out.setdefault('version', 1)
    out.setdefault('name', out['id'])
    out.setdefault('kind', 'room')
    table = out['kind'] == 'table'
    for field, value in {'description': '', 'size': [1.2, 0.8] if table else [5, 4],
                         'wall_height': 0 if table else 2.5, 'floor_color': '#c8b89a',
                         'wall_color': '#e8e4dc', 'walls': [], 'objects': []}.items():
        out.setdefault(field, value)
    out['spawn'] = {'x': -0.75 if table else 0, 'y': 0, 'yaw': 0, **out.get('spawn', {})}
    out['source'] = {'type': 'text', 'notes': out.get('source', {}).get('notes', '')}
    for wall in out['walls']:
        for field, default in {'height': 2.5, 'thickness': 0.1, 'color': out['wall_color']}.items():
            wall.setdefault(field, default)
    for obj in out['objects']:
        for field, default in {'name': obj['id'], 'yaw': 0, 'color': '#e58d49', 'dynamic': True, 'tags': []}.items():
            obj.setdefault(field, default)
        if len(obj['pos']) == 2:
            obj['pos'].append((0.75 if table else 0) + half_extents(obj)[2])
        obj.setdefault('mass', _volume(obj) * 500)
    return out
