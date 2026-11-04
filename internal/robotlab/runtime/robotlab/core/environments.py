"""Environment JSON persistence and adapters to the shared world package."""
from __future__ import annotations

import base64
import shutil
import threading
from importlib import import_module
from pathlib import Path

from .. import robots
from ..assets import menagerie_dir
from ..protocol import RPCError
from .store import identifier, now, read_json, write_json, slugify

_THUMBNAIL_VERSION = 'lighting-v2'


def world_module(name):
    try:
        return import_module('robotlab.world.' + name)
    except ImportError as exc:
        raise RPCError('unavailable', f'world {name} is unavailable') from exc


def build(env, robot=None):
    return world_module('builder').build_world(env, robot, menagerie_dir())


class Environments:
    def __init__(self, root, in_use=lambda id: False, defaults=None):
        self.root = Path(root) / 'environments'
        self.root.mkdir(parents=True, exist_ok=True)
        self.in_use = in_use
        self.lock = threading.RLock()
        defaults = Path(defaults) if defaults else Path(__file__).resolve().parents[1] / 'world' / 'defaults'
        sources = list(defaults.glob('*.json'))
        marker = self.root / '.defaults-v3'
        if sources and not marker.exists():
            for source in sources:
                target = self.root / source.name
                if not target.exists():
                    shutil.copyfile(source, target)
            marker.touch()

    def list(self):
        with self.lock:
            return [{'id': e['id'], 'name': e['name'], 'kind': e['kind'], 'objects': len(e.get('objects', [])),
                     'updated_at': e.get('updated_at'), 'source_type': 'text'}
                    for p in sorted(self.root.glob('*.json')) if (e := read_json(p))]

    def get(self, id):
        with self.lock:
            env = read_json(self.root / (identifier(id, 64) + '.json'))
        if env is None:
            raise RPCError('not_found', 'environment not found')
        return world_module('schema').normalize(env)

    def save(self, environment, create=False):
        if not isinstance(environment, dict) or type(create) is not bool:
            raise RPCError('invalid_params', 'expected environment object and create boolean')
        if create and 'id' not in environment and isinstance(environment.get('name'), str):
            environment = dict(environment, id=slugify(environment['name'], 64, 'environment'))
        schema = world_module('schema')
        errors = schema.validate(environment)
        if errors:
            raise RPCError('invalid_params', '; '.join(errors))
        env = schema.normalize(environment)
        id = identifier(env.get('id'), 64)
        with self.lock:
            path = self.root / (id + '.json')
            previous = read_json(path)
            if create and previous:
                raise RPCError('conflict', 'environment already exists')
            env['created_at'] = previous.get('created_at', now()) if previous else env.get('created_at', now())
            env['updated_at'] = now()
            write_json(path, env)
            (self.root / (id + '.png')).unlink(missing_ok=True)
            (self.root / f'{id}.{_THUMBNAIL_VERSION}.png').unlink(missing_ok=True)
        return env

    def delete(self, id):
        with self.lock:
            self.get(id)
            if self.in_use(id):
                raise RPCError('conflict', 'environment is used by the live session')
            (self.root / (id + '.json')).unlink()
            (self.root / (id + '.png')).unlink(missing_ok=True)
            (self.root / f'{id}.{_THUMBNAIL_VERSION}.png').unlink(missing_ok=True)

    def scene(self, id):
        return build(self.get(id)).scene()

    def preview(self, id, robot=None, cached=False):
        with self.lock:
            env = self.get(id)
            path = self.root / f'{id}.{_THUMBNAIL_VERSION}.png'
            if cached and path.exists():
                return base64.b64encode(path.read_bytes()).decode()
            world = build(env, robots.make(robot) if robot else None)
            png = world_module('thumbnails').render_overview(world, width=640, height=400)
            if cached:
                path.write_bytes(png)
            return base64.b64encode(png).decode()
