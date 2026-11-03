"""Real-time environment ownership, controls, skills and policy playback."""
from __future__ import annotations

import ast
import importlib.util
import math
from pathlib import Path
import threading
import time
import uuid

import numpy as np

from . import registry
from .marks import snapshot
from .protocol import RPCError
from .scene import export_scene, frame
from .teleop import Mapping


class Session:
    """One environment, exclusively mutated while holding its reentrant lock."""
    def __init__(self, scenario: str, params: dict, seed: int | None, emit, directory: Path):
        self.id = 'ses_' + uuid.uuid4().hex
        self.emit = emit
        self.directory = directory / self.id
        self.lock = threading.RLock()
        self.stop = threading.Event()
        self.scenario = registry.get(scenario)(params)
        self.env = self.scenario.make_env()
        self.obs, _ = self.env.reset(seed=seed)
        self.mode = 'idle'
        self.inputs = {}
        self.pose = {}
        self.pose_state = {}
        self.targets = {}
        self.last_control = time.monotonic()
        self.last_input = 0.0
        self.step = self.episode = 0
        self.episode_return = 0.0
        self.policy = None
        self.mapping = None
        self.mapping_code = None
        self.skill_job = None
        self.thread = threading.Thread(target=self._loop, name=self.id, daemon=True)

    def start(self) -> None:
        self.thread.start()

    def scene(self) -> dict:
        with self.lock:
            return export_scene(self.env)

    def reset(self, seed=None) -> dict:
        with self.lock:
            self._cancel_skill('session reset')
            self.obs, _ = self.env.reset(seed=seed)
            self.pose_state = {}
            self.episode += 1
            self.episode_return = 0.0
            self.last_control = time.monotonic()
            return {'scene': export_scene(self.env)}

    def set_params(self, params: dict) -> dict:
        # Build first, so a bad parameter does not destroy the current session.
        scenario = type(self.scenario)(params)
        env = scenario.make_env()
        try:
            obs, _ = env.reset()
            scene = export_scene(env)
        except Exception:
            env.close()
            raise
        with self.lock:
            self._cancel_skill('parameters changed')
            self.env.close()
            self.env, self.scenario, self.obs = env, scenario, obs
            self.policy = None
            self.mode = 'idle'
            self.inputs, self.targets = {}, {}
            self.pose_state = {}
            self.episode += 1
            self.episode_return = 0.0
            self.last_control = time.monotonic()
        return {'scene': scene}

    def control(self, mode: str, inputs=None, pose=None) -> dict:
        if mode not in {'idle', 'manual', 'policy', 'pose', 'paused'}:
            raise RPCError('invalid_params', 'unknown session mode')
        values = {}
        for name, value in (inputs or {}).items():
            value = float(value)
            if not math.isfinite(value) or not -1 <= value <= 1:
                raise ValueError('inputs must be finite values in [-1,1]')
            values[name] = value
        with self.lock:
            if mode == 'policy' and self.policy is None:
                raise RPCError('unavailable', 'load a policy before selecting policy mode')
            self._cancel_skill('control changed')
            self.mode = mode
            self.last_control = time.monotonic()
            if inputs is not None:
                self.inputs = values
                self.last_input = self.last_control
            if pose is not None:
                if not isinstance(pose, dict):
                    raise ValueError('pose must be an object')
                self.pose = pose
                self.last_input = self.last_control
        return {'mode': mode}

    def load_policy(self, path: Path | None = None, algorithm: str | None = None,
                    policy_code: Path | None = None) -> dict:
        if path is None:
            pretrained = self.scenario.pretrained_policy()
            if not pretrained:
                raise RPCError('not_found', 'scenario has no pretrained policy')
            path = Path(pretrained)
        if not path.is_file():
            raise RPCError('not_found', 'policy checkpoint does not exist')
        if path.suffix == '.zip':
            import stable_baselines3 as sb3
            import torch
            torch.set_num_threads(2)
            # A bundled policy may use any supported algorithm.
            errors = []
            for name in ([algorithm] if algorithm else self.scenario.algorithms):
                try:
                    model = getattr(sb3, name.upper()).load(str(path), device='cpu')
                    def act(obs, model=model):
                        action = model.predict(obs, deterministic=True)[0]
                        if name.upper() == 'DQN':
                            from .sdk import discrete_action
                            return discrete_action(int(action), self.env.action_space.shape[0])
                        return action
                    break
                except Exception as exc:
                    errors.append(str(exc))
            else:
                raise ValueError('cannot load checkpoint: ' + '; '.join(errors)[:1000])
        elif policy_code and policy_code.is_file():
            spec = importlib.util.spec_from_file_location('robotlab_user_policy_' + self.id, policy_code)
            module = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(module)
            if not callable(getattr(module, 'act', None)):
                raise ValueError('policy.py must expose act(obs)')
            act = module.act
        elif path.suffix == '.pkl':
            import cloudpickle
            with path.open('rb') as stream:
                act = cloudpickle.load(stream)
            if not callable(act):
                raise ValueError('policy state requires policy.py exposing act(obs)')
        else:
            raise ValueError('custom policy state requires policy.py exposing act(obs)')
        with self.lock:
            self._cancel_skill('policy loaded')
            self.policy = act
            self.mode = 'policy'
            self.last_control = time.monotonic()
        return {'mode': 'policy'}

    def set_mapping(self, code: str | None) -> dict:
        try:
            if code is not None:
                tree = ast.parse(code)
                if not any(isinstance(node, ast.FunctionDef) and node.name == 'map_pose' for node in tree.body):
                    raise ValueError('mapping must define map_pose(pose, state)')
            mapping = Mapping(self.directory, code) if code is not None else None
        except (SyntaxError, ValueError) as exc:
            return {'ok': False, 'errors': [str(exc)]}
        with self.lock:
            old, self.mapping = self.mapping, mapping
            self.mapping_code = code
            self.targets = {}
            self.pose_state = {}
            self.last_control = time.monotonic()
        if old:
            old.close()
        return {'ok': True}

    def get_mapping(self) -> dict:
        """Return the custom script or an editable scenario default template."""
        with self.lock:
            self.last_control = time.monotonic()
            if self.mapping_code is not None:
                return {'source': 'custom', 'code': self.mapping_code}
            source = self.scenario.pose_mapping_source()
            if source is None:
                source = ('def map_pose(pose, state):\n'
                          '    # This scenario does not expose editable default mapping source.\n'
                          '    # Return actuator names mapped to normalized targets in [-1, 1].\n'
                          '    # Saving this template replaces the built-in mapping.\n'
                          '    return {}\n')
            return {'source': 'default', 'code': source}

    def snapshot(self, **params) -> dict:
        with self.lock:
            self.last_control = time.monotonic()
            return snapshot(self.env, **params)

    def skill(self, name: str, args: dict) -> dict:
        timeout = float(args.get('timeout', 30))
        if not math.isfinite(timeout) or not 0 < timeout <= 120:
            raise ValueError('skill timeout must be in (0,120] seconds')
        with self.lock:
            if self.skill_job:
                raise RPCError('conflict', 'a skill is already running')
            generator = iter(self.scenario.skill(self.env, name, args))
            job = {'iterator': generator, 'deadline': time.monotonic() + timeout,
                   'done': threading.Event(), 'frames': 0, 'previous': self.mode,
                   'result': {'ok': False, 'detail': 'skill interrupted', 'frames': 0}}
            self.skill_job = job
            self.mode = 'skill'
            self.last_control = time.monotonic()
        if not job['done'].wait(timeout + 0.5):
            with self.lock:
                self._cancel_skill('skill timeout')
        return job['result']

    def _finish_skill(self, ok: bool, detail: str | dict) -> None:
        job = self.skill_job
        if job:
            self.skill_job = None
            self.mode = job['previous']
            result = detail if isinstance(detail, dict) else {'detail': str(detail)}
            job['result'] = {**result, 'ok': ok, 'frames': job['frames']}
            self.obs = self.env._observation().astype(np.float32)
            if hasattr(job['iterator'], 'close'):
                job['iterator'].close()
            job['done'].set()

    def _cancel_skill(self, detail: str) -> None:
        self._finish_skill(False, detail)

    def _action(self, now: float) -> np.ndarray:
        if self.mode == 'policy':
            return self.policy(self.obs)
        if self.mode == 'manual':
            inputs = self.inputs if now - self.last_input <= 0.5 else {}
            return self.scenario.manual_action(self.env, inputs)
        if self.mode == 'pose':
            state = self.pose_state
            state.update({'t': float(self.env.data.time), 'params': self.scenario.params})
            if self.mapping:
                self.mapping.submit(self.pose, state)
                reply = self.mapping.poll()
                if reply:
                    if 'error' in reply:
                        self.targets = {}
                        self.emit('teleop', {'session': self.id, 'error': reply['error']})
                    else:
                        self.targets = reply.get('targets', {})
            else:
                mapping = self.scenario.pose_mapping()
                self.targets = mapping(self.pose, state) if mapping else {}
            targets = self.targets if now - self.last_input <= 0.5 else {}
            if not isinstance(targets, dict):
                raise ValueError('pose mapping must return an actuator dictionary')
            values = {name: float(value) for name, value in targets.items()}
            if not all(math.isfinite(value) for value in values.values()):
                raise ValueError('pose mapping returned a non-finite value')
            return self.scenario.manual_action(self.env, values)
        return self.scenario.idle_action(self.env)

    def _loop(self) -> None:
        deadline = time.monotonic()
        last_frame = 0.0
        try:
            while not self.stop.is_set():
                now = time.monotonic()
                if now - self.last_control > 600:
                    break
                event = None
                with self.lock:
                    if self.mode != 'paused':
                        reward, terminated, truncated, info = 0.0, False, False, {}
                        if self.skill_job:
                            job = self.skill_job
                            if now >= job['deadline']:
                                self._finish_skill(False, 'skill timeout')
                            else:
                                try:
                                    next(job['iterator'])
                                    job['frames'] += 1
                                except StopIteration as exc:
                                    self._finish_skill(True, exc.value or 'completed')
                                except Exception as exc:
                                    self._finish_skill(False, str(exc))
                        else:
                            try:
                                action = self._action(now)
                            except Exception as exc:
                                self.emit('teleop', {'session': self.id, 'error': str(exc)[:1000]})
                                action = self.scenario.idle_action(self.env)
                            self.obs, reward, terminated, truncated, info = self.env.step(action)
                            self.episode_return += reward
                        self.step += 1
                        ended = terminated or truncated
                        if time.monotonic() - last_frame >= 1 / 30:
                            event = frame(self.env, self.id, self.step, self.episode, reward,
                                          self.episode_return, ended, info, self.mode)
                        if ended and (self.mode == 'policy' or self.scenario.session_auto_reset):
                            self.obs, _ = self.env.reset()
                            self.episode += 1
                            self.episode_return = 0.0
                    dt = self.env.dt
                if event:
                    self.emit('frame', event)
                    last_frame = time.monotonic()
                # Never perform an unbounded burst of catch-up steps.
                deadline = max(deadline + dt, time.monotonic() - dt * 2)
                self.stop.wait(max(0.0, deadline - time.monotonic()))
        except Exception as exc:
            self.emit('log', {'source': self.id, 'line': str(exc)[:2000]})
        finally:
            self.stop.set()
            with self.lock:
                self._cancel_skill('session closed')
                self.env.close()
            if self.mapping:
                self.mapping.close()
                self.mapping = None

    def close(self) -> None:
        self.stop.set()
        self.thread.join(timeout=6)
        if self.mapping:
            self.mapping.close()
            self.mapping = None


