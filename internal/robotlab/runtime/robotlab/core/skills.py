"""Session-local skills, immutable versions, failure regression and accounting."""
from __future__ import annotations

import copy
from contextlib import nullcontext
import hashlib
import json
import os
import shutil
import threading
from pathlib import Path

from ..protocol import RPCError
from . import skillrun
from .store import identifier, now, read_json, write_json, slugify


def _markdown(skill):
    header = '\n'.join(f'{key}: {json.dumps(skill[key], ensure_ascii=False)}'
                       for key in ('name', 'description', 'version', 'updated_at'))
    return f'---\n{header}\n---\n\n{skill["instructions"]}\n'


def _files(folder, skill):
    folder.mkdir(parents=True, exist_ok=True)
    (folder / 'SKILL.md').write_text(_markdown(skill))
    if skill['code'] is not None:
        (folder / 'skill.py').write_text(skill['code'])
    else:
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
            skill = read_json(self.folder(session, name) / 'meta.json')
        if skill is None:
            raise RPCError('not_found', 'skill not found')
        return skill

    @staticmethod
    def summary(skill):
        return {key: skill[key] for key in ('name', 'description', 'version', 'success_rate', 'uses', 'has_code', 'failures', 'updated_at')}

    def list(self, session):
        self.sessions.get(session)
        with self.lock:
            return [self.summary(read_json(p)) for p in sorted((self.root / identifier(session)).glob('*/meta.json'))]

    def _event(self, session, kind, skill):
        with self.sessions.lock:
            s = self.sessions.get(session)
            entry = {'t': now(), 'attempt': s['attempt'], 'kind': kind, 'skill': skill['name'], 'version': skill['version']}
            if self.sessions.live and self.sessions.live['id'] == session:
                s = self.sessions.live
            s['skill_events'].append(entry)
            self.sessions._save(s)
            self.sessions.note(session, f'{kind}: {skill["name"]} v{skill["version"]}', 'skill')

    def write(self, session, name, description, instructions, code=None, failures=None):
        self.sessions.get(session)
        if isinstance(name, str) and name.strip():
            name = slugify(name, 48, 'skill')
        identifier(name, 48)
        if not isinstance(description, str) or not isinstance(instructions, str):
            raise RPCError('invalid_params', 'description and instructions must be text')
        if code is not None:
            errors = skillrun.validate(code)
            if errors:
                raise RPCError('invalid_params', '; '.join(errors))
        failures = [] if failures is None else failures
        if not isinstance(failures, list) or any(type(n) is not int or n < 1 for n in failures):
            raise RPCError('invalid_params', 'failures must list attempt numbers')
        sources = []
        for n in failures:
            source = self.sessions.store.session_dir(session) / 'attempts' / str(n) / 'failure'
            if not (source / 'state.json').is_file():
                raise RPCError('not_found', f'failure evidence not found for attempt {n}')
            sources.append((n, source))
        with self.sessions.action():
            folder = self.folder(session, name)
            previous = read_json(folder / 'meta.json')
            version = 1 + max((v['version'] for v in previous['versions']), default=0) if previous else 1
            for n, source in sources:
                target = folder / 'fallos' / f'{session}-{n}'
                shutil.copytree(source, target, dirs_exist_ok=True)
                write_json(target / 'meta.json', {'session': session, 'attempt': n})
            snapshots = sorted((folder / 'fallos').glob('*/state.json'))
            regression = self._regression(folder, previous, code, snapshots)
            accepted = regression['new_passed'] >= regression['old_passed']
            timestamp = now()
            version_meta = {'version': version, 'created_at': timestamp, 'accepted': accepted,
                            'success_rate': None, 'uses': 0, 'successes': 0, 'regression': regression,
                            'accounted': []}
            candidate = {'name': name, 'description': description, 'version': version, 'success_rate': None,
                         'uses': 0, 'successes': 0, 'has_code': code is not None, 'failures': len(snapshots),
                         'updated_at': timestamp, 'instructions': instructions, 'code': code,
                         'versions': copy.deepcopy(previous['versions']) if previous else []}
            candidate['versions'].append(version_meta)
            archive = folder / 'versions' / f'v{version}'
            _files(archive, candidate)
            write_json(archive / 'meta.json', version_meta)
            if accepted:
                _files(folder, candidate)
                current = candidate
            else:
                current = previous
                current['versions'].append(version_meta)
                current['failures'] = len(snapshots)
            with self.lock:
                # A concurrent stop may have finalized uses while regression ran.
                for v in current['versions']:
                    archived = read_json(folder / 'versions' / f'v{v["version"]}' / 'meta.json', {})
                    for field in ('uses', 'successes', 'success_rate', 'accounted'):
                        if field in archived:
                            v[field] = archived[field]
                    if v['version'] == current['version']:
                        for field in ('uses', 'successes', 'success_rate'):
                            current[field] = v.get(field, 0 if field != 'success_rate' else None)
                write_json(folder / 'meta.json', current)
            self._event(session, ('improved' if previous else 'created') if accepted else 'rejected', candidate)
            return {'skill': current, 'accepted': accepted, 'regression': regression}

    def _regression(self, folder, previous, code, snapshots):
        details = []
        if not code or not snapshots:
            return {'old_passed': 0, 'new_passed': 0, 'total': 0, 'details': []}
        fingerprint = hashlib.sha256(b''.join(p.read_bytes() for p in snapshots)).hexdigest()
        old_results = None
        if previous and previous.get('code'):
            meta_path = folder / 'versions' / f'v{previous["version"]}' / 'meta.json'
            old_meta = read_json(meta_path, {})
            cached = old_meta.get('regression_cache')
            if cached and cached['fingerprint'] == fingerprint:
                old_results = cached['results']
            else:
                old_results = [skillrun.regression(previous['code'], read_json(p)) for p in snapshots]
                with self.lock:
                    old_meta = read_json(meta_path, {})
                    old_meta['regression_cache'] = {'fingerprint': fingerprint, 'results': old_results}
                    write_json(meta_path, old_meta)
        old_results = old_results or [{'passed': False, 'detail': 'no previous code'} for _ in snapshots]
        new_results = [skillrun.regression(code, read_json(p)) for p in snapshots]
        for p, old, new in zip(snapshots, old_results, new_results):
            details.append({'failure': p.parent.name, 'old': old, 'new': new})
        return {'old_passed': sum(r['passed'] for r in old_results), 'new_passed': sum(r['passed'] for r in new_results),
                'total': len(snapshots), 'details': details}

    def use(self, session, name, _skill=None):
        skill = _skill if _skill is not None else self.read(session, name)
        with self.sessions.lock:
            current = self.sessions._current(session, running=True)
            attempt = current['attempts'][-1]
            if name not in attempt['skills_used']:
                attempt['skills_used'].append(name)
            self._event(session, 'used', skill)
        return skill

    def run(self, session, name, args=None):
        skill = self.read(session, name)
        if not skill['code']:
            raise RPCError('invalid_params', 'skill has no code')
        if args is not None and not isinstance(args, dict):
            raise RPCError('invalid_params', 'skill args must be an object')
        def execute():
            proxy = skillrun.RobotProxy(self.sessions.ctx, self.sessions.call_tool, self.sessions._look,
                                       lambda text: self.sessions.note(session, text, 'note'))
            returned = skillrun.run(skill['code'], proxy, args, stop=self.sessions.stop_flag)
            self.sessions.ctx.check()
            return returned
        return self.sessions.execute(session, execute, {'skill': name, 'args': args or {}}, code=skill['code'],
                                     skill=name, on_start=lambda: self.use(session, name, _skill=skill))

    def attempt_end(self, session, attempt):
        with self.lock:
            for name in attempt['skills_used']:
                folder = self.folder(session['id'], name)
                skill = read_json(folder / 'meta.json')
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
        with self.lock:
            for skill in self.list(source):
                name = skill['name']
                dest = self.folder(target, name)
                if dest.exists():
                    raise RPCError('conflict', f'target already has skill: {name}')
            for skill in self.list(source):
                name = skill['name']
                shutil.copytree(self.folder(source, name), self.folder(target, name))
                copied.append(name)
        for name in copied:
            self._event(target, 'copied', self.read(target, name))
        return {'copied': copied}
