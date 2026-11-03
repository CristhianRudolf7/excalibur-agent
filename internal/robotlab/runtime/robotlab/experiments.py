"""Durable experiments with a two-process queue and bounded output streaming."""
from __future__ import annotations

import ast
from copy import deepcopy
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import subprocess
import threading
import time
import uuid

from . import registry, sandbox
from .algorithms import DEFAULTS
from .protocol import RPCError

DEFAULT_CODE = '''from robotlab import sdk
ctx = sdk.context()
env = sdk.make_env()
try:
    model = sdk.algorithm(ctx.algorithm, env, **ctx.hyperparams)
    model.learn(ctx.total_steps, callback=sdk.callback())
    sdk.save(model)
finally:
    env.close()
'''


def timestamp() -> str:
    """UTC timestamp for persistent experiment state."""
    return datetime.now(timezone.utc).isoformat()


class Experiments:
    """One supervisor owns child handles; all state changes hold the store lock."""
    def __init__(self, directory: Path, emit):
        self.directory = directory / 'experiments'
        self.directory.mkdir(parents=True, exist_ok=True)
        self.emit = emit
        self.lock = threading.RLock()
        self.items = {}
        self.active = {}
        self.offsets = {}
        self.stop_event = threading.Event()
        self.isolated = sandbox.available(self.directory)
        for path in sorted(self.directory.glob('exp_*/experiment.json')):
            try:
                experiment = json.loads(path.read_text())
                self.items[experiment['id']] = experiment
                if experiment['status'] == 'running':
                    experiment.update(status='failed', error='sidecar stopped during execution', ended_at=timestamp())
                    for run in experiment['runs']:
                        if run['status'] in {'running', 'queued'}:
                            run['status'] = 'failed'
                    self._persist(experiment)
                # Reconstruct consumed byte offsets from the durable merged journal.
                counts = {}
                journal = path.parent / 'metrics.jsonl'
                if journal.exists():
                    for line in journal.open():
                        point = json.loads(line)
                        counts[point['run']] = counts.get(point['run'], 0) + 1
                for run in experiment['runs']:
                    metric = path.parent / 'runs' / run['id'] / 'metrics.jsonl'
                    offset = 0
                    if metric.exists():
                        with metric.open('rb') as stream:
                            for _ in range(counts.get(run['id'], 0)):
                                stream.readline()
                            offset = stream.tell()
                    self.offsets[run['id']] = offset
            except (OSError, ValueError, KeyError):
                continue
        self.thread = threading.Thread(target=self._loop, name='robotlab-experiments', daemon=True)
        self.thread.start()

    def _path(self, experiment: dict) -> Path:
        return self.directory / experiment['id']

    def _persist(self, experiment: dict) -> None:
        directory = self._path(experiment)
        temporary = directory / 'experiment.json.tmp'
        temporary.write_text(json.dumps(experiment, allow_nan=False, indent=2))
        temporary.replace(directory / 'experiment.json')

    def _get(self, identifier: str) -> dict:
        try:
            return self.items[identifier]
        except KeyError:
            raise RPCError('not_found', 'experiment does not exist') from None

    def get(self, experiment: str) -> dict:
        with self.lock:
            value = deepcopy(self._get(experiment))
            value['code'] = (self._path(value) / 'experiment.py').read_text()
            return value

    def list(self) -> dict:
        with self.lock:
            return {'experiments': [deepcopy(value) for value in self.items.values()]}

    def create(self, name: str, scenario: str, runs: list, params=None, total_steps=None,
               reward_weights=None, code=None) -> dict:
        cls = registry.get(scenario)
        world = cls(params or {})
        if not isinstance(name, str) or not name.strip() or len(name) > 200:
            raise ValueError('name must contain 1..200 characters')
        if not isinstance(runs, list) or not 1 <= len(runs) <= 32:
            raise ValueError('an experiment needs 1..32 runs')
        steps = int(total_steps if total_steps is not None else cls.default_total_steps)
        if not 1 <= steps <= 1_000_000_000:
            raise ValueError('total_steps must be in [1,1000000000]')
        weights = {term.name: term.weight for term in cls.reward_terms}
        if set(reward_weights or {}) - set(weights):
            raise ValueError('unknown reward terms')
        weights.update(reward_weights or {})
        prepared = []
        for run in runs:
            algorithm = run['algorithm'].upper()
            if algorithm not in DEFAULTS:
                raise ValueError('unsupported algorithm')
            hp = dict(run.get('hyperparams') or {})
            if not 1 <= int(hp.get('cpu_seconds', 7200)) <= 86400:
                raise ValueError('cpu_seconds must be in [1,86400]')
            prepared.append({'id': 'run_' + uuid.uuid4().hex[:12], 'label': str(run['label'])[:200],
                             'algorithm': algorithm, 'hyperparams': hp, 'status': 'draft',
                             'steps': 0, 'wall_s': 0.0, 'checkpoints': []})
        code = DEFAULT_CODE if code is None else code
        ast.parse(code)
        value = {'id': 'exp_' + uuid.uuid4().hex[:12], 'name': name, 'scenario': scenario,
                 'params': world.params, 'status': 'draft', 'created_at': timestamp(),
                 'total_steps': steps, 'reward_weights': weights, 'runs': prepared,
                 'sandbox': 'bwrap' if self.isolated else 'rlimits'}
        if not self.isolated:
            value['warning'] = 'Filesystem and network isolation are unavailable; only resource limits are enforced.'
        json.dumps(value, allow_nan=False)
        with self.lock:
            directory = self._path(value)
            directory.mkdir()
            for run in prepared:
                run_dir = directory / 'runs' / run['id']
                run_dir.mkdir(parents=True)
                (run_dir / 'metrics.jsonl').touch()
                (run_dir / 'log.txt').touch()
                self.offsets[run['id']] = 0
            (directory / 'experiment.py').write_text(code)
            self.items[value['id']] = value
            self._persist(value)
            self.emit('experiment', self.get(value['id']))
            return self.get(value['id'])

    def write_code(self, experiment: str, code: str) -> dict:
        try:
            ast.parse(code)
        except SyntaxError as exc:
            return {'ok': False, 'errors': [f'line {exc.lineno}: {exc.msg}']}
        with self.lock:
            value = self._get(experiment)
            if value['status'] == 'running':
                raise RPCError('conflict', 'stop the experiment before editing code')
            (self._path(value) / 'experiment.py').write_text(code)
        return {'ok': True}

    def start(self, experiment: str) -> dict:
        with self.lock:
            value = self._get(experiment)
            if value['status'] == 'running':
                raise RPCError('conflict', 'experiment is already running')
            # Reruns have new run identities, preserving earlier artifacts.
            for run in value['runs']:
                if run['status'] != 'draft':
                    run['id'] = 'run_' + uuid.uuid4().hex[:12]
                    run.update(steps=0, wall_s=0.0, checkpoints=[])
                    run.pop('best_return', None)
                    run.pop('last_return', None)
                    path = self._path(value) / 'runs' / run['id']
                    path.mkdir(parents=True)
                    (path / 'metrics.jsonl').touch()
                    (path / 'log.txt').touch()
                    self.offsets[run['id']] = 0
                run['status'] = 'queued'
            value['sandbox'] = 'bwrap' if self.isolated else 'rlimits'
            if not self.isolated:
                value['warning'] = 'Filesystem and network isolation are unavailable; only resource limits are enforced.'
            else:
                value.pop('warning', None)
            value.update(status='running', started_at=timestamp())
            value.pop('error', None)
            value.pop('ended_at', None)
            self._persist(value)
            self.emit('experiment', self.get(experiment))
            return self.get(experiment)

    def stop(self, experiment: str) -> dict:
        with self.lock:
            value = self._get(experiment)
            if value['status'] != 'running':
                return self.get(experiment)
            points = []
            for run in value['runs']:
                active = self.active.pop(run['id'], None)
                if active:
                    sandbox.terminate(active['process'])
                    points.extend(self._collect(value, run, active, final=True))
                    self._close_active(active)
                if run['status'] in {'running', 'queued'}:
                    run['status'] = 'stopped'
            if points:
                self.emit('metrics', {'experiment': value['id'], 'points': points})
            value.update(status='stopped', ended_at=timestamp())
            self._persist(value)
            self.emit('experiment', self.get(experiment))
            return self.get(experiment)

    def metrics(self, experiment: str, run: str | None = None, since: int = 0) -> dict:
        if not isinstance(since, int) or since < 0:
            raise ValueError('since must be a nonnegative integer')
        with self.lock:
            value = self._get(experiment)
            path = self._path(value) / 'metrics.jsonl'
            points, cursor = [], 0
            if path.exists():
                with path.open() as stream:
                    for index, line in enumerate(stream):
                        cursor = index + 1
                        if index < since:
                            continue
                        point = json.loads(line)
                        if run is None or point['run'] == run:
                            points.append(point)
                        if len(points) >= 1000:
                            break
            return {'points': points, 'next': max(since, cursor)}

    def policy(self, experiment: str, run: str, checkpoint: str = 'best') -> tuple:
        if checkpoint not in {'best', 'last'}:
            raise ValueError('checkpoint must be best or last')
        with self.lock:
            value = self._get(experiment)
            selected = next((item for item in value['runs'] if item['id'] == run), None)
            if selected is None:
                raise RPCError('not_found', 'run does not exist')
            directory = self._path(value) / 'runs' / run
            path = directory / f'{checkpoint}.zip'
            if not path.exists():
                path = next((directory / name for name in ('policy.pkl', 'policy.pt')
                             if (directory / name).exists()), path)
            code = directory / 'policy.py'
            if not code.exists():
                code = self._path(value) / 'policy.py'
            return path, selected['algorithm'], code

    def _launch(self, value: dict, run: dict) -> None:
        directory = self._path(value)
        run_dir = directory / 'runs' / run['id']
        read_fd, write_fd = os.pipe()
        os.set_blocking(read_fd, False)
        os.set_blocking(write_fd, False)
        try:
            process = sandbox.launch('robotlab.sdk_runner', [str(directory), run['id']], directory,
                                     isolated=self.isolated,
                                     cpu_seconds=int(run['hyperparams'].get('cpu_seconds', 7200)),
                                     env={'EXCALIBUR_ROBOTLAB_PREVIEW_FD': str(write_fd)},
                                     pass_fds=(write_fd,),
                                     stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        except Exception:
            os.close(read_fd)
            raise
        finally:
            os.close(write_fd)
        os.set_blocking(process.stdout.fileno(), False)
        self.active[run['id']] = {'process': process, 'preview_fd': read_fd, 'preview_buffer': b'',
                                 'started': time.monotonic(), 'buffer': b'', 'log_bytes': 0,
                                 'last_preview': 0.0}
        run['status'] = 'running'
        self._persist(value)
        self.emit('experiment', self.get(value['id']))

    def _collect(self, value: dict, run: dict, active: dict, *, final=False) -> list:
        directory = self._path(value)
        run_dir = directory / 'runs' / run['id']
        run['wall_s'] = time.monotonic() - active['started']
        # Read at most 64 KiB per polling interval, so noisy children cannot starve control.
        try:
            raw = os.read(active['process'].stdout.fileno(), 65536)
        except (BlockingIOError, OSError):
            raw = b''
        buffer = active['buffer'] + raw
        lines = buffer.split(b'\n')
        active['buffer'] = lines.pop()[-4096:]
        if final and active['buffer']:
            lines.append(active['buffer'])
            active['buffer'] = b''
        remaining = max(0, 8 * 1024**2 - active['log_bytes'])
        if raw and remaining:
            with (run_dir / 'log.txt').open('ab') as output:
                output.write(raw[:remaining])
                active['log_bytes'] += min(len(raw), remaining)
        for line in lines[:256]:
            self.emit('log', {'source': run['id'], 'line': line[:4096].decode(errors='replace')})
        points = []
        metric_path = run_dir / 'metrics.jsonl'
        with metric_path.open('rb') as stream:
            stream.seek(self.offsets.get(run['id'], 0))
            for _ in range(256):
                start = stream.tell()
                line = stream.readline(65538)
                if not line:
                    break
                if active.get('metric_discard'):
                    active['metric_discard'] = not line.endswith(b'\n')
                    continue
                if not line.endswith(b'\n'):
                    if len(line) > 65536:
                        active['metric_discard'] = True
                    else:
                        stream.seek(start)
                    break
                if len(line) > 65536:
                    continue
                try:
                    point = json.loads(line)
                    if not isinstance(point, dict) or point.get('run') != run['id']:
                        continue
                    step = int(point['step'])
                    json.dumps(point, allow_nan=False)
                    run['steps'] = max(run['steps'], step)
                    if 'episode_return' in point:
                        reward = float(point['episode_return'])
                        run['last_return'] = reward
                        run['best_return'] = max(run.get('best_return', reward), reward)
                    points.append(point)
                except (ValueError, KeyError, TypeError):
                    continue
            self.offsets[run['id']] = stream.tell()
        if points:
            with (directory / 'metrics.jsonl').open('a') as output:
                for point in points:
                    output.write(json.dumps(point, allow_nan=False) + '\n')
        try:
            raw = os.read(active['preview_fd'], 262144)
        except BlockingIOError:
            raw = b''
        packets = (active['preview_buffer'] + raw).split(b'\n')
        active['preview_buffer'] = packets.pop()[-8 * 1024**2:]
        for packet in packets:
            try:
                point = json.loads(packet)
                now = time.monotonic()
                if now - active['last_preview'] >= 1 / 15:
                    point.update(session=run['id'], preview=True)
                    self.emit('frame', point)
                    active['last_preview'] = time.monotonic()
            except (ValueError, TypeError):
                pass
        run['checkpoints'] = [name for name in ('best', 'last') if (run_dir / f'{name}.zip').exists()]
        return points

    @staticmethod
    def _close_active(active: dict) -> None:
        active['process'].stdout.close()
        os.close(active['preview_fd'])

    def _loop(self) -> None:
        while not self.stop_event.wait(0.25):
            with self.lock:
                for value in self.items.values():
                    if value['status'] != 'running':
                        continue
                    points = []
                    for run in value['runs']:
                        active = self.active.get(run['id'])
                        if active:
                            try:
                                ended = active['process'].poll() is not None
                                points.extend(self._collect(value, run, active, final=ended))
                                if ended:
                                    run['status'] = 'done' if active['process'].returncode == 0 else 'failed'
                                    # Reap descendants even if the primary child exited normally.
                                    sandbox.terminate(active['process'])
                                    self._close_active(active)
                                    del self.active[run['id']]
                                    if run['status'] == 'failed':
                                        value['error'] = f"run {run['id']} exited {active['process'].returncode}"
                                    self.emit('experiment', self.get(value['id']))
                            except Exception as exc:
                                sandbox.terminate(active['process'])
                                self._close_active(active)
                                del self.active[run['id']]
                                run['status'] = 'failed'
                                value['error'] = str(exc)[:1000]
                        if run['status'] == 'queued' and len(self.active) < 2:
                            try:
                                self._launch(value, run)
                            except Exception as exc:
                                run['status'] = 'failed'
                                value['error'] = str(exc)[:1000]
                    if points:
                        self.emit('metrics', {'experiment': value['id'], 'points': points})
                    if all(run['status'] in {'done', 'failed', 'stopped'} for run in value['runs']):
                        value.update(status='failed' if any(run['status'] == 'failed' for run in value['runs']) else 'done',
                                     ended_at=timestamp())
                        self.emit('experiment', self.get(value['id']))
                    self._persist(value)

    def shutdown(self) -> None:
        self.stop_event.set()
        self.thread.join(timeout=3)
        with self.lock:
            for value in list(self.items.values()):
                if value['status'] == 'running':
                    self.stop(value['id'])
