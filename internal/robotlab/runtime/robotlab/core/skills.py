"""Session-local skills, immutable versions, failure evidence and accounting."""
from __future__ import annotations

import copy
from contextlib import nullcontext
import json
import os
import shutil
import threading
from pathlib import Path

from ..protocol import RPCError
from .store import identifier, now, read_json, write_json, slugify


_SKILL_FIELDS = ('name', 'description', 'version', 'success_rate', 'uses', 'successes',
                 'failures', 'updated_at', 'instructions', 'versions')
_VERSION_FIELDS = ('version', 'created_at', 'accepted', 'success_rate', 'uses', 'successes', 'accounted')


def _read(path):
    value = read_json(path)
    if value is None:
        return None
    skill = {key: value[key] for key in _SKILL_FIELDS if key in value}
    skill['versions'] = [{key: version[key] for key in _VERSION_FIELDS if key in version}
                         for version in value.get('versions', [])]
    return skill


def _markdown(skill):
    header = '\n'.join(f'{key}: {json.dumps(skill[key], ensure_ascii=False)}'
                       for key in ('name', 'description', 'version', 'updated_at'))
    return f'---\n{header}\n---\n\n{skill["instructions"]}\n'


def _files(folder, skill):
    folder.mkdir(parents=True, exist_ok=True)
    (folder / 'SKILL.md').write_text(_markdown(skill))
    (folder / 'skill.py').unlink(missing_ok=True)


