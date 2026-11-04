"""One live session, durable attempts, replay, observations and activity logs."""
from __future__ import annotations

import base64
import copy
import re
import threading
import uuid
from contextlib import contextmanager

from .. import robots
from ..protocol import RPCError
from .environments import build
from .store import Store, now, write_json, read_json, slugify
from .summaries import action_summary
from .validation import check


class Sessions:
    def __init__(self, root, settings, environments, event=lambda *_: None):
        self.store = Store(root)
        self.settings, self.environments, self.event = settings, environments, event
        self.lock = threading.RLock()
        self.action_lock = threading.Lock()
        self.stop_flag = threading.Event()
        self.live = None
        self.world = self.ctx = None
        self.skills = None
        for session in self.store.sessions():
            if session['status'] == 'live':
                session['status'], session['ended_at'] = 'stopped', now()
                attempt = session['attempts'][-1]
                frames = self.store.lines(self.store.session_dir(session['id']) / 'attempts' / str(attempt['n']) / 'frames.jsonl')
                if frames:
                    attempt['frames'] = len(frames)
                    attempt['sim_time'] = max(attempt['sim_time'], frames[-1]['t'])
                if attempt['status'] == 'running':
                    attempt.update(status='stopped', reason='runtime restarted', ended_at=now())
                session['outcome'] = {'status': 'stopped', 'attempt': session['attempt'], 'detail': 'runtime restarted'}
                self.store.save(session)
                if attempt['skills_used']:
                    # Skills accounting is idempotent and recovered when attached.
                    session['_recover_skills'] = True
                    self.store.save(session)

    @contextmanager
    def action(self):
        if not self.action_lock.acquire(blocking=False):
            raise RPCError('conflict', 'an action is already running')
        try:
            yield
        finally:
            self.action_lock.release()

    def attach_skills(self, skills):
        self.skills = skills
        for session in self.store.sessions():
            if session.pop('_recover_skills', False):
                skills.attempt_end(session, session['attempts'][-1])
                self.store.save(session)

    def _save(self, session, emit=True):
        self.store.save(session)
        if emit:
            self.event('session', {'session': copy.deepcopy(session)})

    def get(self, session):
        with self.lock:
            return copy.deepcopy(self.live) if self.live and self.live['id'] == session else self.store.load(session)

    def list(self, robot=None, environment=None, query=None):
        for value in (robot, environment, query):
            if value is not None and not isinstance(value, str):
                raise RPCError('invalid_params', 'session filters must be text')
        def matches(s):
            return ((robot is None or s['robot'] == robot)
                    and (environment is None or environment.casefold() in s['environment'].casefold())
                    and (query is None or any(query.casefold() in s[k].casefold()
                                              for k in ('name', 'task', 'environment'))))
        with self.lock:
            return [dict((k, s[k]) for k in ('id', 'name', 'robot', 'environment', 'task', 'status', 'created_at', 'ended_at'))
                    | {'attempts': len(s['attempts']), 'live': s['status'] == 'live'} for s in self.store.sessions() if matches(s)]

    def _current(self, session, running=False):
        if not self.live or self.live['id'] != session or self.live['status'] != 'live':
            raise RPCError('conflict', 'session is not live')
        attempt = self.live['attempts'][-1]
        if running and attempt['status'] != 'running':
            raise RPCError('conflict', f'attempt {attempt["n"]} ended ({attempt["reason"]}): call attempt_reset with a reflection')
        return self.live

    def start(self, robot, environment, task, goal, name=None, copy_skills_from=None, conversation=None):
        from . import goals
        with self.action(), self.lock:
            if self.live and self.live['status'] == 'live':
                raise RPCError('conflict', 'a live session already exists')
            env = self.environments.get(environment)
            plugin = robots.make(robot)
            if env['kind'] != plugin.world_kind:
                raise RPCError('invalid_params', 'robot and environment kinds do not match')
            errors = goals.validate(goal, env.get('objects', []))
            if errors:
                raise RPCError('invalid_params', '; '.join(errors))
            if not isinstance(task, str) or not task.strip():
                raise RPCError('invalid_params', 'task is required')
            if name is not None and (not isinstance(name, str) or not name.strip()):
                raise RPCError('invalid_params', 'name must be nonempty')
            if copy_skills_from:
                self.get(copy_skills_from)
            title = name or f'{plugin.name["es"]} · {env["name"]} · {now()[:10]}'
            slug = slugify(title, 60, 'session')
            sid = slug + '-' + uuid.uuid4().hex[:4]
            while (self.store.session_dir(sid) / 'session.json').exists():
                sid = slug + '-' + uuid.uuid4().hex[:4]
            world = build(env, plugin)
            self.live = {'id': sid, 'name': title, 'robot': robot, 'environment': environment, 'task': task,
                         'goal': copy.deepcopy(goal), 'status': 'live', 'created_at': now(), 'ended_at': None,
                         'max_attempts': self.settings.get()['max_attempts'], 'attempt': 0, 'attempts': [],
                         'conversation': conversation, 'skill_events': [], 'outcome': None}
            write_json(self.store.session_dir(sid) / 'environment.json', env)
            self.world = world
            self._new_attempt()
            # Geometry and mesh keys stay fixed for the entire session. Export
            # before actions start so viewers never need the live physics data.
            write_json(self.store.session_dir(sid) / 'scene.json', world.scene())
            if copy_skills_from and self.skills:
                self.skills.copy(copy_skills_from, sid, _reserved=True)
            self.note(sid, 'Session started', 'system')
            self._save(self.live)
            return self._started(plugin)

    def _started(self, plugin):
        return {'session': copy.deepcopy(self.live), 'card': plugin.card(),
                'skills': self.skills.list(self.live['id']) if self.skills else [],
                'next': 'Read the robot card (joints, limits, reference pose, notes) and session skills; '
                        'call skill_use(name) for relevant skills before acting. Plan small keyframes, '
                        'check with ik, then sequence (prefer 4–12 keyframes per call). Read state '
                        '(support, margin, terrain) after each call. On failure, reflect and write or '
                        'improve a markdown skill with the lesson and exact keyframes that worked '
                        '(failures=[N]), then attempt_reset. Only claim success when goal.met is true.'}

    def _resume_available(self, session):
        if self.live and self.live['status'] == 'live':
            raise RPCError('conflict', f'Live session {self.live["id"]} ({self.live["name"]}) already exists; '
                           'call session_stop on it by agent, then retry session_resume.')
        saved = self.store.load(session)
        if saved['status'] == 'live':
            raise RPCError('conflict', f'Session {session} is already live; inspect session_status first.')
        return saved

    def resume(self, session, reason=None, conversation=None):
        if reason is not None and not isinstance(reason, str):
            raise RPCError('invalid_params', 'reason must be text')
        # Report the live owner even while its motion holds the action lock.
        with self.lock:
            self._resume_available(session)
        with self.action(), self.lock:
            saved = self._resume_available(session)
            env = read_json(self.store.session_dir(session) / 'environment.json')
            if env is None:
                raise RPCError('not_found', 'saved session environment not found')
            plugin = robots.make(saved['robot'])
            world = build(env, plugin)
            saved.update(status='live', ended_at=None, outcome=None, conversation=conversation,
                         max_attempts=len(saved['attempts']) + self.settings.get()['max_attempts'])
            self.live, self.world = saved, world
            self._new_attempt()
            # Keep the original scene and all previous recordings intact.
            self.note(session, 'Session resumed' + (f': {reason}' if reason else ''), 'system')
            self._save(self.live)
            return self._started(plugin)

    def _new_attempt(self):
        import mujoco
        from .context import ToolContext
        self.stop_flag = threading.Event()
        mujoco.mj_resetData(self.world.model, self.world.data)
        self.world.robot.reset(self.world.model, self.world.data)
        mujoco.mj_forward(self.world.model, self.world.data)
        self.live['attempt'] += 1
        n = self.live['attempt']
        self.live['attempts'].append({'n': n, 'status': 'running', 'reason': '', 'started_at': now(),
                                      'ended_at': None, 'sim_time': 0., 'frames': 0, 'skills_used': []})
        path = self.store.session_dir(self.live['id']) / 'attempts' / str(n)
        path.mkdir(parents=True, exist_ok=True)
        (path / 'frames.jsonl').touch()
        self.ctx = ToolContext(self.world, goal=self.live['goal'], stop=self.stop_flag,
                               speed=lambda: self.settings.get()['sim_speed'], record=self._record, event=self.event,
                               log=lambda text: self.note(self.live['id'], text, 'note'),
                               session=self.live['id'], attempt=n)
        self._last_call = {'tool': None, 'args': {}}
        self.ctx._frame(initial=True)

    def _cache_observation(self):
        # Called only by the simulation owner, with self.lock held. Readers use
        # this coherent snapshot instead of inspecting data during mj_step.
        self._cached_state = self.world.robot.state(self.world.model, self.world.data)
        self._cached_observation = copy.deepcopy({
            'state': self._cached_state, 'held': self.ctx.held(), 'goal': self.ctx.goal_result()})

    def _record(self, frame):
        with self.lock:
            attempt = self.live['attempts'][-1]
            attempt['frames'] += 1
            attempt['sim_time'] = float(self.world.data.time)
            self._cache_observation()
            self.store.append(self.store.session_dir(self.live['id']) / 'attempts' / str(attempt['n']) / 'frames.jsonl', frame)

    def note(self, session, text, kind='note', **fields):
        if kind not in ('act', 'look', 'skill', 'note', 'plan', 'reflection', 'system'):
            raise RPCError('invalid_params', 'invalid log kind')
        if not isinstance(text, str):
            raise RPCError('invalid_params', 'log text must be a string')
        with self.lock:
            s = self.get(session)
            entry = {'id': uuid.uuid4().hex, 't': now(), 'session': session, 'attempt': s['attempt'],
                     'kind': kind, 'summary': text, 'sim_time': (float(self.world.data.time) if self.world is not None
                          and self.live and self.live['id'] == session else s['attempts'][-1]['sim_time']), **fields}
            self.store.append(self.store.session_dir(session) / 'log.jsonl', entry)
            self.event('log', {'entry': copy.deepcopy(entry)})
            return entry

    def log(self, session, attempt=None, since=0):
        self.get(session)
        if type(since) is not int or since < 0:
            raise RPCError('invalid_params', 'since must be a nonnegative cursor')
        entries = self.store.lines(self.store.session_dir(session) / 'log.jsonl')
        return {'entries': [e for e in entries[since:] if attempt is None or e['attempt'] == attempt], 'next': len(entries)}

    def frames(self, session, attempt, start=0, count=400):
        s = self.get(session)
        if type(attempt) is not int or not 1 <= attempt <= len(s['attempts']):
            raise RPCError('not_found', 'attempt not found')
        if type(start) is not int or start < 0 or type(count) is not int or not 1 <= count <= 400:
            raise RPCError('invalid_params', 'invalid frame range (count must be 1–400)')
        entries = self.store.lines(self.store.session_dir(session) / 'attempts' / str(attempt) / 'frames.jsonl')
        return {'frames': entries[start:start+count], 'total': len(entries)}

    def image(self, session, path):
        self.get(session)
        if not isinstance(path, str) or not re.fullmatch(
                r'attempts/[1-9][0-9]*/(?:looks/[^/\\\x00]+\.png|failure/image\.png)', path):
            raise RPCError('invalid_params', 'invalid session image path')
        root = self.store.session_dir(session).resolve()
        image = (root / path).resolve()
        if not image.is_relative_to(root):
            raise RPCError('invalid_params', 'invalid session image path')
        try:
            return {'png': base64.b64encode(image.read_bytes()).decode()}
        except (FileNotFoundError, IsADirectoryError):
            raise RPCError('not_found', 'session image not found') from None

    def _end_attempt(self, status, reason):
        attempt = self.live['attempts'][-1]
        if attempt['status'] != 'running':
            return
        attempt.update(status=status, reason=reason, ended_at=now(), sim_time=float(self.world.data.time))
        if self.skills:
            self.skills.attempt_end(self.live, attempt)

    def _end_session(self, status, detail):
        self.live.update(status=status, ended_at=now(), outcome={
            'status': status, 'attempt': self.live['attempt'], 'detail': detail})

    def stop(self, session, by, reason=''):
        if by not in ('user', 'agent'):
            raise RPCError('invalid_params', 'stop by must be user or agent')
        with self.lock:
            self._current(session)
            self.stop_flag.set()
            detail = reason or f'stopped by {by}'
            self._end_attempt('stopped', detail)
            self._end_session('stopped', detail)
            self.note(session, detail, 'system')
            self._save(self.live)
            return {'session': copy.deepcopy(self.live)}

    def reset(self, session, reflection, no_skill_reason=None):
        if not isinstance(reflection, str) or not reflection.strip():
            raise RPCError('invalid_params', 'reflection is required')
        if no_skill_reason is not None and (not isinstance(no_skill_reason, str) or not no_skill_reason.strip()):
            raise RPCError('invalid_params', 'no_skill_reason must be nonempty text')
        with self.action():
            with self.lock:
                self._current(session)
                self.note(session, reflection, 'reflection')
                abandoned = self.live['attempts'][-1]['status'] == 'running'
                self._end_attempt('abandoned', reflection)
            if abandoned:
                self._failure(self._last_call)
            with self.lock:
                # A user stop can arrive while the failure cameras render.
                self._current(session)
                if no_skill_reason is not None:
                    self.note(session, f'No new skill: {no_skill_reason}', 'reflection')
                # Persist abandonment and its evidence even when the learning
                # gate refuses advancement, so failures=[N] is immediately usable.
                self._save(self.live)
                if self.live['attempt'] >= self.live['max_attempts']:
                    self._end_session('exhausted', 'attempt limit reached')
                    self._save(self.live)
                    raise RPCError('conflict', 'attempt limit reached; session exhausted')
                attempt = self.live['attempts'][-1]
                learned = any(e['kind'] in ('created', 'improved') and (
                    e['attempt'] == attempt['n'] or (attempt['ended_at'] and e['t'] >= attempt['ended_at']))
                    for e in self.live['skill_events'])
                if attempt['status'] in ('failed', 'abandoned') and not learned and no_skill_reason is None:
                    raise RPCError('conflict', 'Before the next attempt, write or improve a skill from this failure: '
                                   'skill_write(name, description, instructions, failures=[N]). '
                                   'Pass no_skill_reason only if nothing new was learned.')
                self._new_attempt()
                self._save(self.live)
                return {'session': copy.deepcopy(self.live), 'attempt': self.live['attempt']}

    def scene(self, session):
        s = self.get(session)
        path = self.store.session_dir(session) / 'scene.json'
        scene = read_json(path)
        if scene is None:
            # Older recordings predate the scene cache. Rebuild in an isolated
            # world, without reserving or reading the active simulation.
            env = read_json(path.parent / 'environment.json') or self.environments.get(s['environment'])
            scene = build(env, robots.make(s['robot'])).scene()
            write_json(path, scene)
        return scene

    def look(self, session, **options):
        with self.action():
            with self.lock:
                self._current(session, running=True)
            return self._look(**options)

    def _look(self, **options):
        from . import perception
        observation = perception.look(self.world, **options)
        sid, n = self.live['id'], self.live['attempt']
        folder = self.store.session_dir(sid) / 'attempts' / str(n) / 'looks'
        folder.mkdir(parents=True, exist_ok=True)
        k = len(list(folder.glob('*.png'))) + 1
        image = f'attempts/{n}/looks/{k}.png'
        (self.store.session_dir(sid) / image).write_bytes(base64.b64decode(observation['png']))
        self.note(sid, f'Observed {len(observation["visible"])} objects', 'look', image=image)
        return observation

    def state(self, session):
        with self.lock:
            self._current(session)
            return copy.deepcopy(dict(self._cached_observation, attempt=self.live['attempts'][-1]))

    def call_tool(self, tool, args):
        tools = self.world.robot.tools()
        if tool not in tools:
            raise RPCError('not_found', f'unknown tool: {tool}')
        spec = tools[tool]
        check(args, spec.params)
        self.ctx.check()
        return spec.fn(self.ctx, **args)

    def act(self, session, tool, args):
        return self.execute(session, lambda: self.call_tool(tool, args), {'tool': tool, 'args': args})

    def execute(self, session, fn, last_call):
        from ..robots.base import ActionInterrupted
        with self.action():
            with self.lock:
                self._current(session, running=True)
                previous_status = self.live['attempts'][-1]['status']
                args = last_call.get('args')
                self.ctx.begin_action(settle=args.get('settle', .2) if isinstance(args, dict) else .2)
                self._last_call = copy.deepcopy(last_call)
            result, interrupted = {}, None
            try:
                result = fn()
            except ActionInterrupted as exc:
                interrupted = exc
            except (TypeError, ValueError) as exc:
                raise RPCError('invalid_params', str(exc)) from exc
            with self.lock:
                if interrupted:
                    reason = interrupted.detail or interrupted.reason
                    if interrupted.reason == 'goal':
                        self._end_attempt('succeeded', 'goal')
                        if self.live['status'] == 'live':
                            self._end_session('succeeded', 'goal met')
                    elif interrupted.reason == 'stopped':
                        self._end_attempt('stopped', reason)
                        if self.live['status'] == 'live':
                            self._end_session('stopped', reason)
                    elif interrupted.reason == 'timeout':
                        # A long action only ends itself; the attempt continues from here.
                        if not isinstance(result, dict) or not result:
                            result = {'ok': False, 'error': f'action stopped at the {reason}; the attempt continues. '
                                      'Use shorter keyframe sequences and inspect the state before continuing.'}
                    else:
                        contact_detail = self.ctx.contacts.describe_recent()
                        if contact_detail:
                            if reason != interrupted.reason:
                                reason = f'{interrupted.reason}: {reason}'
                            reason += contact_detail
                        result = dict(result, error=reason, contacts=self.ctx.contacts.recent())
                        self._end_attempt('failed', reason)
            if interrupted and interrupted.reason not in ('goal', 'stopped') and not (interrupted.reason == 'timeout'):
                self._failure(last_call)
            with self.lock:
                attempt = self.live['attempts'][-1]
                attempt['sim_time'] = float(self.world.data.time)
                self._cache_observation()
                ok = (not interrupted or interrupted.reason == 'goal') and (not isinstance(result, dict) or result.get('ok', True))
                if isinstance(result, dict):
                    result = dict(result, contacts=(self.ctx.contacts.recent() if attempt['status'] == 'failed'
                                                    else self.ctx.contacts.during_action()))
                response = {'ok': bool(ok), 'result': result, 'state': self._cached_state,
                            'attempt': {k: attempt[k] for k in ('n', 'status', 'reason')}, 'goal': self.ctx.goal_result()}
                if attempt['status'] == 'failed':
                    response['next'] = (f'Attempt {attempt["n"]} failed ({attempt["reason"]}). Reflect, write or improve '
                                        f'a markdown skill with the lesson and exact keyframes that worked using skill_write(..., failures=[{attempt["n"]}]), then attempt_reset.')
                self.note(session, action_summary(last_call.get('tool', 'action'), response,
                                                  previous_status, held=self.ctx.held()),
                          'act', args=last_call.get('args', {}), ok=bool(ok), state=copy.deepcopy(self._cached_state),
                          **({'tool': last_call['tool']} if 'tool' in last_call else {}))
                self._save(self.live)
                return response

    def _failure(self, last_call):
        from . import perception
        from . import snapshots
        folder = self.store.session_dir(self.live['id']) / 'attempts' / str(self.live['attempt']) / 'failure'
        folder.mkdir(parents=True, exist_ok=True)
        final = snapshots.capture(self.ctx)
        state = dict(copy.deepcopy(self.ctx.before_failure()), fell_at=final['time'], final=final,
                     env=copy.deepcopy(self.world.env), robot=self.live['robot'], last_call=last_call,
                     contacts=self.ctx.contacts.recent())
        write_json(folder / 'state.json', state)
        equivalent = (f'robot.{last_call["tool"]}(**{last_call.get("args", {})!r})\n' if last_call.get('tool')
                      else '# No tool or skill was called in this attempt.\n')
        (folder / 'code.py').write_text(equivalent)
        try:
            for name, pose in (('image.png', state), ('image_fall.png', final)):
                snapshots.restore(self.ctx, pose)
                try:
                    image = base64.b64decode(perception.look(self.world)['png'])
                except Exception as exc:
                    # Preserve complete evidence even if the render backend fails.
                    import cv2
                    import numpy as np
                    image = cv2.imencode('.png', np.zeros((480, 640, 3), np.uint8))[1].tobytes()
                    self.note(self.live['id'], f'Failure camera unavailable: {exc}', 'system')
                (folder / name).write_bytes(image)
        finally:
            snapshots.restore(self.ctx, final)
