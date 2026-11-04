"""Torque-controlled quadruped with a trunk-mounted six-axis gripper arm."""
from __future__ import annotations

import math
from collections import Counter, deque
from pathlib import Path

import mujoco
import numpy as np

from .base import Robot, Spawn, ToolContext, ToolSpec, json_fail, json_ok


LEGS = ('FL', 'FR', 'RL', 'RR')
HIPS = np.array([[.1934, .0465, 0], [.1934, -.0465, 0],
                 [-.1934, .0465, 0], [-.1934, -.0465, 0]])
SIDES = np.array([1, -1, 1, -1])
STOW = np.array([0., .10, -.15, .05, 0., 0.])


def _stow_error(q):
    # The wrist roll does not change the folded arm's footprint or balance; a held
    # object can block it against the trunk, so it is not part of the stowed check.
    return float(np.max(np.abs(np.asarray(q)[:5]-STOW[:5])))


class _MotionTimeout(Exception):
    """Internal navigation deadline, distinct from runtime interruptions."""


class _NavigationContext:
    """Bound every preparation/motion/settling step to one simulation deadline."""
    run_until = ToolContext.run_until

    def __init__(self, ctx, deadline):
        self.ctx, self.deadline = ctx, deadline

    def __getattr__(self, name):
        return getattr(self.ctx, name)

    def step(self, seconds):
        remaining = self.deadline-self.data.time
        if seconds > remaining+1e-9:
            dt = float(self.model.opt.timestep)
            ticks = math.floor((remaining+1e-9)/dt)
            if ticks > 0:
                self.ctx.step(ticks*dt)
            raise _MotionTimeout
        self.ctx.step(seconds)


def wrap(angle):
    return (angle + math.pi) % (2 * math.pi) - math.pi


def number(value, low, high):
    value = float(value)
    if not math.isfinite(value):
        raise ValueError('Expected a finite number')
    return min(high, max(low, value))


def leg_ik(x, y, z, side):
    """Analytic abduction/hip/knee inverse kinematics in the hip frame."""
    offset, length = side * .0955, .213
    radius = math.hypot(y, z)
    vertical = math.sqrt(max(.003, radius * radius - offset * offset))
    abduction = math.atan2(z, y) - math.atan2(-vertical, offset)
    knee = -math.acos(min(.999, max(-.999, (x*x + vertical*vertical - 2*length*length) / (2*length*length))))
    hip = math.atan2(-x, vertical) - knee / 2
    return [wrap(abduction), hip, knee]


