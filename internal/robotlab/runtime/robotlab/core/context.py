"""Fixed-step physics, grasp assist, interruption, recording and pacing."""
from __future__ import annotations

import math
import os
import threading
import time
from collections import deque

import mujoco
import numpy as np

from ..robots.base import ToolContext as BaseContext, ActionInterrupted
from ..protocol import RPCError
from .goals import evaluate
from .contacts import Contacts
from . import snapshots


class ToolContext(BaseContext):
    def __init__(self, world, *, goal=None, robot_start=None, stop=None, speed=1.,
                 record=None, event=None, log=None, session='', attempt=1):
        self.world, self.robot = world, world.robot
        self.model, self.data = world.model, world.data
        self.rng = np.random.default_rng(0)
        self.goal = goal
        self.robot_start = robot_start if robot_start is not None else self.robot.base_xy(self.model, self.data)
        self.stop = stop if stop is not None else threading.Event()
        self.speed = speed
        self.record, self.event, self.logger = record, event, log
        self.session, self.attempt = session, attempt
        self._object_bodies = {oid: world.object_body(oid) for oid in world.object_ids()}
        self._held = None
        self.seq = 0
        self.origin = float(self.data.time)
        self.next_frame = self.origin + .05
        self.last_live = float('-inf')
        self.history = deque()
        self.contacts = Contacts(world)
        self.begin_action()

    def begin_action(self, limit=60.):
        self.contacts.begin_action()
        self.action_time = float(self.data.time)
        self.wall_start = time.monotonic()
        self.wall_target = self.wall_start
        self.limit = min(float(limit), 60.)
        self.remember()

    def remember(self):
        state = snapshots.capture(self)
        if self.history and self.history[-1]['time'] == state['time']:
            self.history.pop()
        self.history.append(state)
        while len(self.history) > 1 and self.history[0]['time'] < self.data.time - 2. - 1e-9:
            self.history.popleft()

    def before_failure(self):
        target = float(self.data.time) - 1.5
        return min(self.history, key=lambda state: abs(state['time'] - target))

    def check(self):
        if self.stop.is_set():
            raise ActionInterrupted('stopped')
        reason = self.robot.fallen(self.model, self.data)
        if reason:
            raise ActionInterrupted('fell', reason)
        reason = self.robot.safety(self.model, self.data)
        if reason:
            raise ActionInterrupted('safety', reason)
        if self.goal and self.goal_result(fallen_checked=True)['met']:
            raise ActionInterrupted('goal')

    def step(self, seconds):
        if type(seconds) not in (int, float) or not math.isfinite(seconds) or seconds < 0:
            raise RPCError('invalid_params', 'step duration must be finite and nonnegative')
        dt = float(self.model.opt.timestep)
        for _ in range(max(0, math.ceil(seconds / dt - 1e-9))):
            if self.stop.is_set():
                raise ActionInterrupted('stopped')
            if self.data.time - self.action_time + dt > self.limit + 1e-8:
                raise ActionInterrupted('timeout', f'{self.limit:g} s simulation limit')
            self.robot.control(self.model, self.data)
            mujoco.mj_step(self.model, self.data)
            self.contacts.record()
            self._apply_hold()
            # Refresh poses, contacts and spatial velocities at the integrated
            # state. Actuation/acceleration/constraint solving happens in the
            # next mj_step; repeating it here doubles the solver work.
            mujoco.mj_fwdPosition(self.model, self.data)
            mujoco.mj_fwdVelocity(self.model, self.data)
            interruption = None
            try:
                self.check()
            except ActionInterrupted as exc:
                interruption = exc
            while self.data.time + 1e-9 >= self.next_frame:
                self.remember()
                self._frame(interruption)
                self.next_frame += .05
            if os.environ.get('EXCALIBUR_ROBOTLAB_FAST') != '1':
                speed = self.speed() if callable(self.speed) else self.speed
                self.wall_target += dt / speed
                remaining = self.wall_target - time.monotonic()
                # Accumulate small waits across controller step calls. Keeping
                # physics at most 10 ms ahead avoids 500 timer wakeups/s while
                # remaining smoother than the 50 ms recording/live cadence.
                if remaining >= .01:
                    self.stop.wait(remaining)
            if interruption:
                raise interruption

    def _frame(self, interruption=None):
        self.seq += 1
        status = {'goal': 'succeeded', 'stopped': 'stopped'}.get(
            interruption.reason, 'failed') if interruption else 'running'
        camera = self.robot.default_camera
        frame = {'session': self.session, 'attempt': self.attempt, 'seq': self.seq,
                 't': round(self.next_frame - self.origin, 4),
                 'poses': np.asarray(self.world.poses()).round(4).tolist(),
                 'camera': {'name': camera, 'pose': [round(float(v), 4) for v in self.world.camera_pose(camera)]},
                 'held': self.held(), 'status': status}
        if self.record:
            self.record(frame)
        wall = time.monotonic()
        if self.event and wall - self.last_live >= .05:
            self.event('frame', frame)
            self.last_live = wall

    def _body(self, object_id):
        if object_id not in self._object_bodies:
            raise RPCError('not_found', f'object not found: {object_id}')
        return self._object_bodies[object_id]

    def object_pose(self, object_id):
        b = self._body(object_id)
        return self.data.xpos[b].copy(), self.data.xquat[b].copy()

    def objects(self):
        return [dict(self.world.object_info(id), pos=self.object_pose(id)[0].round(4).tolist())
                for id in self.world.object_ids()]

    def hold(self, object_id, site):
        sid = mujoco.mj_name2id(self.model, mujoco.mjtObj.mjOBJ_SITE, site)
        b = self._body(object_id)
        if sid < 0 or self.model.body_jntnum[b] != 1:
            raise RPCError('invalid_params', 'grasp requires a valid site and a free object')
        jnt = self.model.body_jntadr[b]
        if self.model.jnt_type[jnt] != mujoco.mjtJoint.mjJNT_FREE:
            raise RPCError('invalid_params', 'only free objects can be held')
        sm = self.data.site_xmat[sid].reshape(3, 3)
        rel_p = sm.T @ (self.data.xpos[b] - self.data.site_xpos[sid])
        sq = np.zeros(4)
        mujoco.mju_mat2Quat(sq, sm.flatten())
        inv, rel_q = np.zeros(4), np.zeros(4)
        mujoco.mju_negQuat(inv, sq)
        mujoco.mju_mulQuat(rel_q, inv, self.data.xquat[b])
        self._held = (object_id, sid, rel_p, rel_q)

    def _apply_hold(self):
        if self._held is None:
            return
        oid, site, rel_p, rel_q = self._held
        jnt = self.model.body_jntadr[self._body(oid)]
        sm = self.data.site_xmat[site].reshape(3, 3)
        sq, q = np.zeros(4), np.zeros(4)
        mujoco.mju_mat2Quat(sq, sm.flatten())
        mujoco.mju_mulQuat(q, sq, rel_q)
        adr, vadr = self.model.jnt_qposadr[jnt], self.model.jnt_dofadr[jnt]
        self.data.qpos[adr:adr + 3] = self.data.site_xpos[site] + sm @ rel_p
        self.data.qpos[adr + 3:adr + 7] = q
        self.data.qvel[vadr:vadr + 6] = 0

    def held(self):
        return self._held[0] if self._held else None

    def release(self):
        oid = self.held()
        self._held = None
        return oid

    def goal_result(self, *, fallen_checked=False):
        return evaluate(self.goal, self, self.robot_start, fallen_checked=fallen_checked) if self.goal else {'met': False, 'detail': 'no goal'}

    def log(self, text):
        if self.logger:
            self.logger(text)

    def held_state(self):
        if not self._held:
            return None
        oid, sid, p, q = self._held
        return {'object': oid, 'site': self.model.site(sid).name, 'position': p.tolist(), 'quat': q.tolist()}

    def restore_hold(self, value, *, apply=True):
        if value:
            self._held = (value['object'], self.model.site(value['site']).id,
                          np.array(value['position']), np.array(value['quat']))
            if apply:
                self._apply_hold()
                mujoco.mj_forward(self.model, self.data)
