"""Multiplexed JSON-lines sidecar for deterministic robot training sessions."""
from __future__ import annotations

import argparse
import json
import logging
import os
import sys
import threading
from concurrent.futures import ThreadPoolExecutor

from . import __version__, assets, robots
from .protocol import RPCError, Writer, read_line
from .core.environments import Environments, world_module
from .core.session import Sessions
from .core.settings import Settings


class Runtime:
    def __init__(self, data, skills=None, event=lambda *_: None):
        assets.set_data_dir(data)
        self.settings = Settings(data)
        self.environments = Environments(data)
        self.sessions = Sessions(data, self.settings, self.environments, event)
        self._data, self._skills_root = data, skills
        self._skills = self._media = None
        self._lazy_lock = threading.RLock()
        self.environments.in_use = lambda id: bool(self.sessions.live and self.sessions.live['status'] == 'live'
                                                  and self.sessions.live['environment'] == id)

    @property
    def skills(self):
        with self._lazy_lock:
            if self._skills is None:
                from .core.skills import Skills
                self._skills = Skills(self.sessions, self._skills_root)
            return self._skills

    @property
    def media(self):
        with self._lazy_lock:
            if self._media is None:
                from .core.media import Media
                self._media = Media(self._data)
            return self._media

    def _start(self, **params):
        # Attach accounting before an attempt can use or copy skills.
        self.skills
        return self.sessions.start(**params)

    def status(self):
        session = self.sessions.live
        return {'version': __version__, 'live_session': session['id'] if session and session['status'] == 'live' else None,
                'robots': len(robots.ROBOTS), 'environments': len(self.environments.list())}

    def dispatch(self, method, params=None):
        if not isinstance(method, str) or not isinstance(params, dict):
            raise RPCError('invalid_params', 'method must be text and params must be an object')
        p = dict(params)
        # Explicit routing keeps the public API independent of implementation names.
        routes = {
            'status': lambda: self.status(),
            'robots.list': lambda: {'robots': robots.cards()},
            'env.list': lambda: {'environments': self.environments.list()},
            'env.get': lambda id: {'environment': self.environments.get(id)},
            'env.save': lambda environment, create=False: {'environment': self.environments.save(environment, create)},
            'env.delete': self._delete_environment,
            'env.thumbnail': lambda id: {'png': self.environments.preview(id, cached=True)},
            'env.scene': lambda id: {'scene': self.environments.scene(id)},
            'env.preview': lambda id, robot=None: {'png': self.environments.preview(id, robot)},
            'media.ingest': lambda **p: self.media.ingest(**p),
            'media.get': lambda **p: self.media.get(**p),
            'settings.get': lambda: self.settings.get(),
            'settings.set': lambda **fields: self.settings.set(fields),
            'session.start': self._start,
            'session.list': lambda: {'sessions': self.sessions.list()},
            'session.get': lambda session: {'session': self.sessions.get(session)},
            'session.stop': self.sessions.stop,
            'session.scene': lambda session: {'scene': self.sessions.scene(session)},
            'session.log': self.sessions.log,
            'session.image': self.sessions.image,
            'attempt.reset': self.sessions.reset,
            'attempt.frames': self._frames,
            'robot.act': self.sessions.act,
            'robot.look': self.sessions.look,
            'robot.state': self.sessions.state,
            'skills.list': lambda session: {'skills': self.skills.list(session)},
            'skills.read': lambda session, name: {'skill': self.skills.read(session, name)},
            'skills.write': lambda **p: self.skills.write(**p),
            'skills.use': lambda session, name: {'skill': self.skills.use(session, name)},
            'skills.run': lambda **p: self.skills.run(**p),
            'skills.copy': self._copy_skills,
            'log.note': self._note,
            'mesh.get': self._mesh,
        }
        if method not in routes:
            raise RPCError('not_found', f'unknown method: {method}')
        try:
            return routes[method](**p)
        except TypeError as exc:
            raise RPCError('invalid_params', str(exc)) from exc

    def _delete_environment(self, id):
        self.environments.delete(id)
        return {}

    def _frames(self, session, attempt, count=400, **fields):
        if set(fields) - {'from'}:
            raise RPCError('invalid_params', 'unknown frame parameter')
        return self.sessions.frames(session, attempt, fields.get('from', 0), count)

    def _copy_skills(self, to, **fields):
        if set(fields) != {'from'}:
            raise RPCError('invalid_params', 'skills.copy requires from and to')
        return self.skills.copy(fields['from'], to)

    def _note(self, session, text, kind='note'):
        if kind not in ('note', 'plan', 'reflection'):
            raise RPCError('invalid_params', 'invalid note kind')
        return {'entry': self.sessions.note(session, text, kind)}

    @staticmethod
    def _mesh(key):
        blob = world_module('meshes').mesh_blob(key)
        return json.loads(blob) if isinstance(blob, (bytes, str)) else blob

    def close(self):
        live = self.sessions.live
        if live and live['status'] == 'live':
            self.sessions.stop(live['id'], 'agent', 'runtime shutting down')


def serve(runtime, writer, input_stream):
    """Read continuously while actions and regression runs execute off-thread."""
    workers = ThreadPoolExecutor(max_workers=4, thread_name_prefix='robot-action')
    slots = threading.BoundedSemaphore(8)
    long_methods = {'robot.act', 'skills.run', 'skills.write'}

    def respond(request):
        id = request.get('id')
        try:
            result = runtime.dispatch(request.get('method'), request.get('params', {}))
            writer.send({'id': id, 'result': result})
        except RPCError as exc:
            writer.send({'id': id, 'error': {'code': exc.code, 'message': str(exc)}})
        except Exception:
            logging.exception('Runtime request failed')
            writer.send({'id': id, 'error': {'code': 'internal', 'message': 'runtime request failed'}})

    def work(request):
        try:
            respond(request)
        finally:
            slots.release()

    writer.event('ready', {'version': __version__})
    try:
        while True:
            try:
                request = read_line(input_stream)
            except RPCError as exc:
                writer.send({'id': None, 'error': {'code': exc.code, 'message': str(exc)}})
                continue
            if request is None:
                break
            if isinstance(request.get('method'), str) and request['method'] in long_methods:
                if slots.acquire(blocking=False):
                    workers.submit(work, request)
                else:
                    writer.send({'id': request.get('id'), 'error': {'code': 'conflict', 'message': 'runtime is busy'}})
            else:
                respond(request)
    finally:
        runtime.close()
        workers.shutdown(wait=True, cancel_futures=True)


def main(argv=None):
    parser = argparse.ArgumentParser(description='Robot Lab runtime')
    parser.add_argument('--data', required=True)
    parser.add_argument('--skills')
    args = parser.parse_args(argv)
    # Native libraries can write directly to fd 1. Reserve a duplicate for the
    # protocol and route all other Python/native stdout to diagnostics.
    protocol_output = os.fdopen(os.dup(sys.stdout.fileno()), 'w', buffering=1)
    os.dup2(sys.stderr.fileno(), sys.stdout.fileno())
    sys.stdout = sys.stderr
    with protocol_output:
        writer = Writer(protocol_output)
        runtime = Runtime(args.data, args.skills, writer.event)
        serve(runtime, writer, sys.stdin.buffer)


if __name__ == '__main__':
    main()