class DogRobot(Robot):
    id = 'dog'
    name = {'en': 'Dog with arm', 'es': 'Perro con brazo'}
    world_kind = 'room'
    default_camera = 'robot/head'

    def attach(self, spec, spawn: Spawn, menagerie: str):
        self.spawn = spawn
        dog = mujoco.MjSpec.from_file(str(Path(menagerie) / 'unitree_go2/go2.xml'))
        arm = mujoco.MjSpec.from_file(str(Path(menagerie) / 'unitree_z1/z1_gripper.xml'))
        for child in (dog, arm):
            for key in list(child.keys):
                child.delete(key)
        trunk = dog.body('base')
        mount = trunk.add_frame(pos=[.035, 0, .065])
        arm.option.integrator = dog.option.integrator
        dog.attach(arm, prefix='arm/', frame=mount)
        trunk.add_camera(name='head', pos=[.31, 0, .035],
                         xyaxes=[0, -1, 0, .258819, 0, .965926], fovy=65)
        hand = dog.body('arm/link06')
        hand.add_site(name='arm/grasp', pos=[.186, 0, .05], size=[.008, 0, 0])
        hand.add_camera(name='arm', pos=[.11, 0, .04], xyaxes=[0, -1, 0, 0, 0, 1], fovy=65)
        # Preserve floor/object contacts and exclude robot self contacts: the
        # imported arm and trunk have no authored mounting collision exclusions.
        for geom in dog.geoms:
            if geom.group == 3:
                geom.contype = 2
                geom.conaffinity = 1
        frame = spec.worldbody.add_frame(pos=[spawn.x, spawn.y, 0],
                                         quat=[math.cos(spawn.yaw/2), 0, 0, math.sin(spawn.yaw/2)])
        dog.option.integrator = spec.option.integrator
        dog.option.impratio = spec.option.impratio
        dog.option.cone = spec.option.cone
        spec.attach(dog, prefix='robot/', frame=frame)
        spec.option.timestep = .002
        spec.option.integrator = mujoco.mjtIntegrator.mjINT_IMPLICITFAST
        spec.option.cone = mujoco.mjtCone.mjCONE_ELLIPTIC

    def bind(self, model, data):
        self.base = model.body('robot/base').id
        self.root = model.body_jntadr[self.base]
        self.rq = model.jnt_qposadr[self.root]
        self.rv = model.jnt_dofadr[self.root]
        joints = [model.joint('robot/' + leg + '_' + part + '_joint').id
                  for leg in LEGS for part in ('hip', 'thigh', 'calf')]
        self.lq = model.jnt_qposadr[joints]
        self.lv = model.jnt_dofadr[joints]
        self.lrange = model.jnt_range[joints].copy()
        self.la = np.array([model.actuator('robot/' + leg + '_' + part).id
                            for leg in LEGS for part in ('hip', 'thigh', 'calf')])
        self.feet = [model.geom('robot/' + leg).id for leg in LEGS]
        aj = [model.joint('robot/arm/joint' + str(i)).id for i in range(1, 7)]
        self.aq, self.av = model.jnt_qposadr[aj], model.jnt_dofadr[aj]
        self.aa = [model.actuator('robot/arm/motor' + str(i)).id for i in range(1, 7)]
        self.arange = model.jnt_range[aj].copy()
        self.gq = model.joint('robot/arm/jointGripper').qposadr[0]
        self.ga = model.actuator('robot/arm/motorGripper').id
        self.site = model.site('robot/arm/grasp').id
        self.mass = float(model.body_subtreemass[self.base])
        self.scratch = mujoco.MjData(model)
        self.jac = np.zeros((3, model.nv))
        self.jacr = np.zeros((3, model.nv))
        self.target = np.zeros(12)

    def reset(self, model, data):
        self.height = .30
        data.qpos[self.rq:self.rq+7] = [self.spawn.x, self.spawn.y, .30,
                                       math.cos(self.spawn.yaw/2), 0, 0, math.sin(self.spawn.yaw/2)]
        for i, side in enumerate(SIDES):
            data.qpos[self.lq[i*3:i*3+3]] = leg_ik(0, side*.0955, -.278, side)
        self.arm_target = STOW.copy()
        data.qpos[self.aq] = self.arm_target
        self.gripper = 'open'
        data.qpos[self.gq] = -1.5
        self.velocity = np.zeros(2)
        self.yaw_target = self.spawn.yaw
        self.turn_rate = 0.
        self.step_height, self.period = .08, .4
        self.low_since = None
        self.holding = None
        self._last = -1.
        self.phase = 0.
        self._arm_stowed = True

    def attitude(self, data):
        rot = data.xmat[self.base].reshape(3, 3)
        return rot, math.atan2(rot[2, 1], rot[2, 2]), math.asin(np.clip(-rot[2, 0], -1, 1)), math.atan2(rot[1, 0], rot[0, 0])

    def control(self, model, data):
        rot, roll, pitch, yaw = self.attitude(data)
        moving = np.linalg.norm(self.velocity) > .005 or abs(self.turn_rate) > .005
        omega = np.clip(self.turn_rate + 2.5*wrap(self.yaw_target-yaw), -.9, .9)
        if data.time - self._last >= .009 or self._last < 0:
            elapsed = .01 if self._last < 0 else data.time-self._last
            self._last = data.time
            if moving:
                self.phase = (self.phase + elapsed / self.period) % 1
            else:
                self.phase = 0.
            local_v = rot.T @ data.qvel[self.rv:self.rv+3]
            for i, side in enumerate(SIDES):
                hip = HIPS[i]
                p = (self.phase + (0 if i in (0, 3) else .5)) % 1
                stance = not moving or p < .6
                vel = self.velocity + omega * np.array([-hip[1]-side*.0955, hip[0]])
                stride = vel * self.period * .6
                if not moving:
                    offset, lift = np.zeros(2), 0.
                elif stance:
                    offset, lift = stride*(.5-p/.6), 0.
                else:
                    u = (p-.6)/.4
                    offset, lift = stride*(-.5 + (3*u*u-2*u*u*u)), self.step_height*math.sin(math.pi*u)
                # Raibert placement corrects speed; stance width supports the arm mass.
                offset += .10*(local_v[:2]-self.velocity)
                # The folded arm shifts the combined center of mass rearward.
                x = offset[0] - .025
                y = side*.11 + offset[1]
                z = -self.height+.022+lift + .45*(roll*(hip[1]+side*.11)-pitch*hip[0])
                self.target[i*3:i*3+3] = leg_ik(x, y, z, side)
            np.clip(self.target, self.lrange[:, 0], self.lrange[:, 1], out=self.target)
        q, v = data.qpos[self.lq], data.qvel[self.lv]
        torque = 55*(self.target-q) - 1.8*v
        # Distribute gravity and velocity feedback through the stance feet.
        local_v = rot.T @ data.qvel[self.rv:self.rv+3]
        support = []
        for i in range(4):
            p = (self.phase + (0 if i in (0, 3) else .5)) % 1
            if not moving or p < .6:
                support.append(i)
        for i in support:
            foot = data.geom_xpos[self.feet[i]]
            mujoco.mj_jac(model, data, self.jac, None, foot, model.geom_bodyid[self.feet[i]])
            force = np.array([self.mass*5.0*(self.velocity[0]-local_v[0]),
                              self.mass*5.0*(self.velocity[1]-local_v[1]), self.mass*9.81]) / len(support)
            # Vertical forces oppose trunk angular error and angular velocity.
            r = rot.T @ (foot-data.xpos[self.base])
            ang = data.qvel[self.rv+3:self.rv+6]
            force[2] += (-90*roll-8*ang[0])*r[1]/.12 + (90*pitch+8*ang[1])*r[0]/.18
            force[2] = max(0., force[2])
            idx = slice(i*3, i*3+3)
            torque[idx] -= self.jac[:, self.lv[idx]].T @ (rot @ force)
        data.ctrl[self.la] = np.clip(torque, model.actuator_ctrlrange[self.la, 0], model.actuator_ctrlrange[self.la, 1])
        data.ctrl[self.aa] = self.arm_target
        data.ctrl[self.ga] = -1.5 if self.gripper == 'open' else 0.

    def fallen(self, model, data):
        rot = data.xmat[self.base].reshape(3, 3)
        if rot[2, 2] < math.cos(math.radians(50)):
            return 'trunk tilt exceeds 50 degrees'
        if data.xpos[self.base, 2] < .12:
            if self.low_since is None:
                self.low_since = data.time
            if data.time-self.low_since >= .3:
                return 'trunk below 0.12 m for 0.3 s'
        else:
            self.low_since = None
        return None

    def state(self, model, data):
        _, roll, pitch, yaw = self.attitude(data)
        p = data.xpos[self.base]
        return dict(x=float(p[0]), y=float(p[1]), z=float(p[2]), yaw_deg=math.degrees(yaw),
                    roll_deg=math.degrees(roll), pitch_deg=math.degrees(pitch),
                    gripper=self.gripper, holding=self.holding, arm_stowed=self._arm_stowed,
                    grasp=data.site_xpos[self.site].tolist())

    def result(self, ctx, **fields):
        return json_ok(state=self.state(ctx.model, ctx.data), **fields)

    def stand(self, ctx):
        self.velocity[:] = 0
        self.turn_rate = 0.
        self.height = .30
        self.yaw_target = self.attitude(ctx.data)[3]
        ctx.step(1.)
        return self.result(ctx, stopped='standing')

    def stop(self, ctx):
        self.velocity[:] = 0
        self.turn_rate = 0.
        self.yaw_target = self.attitude(ctx.data)[3]
        ctx.step(.6)
        return self.result(ctx, stopped='stationary')

    def crouch(self, ctx, height=.22):
        height = number(height, .18, .32)
        self.velocity[:] = 0
        start = self.height
        for u in np.linspace(0, 1, 60):
            self.height = start+(height-start)*u
            ctx.step(.02)
        error = abs(float(ctx.data.xpos[self.base, 2])-height)
        if error > .035:
            return json_fail('Trunk height target not reached', height_target=height, error_m=error,
                             state=self.state(ctx.model, ctx.data))
        return self.result(ctx, height_target=height, error_m=error)

    def _move(self, ctx, target, speed, skip_folded=True):
        stowed = self._stow(ctx, skip_folded=skip_folded)
        if not stowed['ok']:
            return stowed
        self.height = .30
        origin = ctx.data.xpos[self.base, :2].copy()
        history = deque()
        blocked = False
        blocking_object = None
        try:
            while True:
                pos = ctx.data.xpos[self.base, :2]
                error = target-pos
                dist = float(np.linalg.norm(error))
                if dist < .035:
                    break
                history.append((float(ctx.data.time), dist, self._blocking_contacts(ctx)))
                while len(history) > 1 and history[1][0] <= ctx.data.time-1.5:
                    history.popleft()
                if ctx.data.time-history[0][0] >= 1.5 and history[0][1]-dist < .05:
                    blocked = True
                    contacts = Counter(oid for _, _, ids in history for oid in ids)
                    blocking_object = contacts.most_common(1)[0][0] if contacts else None
                    break
                rot = self.attitude(ctx.data)[0]
                world_v = error * min(speed, max(.15, 1.5*dist)) / max(dist, 1e-6)
                self.velocity[:] = (rot[:2, :2].T @ world_v)
                ctx.step(.04)
        finally:
            self.velocity[:] = 0
            self.turn_rate = 0.
        if blocked:
            try:
                self.stand(ctx)
            except _MotionTimeout:
                # Standing targets are already set; preserve the contact report
                # if the action budget cuts the recovery settling step short.
                pass
            position = ctx.data.xpos[self.base].tolist()
            distance_done = float(np.linalg.norm(ctx.data.xpos[self.base, :2]-origin))
            return json_fail(f'blocked by {blocking_object or "unknown obstacle"} after {distance_done:.2f} m',
                             position=position, blocking_object=blocking_object, distance_done=distance_done,
                             state=self.state(ctx.model, ctx.data))
        ctx.step(.6)
        error = float(np.linalg.norm(target-ctx.data.xpos[self.base, :2]))
        result = self.result(ctx, target=target.tolist(), error_m=error, stopped='target' if error < .1 else 'timeout')
        if error >= .1:
            return json_fail('Position target not reached', **{k:v for k,v in result.items() if k != 'ok'})
        return result

    def _navigate(self, ctx, target, speed, face_target=False):
        origin = ctx.data.xpos[self.base, :2].copy()
        initial = float(np.linalg.norm(target-origin))
        timeout = min(60., 2.5*initial/speed+3.)
        deadline = ctx.data.time+timeout
        # Skills can share a shorter remaining core action budget. Finish before
        # that guard so navigation reports a factual result rather than timeout.
        action_start = getattr(ctx, 'action_time', ctx.data.time)
        deadline = min(deadline, action_start+getattr(ctx, 'limit', 60.)-.01)
        deadline = min(deadline, getattr(ctx, 'deadline', deadline))
        timeout = max(0., deadline-ctx.data.time)
        bounded = _NavigationContext(ctx, deadline)
        fresh = self._last < 0
        try:
            self.height = .30
            delta = target-origin
            if face_target and initial > .065:
                angle = wrap(math.atan2(delta[1], delta[0])-self.attitude(ctx.data)[3])
                # Preserve the initial stance preparation after reset. Later
                # moves can proceed immediately when the arm is already folded.
                # Heading feedback finishes alignment while translation begins.
                result = self._turn(bounded, math.degrees(angle), settle=fresh,
                                    skip_folded=not fresh, tolerance_deg=5)
                if not result['ok']:
                    return dict(result, **self._navigation_fields(ctx, target, origin))
            result = self._move(bounded, target, speed, skip_folded=not fresh)
        except _MotionTimeout:
            self.height = .30
            self.yaw_target = self.attitude(ctx.data)[3]
            if np.linalg.norm(target-ctx.data.xpos[self.base, :2]) < .1:
                result = self.result(ctx, stopped='target')
            else:
                result = json_fail(f'Position target not reached before {timeout:g} s simulation timeout',
                                   stopped='timeout', state=self.state(ctx.model, ctx.data))
        finally:
            self.velocity[:] = 0
            self.turn_rate = 0.
        fields = self._navigation_fields(ctx, target, origin)
        if 'blocking_object' in result:
            result['error'] = (f'blocked by {result["blocking_object"] or "unknown obstacle"} '
                               f'after {fields["distance_done"]:.2f} m')
        return dict(result, **fields)

    def _navigation_fields(self, ctx, target, origin):
        position = ctx.data.xpos[self.base].copy()
        distance = float(np.linalg.norm(target-position[:2]))
        return dict(position=position.tolist(), target=target.tolist(),
                    distance_to_target=distance, error_m=distance,
                    distance_done=float(np.linalg.norm(position[:2]-origin)))

    def _blocking_contacts(self, ctx):
        """Fixed furniture/wall contacts with the trunk or legs, excluding the arm."""
        blockers = set()
        for contact in ctx.data.contact:
            for robot_geom, other_geom in ((contact.geom1, contact.geom2), (contact.geom2, contact.geom1)):
                body = ctx.model.body(ctx.model.geom_bodyid[robot_geom]).name
                if not body.startswith('robot/') or body.startswith('robot/arm/'):
                    continue
                other = ctx.model.geom(other_geom)
                name = other.name
                if ctx.model.body_weldid[other.bodyid[0]] != 0:
                    continue
                if name.startswith('obj/'):
                    blockers.add(name[4:].split('/', 1)[0])
                elif name.startswith('wall/'):
                    blockers.add(name)
        return sorted(blockers)

    def walk(self, ctx, distance, speed=.3, step_height=.08, gait_period=.4):
        distance, speed = number(distance, -10, 10), number(speed, .05, .6)
        self.step_height = number(step_height, .02, .12)
        self.period = number(gait_period, .3, .8)
        yaw = self.attitude(ctx.data)[3]
        self.yaw_target = yaw
        target = ctx.data.xpos[self.base, :2].copy()+distance*np.array([math.cos(yaw), math.sin(yaw)])
        return self._navigate(ctx, target, speed)

    def turn(self, ctx, angle_deg):
        return self._turn(ctx, angle_deg)

    def _turn(self, ctx, angle_deg, settle=True, skip_folded=False, tolerance_deg=2):
        angle = math.radians(number(angle_deg, -180, 180))
        stowed = self._stow(ctx, skip_folded=skip_folded)
        if not stowed['ok']:
            return stowed
        self.yaw_target = wrap(self.attitude(ctx.data)[3]+angle)
        self.velocity[:] = 0
        anchor = ctx.data.xpos[self.base, :2].copy()
        try:
            for _ in range(500):
                err = wrap(self.yaw_target-self.attitude(ctx.data)[3])
                if abs(err) < math.radians(tolerance_deg):
                    break
                self.turn_rate = np.clip(1.2*err, -.6, .6)
                correction = self.attitude(ctx.data)[0][:2, :2].T @ (anchor-ctx.data.xpos[self.base, :2])
                self.velocity[:] = np.clip(2*correction, -.18, .18)
                ctx.step(.04)
        finally:
            self.turn_rate = 0.
            self.velocity[:] = 0
        if settle:
            ctx.step(.6)
        error = math.degrees(wrap(self.yaw_target-self.attitude(ctx.data)[3]))
        if abs(error) > 10:
            return json_fail('Heading target not reached', error_deg=error, state=self.state(ctx.model, ctx.data))
        return self.result(ctx, error_deg=error)

    def walk_to(self, ctx, x, y, speed=.3):
        target = np.array([number(x, -15, 15), number(y, -15, 15)])
        speed = number(speed, .05, .6)
        return self._navigate(ctx, target, speed, face_target=True)

    def walk_path(self, ctx, points, speed=.3):
        if not isinstance(points, (list, tuple)) or not 1 <= len(points) <= 8:
            raise ValueError('points must contain 1 to 8 coordinate pairs')
        targets = []
        for point in points:
            if not isinstance(point, (list, tuple)) or len(point) != 2:
                raise ValueError('Each waypoint must be an [x, y] coordinate pair')
            targets.append(np.array([number(point[0], -15, 15), number(point[1], -15, 15)]))
        speed = number(speed, .05, .6)
        # The full path is one action, even though each segment also has the
        # distance-based walk_to bound. Validate all waypoints before stepping.
        bounded = _NavigationContext(ctx, ctx.data.time+59.99)
        distance = 0.
        for index, target in enumerate(targets):
            result = self._navigate(bounded, target, speed, face_target=True)
            distance += result['distance_done']
            result.update(waypoints_completed=index+int(result['ok']),
                          waypoint_index=index, path_distance_done=distance)
            if not result['ok']:
                return result
        return result

    def side_step(self, ctx, distance):
        distance = number(distance, -3, 3)
        yaw = self.attitude(ctx.data)[3]
        self.yaw_target = yaw
        target = ctx.data.xpos[self.base, :2].copy()+distance*np.array([-math.sin(yaw), math.cos(yaw)])
        return self._navigate(ctx, target, .15)

    def arm_stow(self, ctx):
        return self._stow(ctx)

    def _stow(self, ctx, skip_folded=False):
        error = _stow_error(ctx.data.qpos[self.aq])
        if skip_folded and self._arm_stowed and error < .04:
            return self.result(ctx, stopped='arm stowed', error_rad=error)
        self._arm_stowed = False
        start = self.arm_target.copy()
        for u in np.linspace(0, 1, 40):
            self.arm_target = start+(STOW-start)*u
            ctx.step(.02)
        reached = ctx.run_until(
            lambda: _stow_error(ctx.data.qpos[self.aq]) < .04, timeout=1.2)
        self._arm_stowed = bool(reached)
        if not reached:
            return json_fail('Arm did not reach stowed pose', state=self.state(ctx.model, ctx.data))
        return self.result(ctx, stopped='arm stowed',
                           error_rad=_stow_error(ctx.data.qpos[self.aq]))

    def arm_move_to(self, ctx, x, y, z, frame='world'):
        target = np.array([number(x, -15, 15), number(y, -15, 15), number(z, -.6 if frame == 'body' else .025, 1.2)])
        if frame not in ('world', 'body'):
            return json_fail('frame must be world or body')
        rot = self.attitude(ctx.data)[0]
        if frame == 'body':
            target = ctx.data.xpos[self.base]+rot@target
        if target[2] < .025:
            return json_fail('Target below minimum grasp height')
        if np.linalg.norm(target-ctx.data.xpos[self.base]) > .85:
            return json_fail('Target outside arm workspace')
        scratch = self.scratch
        scratch.qpos[:] = ctx.data.qpos
        q = self.arm_target.copy()
        for _ in range(150):
            scratch.qpos[self.aq] = q
            mujoco.mj_forward(ctx.model, scratch)
            poserr = target-scratch.site_xpos[self.site]
            axis = scratch.site_xmat[self.site].reshape(3, 3)[:, 0]
            orienterr = np.cross(axis, [0, 0, -1])
            if np.linalg.norm(poserr) < .003 and np.linalg.norm(orienterr) < .025:
                break
            mujoco.mj_jacSite(ctx.model, scratch, self.jac, self.jacr, self.site)
            jac = np.vstack([self.jac[:, self.av], .2*self.jacr[:, self.av]])
            err = np.r_[poserr, .2*orienterr]
            dq = jac.T@np.linalg.solve(jac@jac.T+.0001*np.eye(6), err)
            q = np.clip(q+np.clip(dq, -.15, .15), self.arange[:, 0], self.arange[:, 1])
        scratch.qpos[self.aq] = q
        mujoco.mj_forward(ctx.model, scratch)
        poserr = target-scratch.site_xpos[self.site]
        down = -scratch.site_xmat[self.site].reshape(3, 3)[2, 0]
        if np.linalg.norm(poserr) > .035 or down < .98:
            return json_fail('No reachable downward arm solution',
                             error_m=float(np.linalg.norm(poserr)))
        self._arm_stowed = False
        start = self.arm_target.copy()
        for u in np.linspace(0, 1, 80):
            self.arm_target = start+(q-start)*u
            ctx.step(.02)
        ctx.step(.4)
        error = float(np.linalg.norm(target-ctx.data.site_xpos[self.site]))
        return (self.result(ctx, error_m=error) if error < .04 else
                json_fail('Arm target not reached', error_m=error, state=self.state(ctx.model, ctx.data)))

    def open_gripper(self, ctx):
        released = ctx.release()
        self.holding = None
        self.gripper = 'open'
        ctx.step(.3)
        return self.result(ctx, released=released)

    def close_gripper(self, ctx):
        self.gripper = 'closed'
        if ctx.held() is not None:
            self.holding = ctx.held()
            ctx.step(.3)
            return self.result(ctx, held=ctx.held())
        ctx.step(.02)
        site = ctx.data.site_xpos[self.site]
        candidates = []
        for obj in ctx.objects():
            if obj.get('dynamic', True):
                pos, _ = ctx.object_pose(obj['id'])
                delta = ctx.data.site_xmat[self.site].reshape(3, 3).T@(pos-site)
                distance = float(np.linalg.norm(delta))
                if distance <= .04 and abs(delta[1]) <= .035 and abs(delta[2]) <= .035:
                    candidates.append((distance, obj['id']))
        if candidates:
            _, self.holding = min(candidates)
            ctx.hold(self.holding, 'robot/arm/grasp')
        ctx.step(.28)
        return self.result(ctx, held=ctx.held())

    def tools(self):
        def schema(**props):
            return dict(type='object', properties=props, additionalProperties=False)
        def num(low, high, default=None):
            result = dict(type='number', minimum=low, maximum=high)
            if default is not None:
                result['default'] = default
            return result
        descriptions = {
            'stand': 'Stand still / Mantenerse de pie', 'stop': 'Stop walking / Detener la marcha',
            'walk': 'Walk a signed distance / Caminar una distancia con signo',
            'turn': 'Turn in place / Girar en el sitio', 'walk_to': 'Walk to world coordinates / Caminar a coordenadas del mundo',
            'walk_path': 'Follow up to eight world waypoints / Seguir hasta ocho puntos del mundo',
            'side_step': 'Step sideways / Desplazarse de lado', 'crouch': 'Set trunk height / Ajustar altura del tronco',
            'arm_move_to': 'Move grasp site, pointing down / Mover agarre apuntando abajo',
            'arm_stow': 'Fold arm on back / Plegar brazo sobre el lomo',
            'open_gripper': 'Open and release / Abrir y soltar', 'close_gripper': 'Close with grasp assist / Cerrar con ayuda de agarre'}
        params = {name:schema() for name in descriptions}
        params.update(walk=schema(distance=num(-10,10), speed=num(.05,.6,.3), step_height=num(.02,.12,.08), gait_period=num(.3,.8,.4)),
                      turn=schema(angle_deg=num(-180,180)), walk_to=schema(x=num(-15,15), y=num(-15,15), speed=num(.05,.6,.3)),
                      walk_path=schema(points=dict(type='array', minItems=1, maxItems=8,
                                                   items=dict(type='array', minItems=2, maxItems=2, items=num(-15,15))),
                                       speed=num(.05,.6,.3)),
                      side_step=schema(distance=num(-3,3)), crouch=schema(height=num(.18,.32,.22)),
                      arm_move_to=schema(x=num(-15,15),y=num(-15,15),z=num(-.6,1.2),frame=dict(type='string',enum=['world','body'],default='world')))
        for name, required in {'walk':['distance'], 'turn':['angle_deg'], 'walk_to':['x','y'], 'walk_path':['points'], 'side_step':['distance'], 'arm_move_to':['x','y','z']}.items():
            params[name]['required'] = required
        return {name:ToolSpec(name, desc, params[name], getattr(self,name)) for name,desc in descriptions.items()}

    def card(self):
        return dict(id=self.id, name=self.name,
                    description={'en':'Quadruped carrying a six-axis arm; walks, turns, crouches and fetches small objects.',
                                 'es':'Cuadrúpedo con brazo de seis ejes; camina, gira, se agacha y recoge objetos pequeños.'},
                    sensors=['joint positions / posiciones articulares','trunk attitude / orientación del tronco'],
                    cameras=['robot/head','robot/arm'],
                    tools=[dict(name=t.name,description=t.description,params=t.params) for t in self.tools().values()]+[
                        dict(name='look',description='Observe camera / Observar cámara',params={'type':'object','properties':{'camera':{'type':'string','enum':['robot/head','robot/arm']}}})],
                    limits=dict(speed_m_s=.6, trunk_height_m=[.18,.32], arm_reach_m=.85, grasp_tolerance_m=.04,
                                body_footprint_m=[.72,.35], recommended_clearance_m=.35,
                                crouched_floor_reach_m=[.25,.60]),
                    notes=['Arm folds low on the back before walking. / El brazo se pliega sobre el lomo antes de caminar.',
                           'Graspable objects: diameter or width up to 0.12 m and mass up to 0.5 kg; bigger or heavier objects slip or push away. / Objetos agarrables: hasta 0,12 m de diámetro o ancho y 0,5 kg; los más grandes o pesados se resbalan o se empujan.',
                           'Walking into a loose object kicks it: stop 0.35-0.5 m before it and use look again. / Caminar sobre un objeto suelto lo patea: detente a 0,35-0,5 m y vuelve a mirar.',
                           'Standing footprint with folded arm is approximately 0.72 m long × 0.35 m wide; leave 0.35 m clearance from furniture. / La huella de pie con el brazo plegado mide unos 0.72 m de largo × 0.35 m de ancho; dejar 0.35 m de separación de los muebles.',
                           'At 0.22 m trunk height, measured floor reach is 0.25–0.60 m ahead of the trunk center (head camera: 0.31 m ahead). Measurements target the grasp site 0.025 m above the floor. / Con el tronco a 0.22 m, el alcance medido al suelo es de 0.25–0.60 m delante del centro del tronco (cámara de la cabeza: 0.31 m delante). Las mediciones sitúan el punto de agarre a 0.025 m sobre el suelo.',
                           'Grasp assist attaches a dynamic object within 4 cm of the grasp site between the fingers. / La ayuda de agarre sujeta objetos dinámicos a menos de 4 cm del punto de agarre entre los dedos.',
                           'Approach a floor object so it is 0.35–0.5 m in front of the trunk center, aligned with the heading, then crouch to 0.22 m and arm_move_to; move above it, lower to its center, close, then stow. / Acercarse a un objeto del suelo hasta dejarlo a 0.35–0.5 m delante del centro del tronco, alineado con la orientación, luego agacharse a 0.22 m y usar arm_move_to; mover encima, bajar al centro, cerrar y plegar.',
                           'Walking follows straight segments; inspect obstacles with look and route around them with walk_path. Blocked motion stops and reports the contacted furniture or wall id. / La marcha sigue segmentos rectos; inspeccionar obstáculos con look y rodearlos con walk_path. Si se bloquea, se detiene e informa del identificador del mueble o pared en contacto.',
                           'World coordinates are meters; turn angles are degrees. / Coordenadas en metros; giros en grados.'])