class Skills:
    def __init__(self, sessions, root=None):
        self.sessions = sessions
        self.root = Path(root or os.environ.get('EXCALIBUR_ROBOT_SKILLS_DIR') or Path.home() / '.local/share/excalibur/robot-skills')
        self.root.mkdir(parents=True, exist_ok=True)
        self.lock = threading.RLock()
        sessions.attach_skills(self)

    def folder(self, session, name):
        return self.root / identifier(session) / identifier(name, 48)

    def read(self, session, name):
        self.sessions.get(session)
        with self.lock:
            skill = _read(self.folder(session, name) / 'meta.json')
        if skill is None:
            raise RPCError('not_found', 'skill not found')
        return skill

    @staticmethod
    def summary(skill):
        return {key: skill[key] for key in ('name', 'description', 'version', 'success_rate', 'uses', 'failures', 'updated_at')}

    def list(self, session):
        self.sessions.get(session)
        with self.lock:
            return [self.summary(_read(p)) for p in sorted((self.root / identifier(session)).glob('*/meta.json'))]

    def _event(self, session, kind, skill):
        with self.sessions.lock:
            s = self.sessions.get(session)
            entry = {'t': now(), 'attempt': s['attempt'], 'kind': kind, 'skill': skill['name'], 'version': skill['version']}
            if self.sessions.live and self.sessions.live['id'] == session:
                s = self.sessions.live
            s['skill_events'].append(entry)
            self.sessions._save(s)
            self.sessions.note(session, f'{kind}: {skill["name"]} v{skill["version"]}', 'skill')

    def write(self, session, name, description, instructions, failures=None):
        self.sessions.get(session)
        if isinstance(name, str) and name.strip():
            name = slugify(name, 48, 'skill')
        identifier(name, 48)
        if not isinstance(description, str) or not isinstance(instructions, str):
            raise RPCError('invalid_params', 'description and instructions must be text')
        failures = [] if failures is None else failures
        if not isinstance(failures, list) or any(type(n) is not int or n < 1 for n in failures):
            raise RPCError('invalid_params', 'failures must list attempt numbers')
        sources = []
        for n in failures:
            source = self.sessions.store.session_dir(session) / 'attempts' / str(n) / 'failure'
            if not (source / 'state.json').is_file():
                raise RPCError('not_found', f'failure evidence not found for attempt {n}. If attempt {n} is still running, '
                               'call attempt_reset with your reflection first: it records the evidence and then asks '
                               f'for this skill_write(..., failures=[{n}]) before starting the next attempt.')
            sources.append((n, source))
        with self.sessions.action():
            folder = self.folder(session, name)
            previous = _read(folder / 'meta.json')
            version = 1 + max((v['version'] for v in previous['versions']), default=0) if previous else 1
            for n, source in sources:
                target = folder / 'fallos' / f'{session}-{n}'
                shutil.copytree(source, target, dirs_exist_ok=True)
                write_json(target / 'meta.json', {'session': session, 'attempt': n})
            snapshots = sorted((folder / 'fallos').glob('*/state.json'))
            timestamp = now()
            version_meta = {'version': version, 'created_at': timestamp, 'accepted': True,
                            'success_rate': None, 'uses': 0, 'successes': 0, 'accounted': []}
            with self.sessions.lock, self.lock:
                # Stop may have finalized accounting while evidence was copied.
                previous = _read(folder / 'meta.json')
                candidate = {'name': name, 'description': description, 'version': version,
                             'success_rate': None, 'uses': 0, 'successes': 0,
                             'failures': len(snapshots), 'updated_at': timestamp,
                             'instructions': instructions,
                             'versions': copy.deepcopy(previous['versions']) if previous else []}
                candidate['versions'].append(version_meta)
                archive = folder / 'versions' / f'v{version}'
                _files(archive, candidate)
                write_json(archive / 'meta.json', version_meta)
                _files(folder, candidate)
                write_json(folder / 'meta.json', candidate)
                self._event(session, 'improved' if previous else 'created', candidate)
            return {'skill': candidate, 'accepted': True}

    def use(self, session, name, _skill=None):
        skill = _skill if _skill is not None else self.read(session, name)
        with self.sessions.lock:
            current = self.sessions._current(session, running=True)
            attempt = current['attempts'][-1]
            if name not in attempt['skills_used']:
                attempt['skills_used'].append(name)
            self._event(session, 'used', skill)
        return skill

    def attempt_end(self, session, attempt):
        with self.lock:
            for name in attempt['skills_used']:
                folder = self.folder(session['id'], name)
                skill = _read(folder / 'meta.json')
                if skill is None:
                    continue
                # Use events pin accounting to the version actually used.
                used_versions = {e['version'] for e in session['skill_events']
                                 if e['attempt'] == attempt['n'] and e['skill'] == name and e['kind'] == 'used'}
                for version in skill['versions']:
                    if version['version'] not in used_versions:
                        continue
                    key = f'{session["id"]}-{attempt["n"]}'
                    if key in version.get('accounted', []):
                        continue
                    version.setdefault('accounted', []).append(key)
                    version['uses'] += 1
                    version['successes'] = version.get('successes', 0) + (attempt['status'] == 'succeeded')
                    version['success_rate'] = version['successes'] / version['uses']
                    archive = folder / 'versions' / f'v{version["version"]}' / 'meta.json'
                    existing = read_json(archive, {})
                    write_json(archive, dict(existing, **version))
                    if version['version'] == skill['version']:
                        for field in ('uses', 'successes', 'success_rate'):
                            skill[field] = version[field]
                write_json(folder / 'meta.json', skill)

    def copy(self, source, target, _reserved=False):
        with nullcontext() if _reserved else self.sessions.action():
            return self._copy(source, target)

    def _copy(self, source, target):
        self.sessions.get(source)
        self.sessions.get(target)
        if source == target:
            raise RPCError('invalid_params', 'source and target sessions must differ')
        copied = []
        with self.sessions.lock, self.lock:
            for skill in self.list(source):
                name = skill['name']
                dest = self.folder(target, name)
                if dest.exists():
                    raise RPCError('conflict', f'target already has skill: {name}')
            for skill in self.list(source):
                name = skill['name']
                shutil.copytree(self.folder(source, name), self.folder(target, name),
                                ignore=shutil.ignore_patterns('skill.py'))
                copied.append(name)
        for name in copied:
            self._event(target, 'copied', self.read(target, name))
        return {'copied': copied}