class Sessions:
    """Bounded session registry; stopped sessions are reaped before each open."""
    def __init__(self, emit, directory: Path):
        self.emit, self.directory = emit, directory
        self.items = {}
        self.lock = threading.RLock()

    def open(self, scenario: str, params: dict | None = None, seed=None) -> dict:
        with self.lock:
            for key, value in list(self.items.items()):
                if value.stop.is_set():
                    value.close()
                    del self.items[key]
            if len(self.items) >= 4:
                # Browsers can vanish without closing their session; evict the
                # least recently controlled one instead of refusing new work.
                key = min(self.items, key=lambda k: self.items[k].last_control)
                stale = self.items.pop(key)
                stale.close()
                self.emit('log', {'source': 'sessions', 'line': f'closed least recently used session {key}'})
            session = Session(scenario, params or {}, seed, self.emit, self.directory)
            self.items[session.id] = session
            scene = session.scene()
            session.start()
            return {'session': session.id, 'scene': scene}

    def get(self, session: str) -> Session:
        with self.lock:
            value = self.items.get(session)
            if value is None or value.stop.is_set():
                raise RPCError('not_found', 'session does not exist')
            return value

    def close(self, session: str) -> dict:
        with self.lock:
            value = self.get(session)
            del self.items[session]
        value.close()
        return {}

    def shutdown(self) -> None:
        with self.lock:
            values, self.items = list(self.items.values()), {}
        for session in values:
            session.close()
