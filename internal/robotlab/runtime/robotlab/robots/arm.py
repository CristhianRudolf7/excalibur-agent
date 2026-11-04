"""Low-level table arm targets, Cartesian IK and bilateral finger-pad grasp assist."""
from __future__ import annotations

import math
import numbers

import mujoco
import numpy as np

from .arm_kinematics import ArmIK, down_rotation, grasp_angles, grasp_rotation
from .arm_model import attach_model
from .base import ActionInterrupted, Robot, Spawn, ToolSpec, json_fail, json_ok

_JOINTS = ('shoulder_pan', 'shoulder_lift', 'elbow', 'wrist_1', 'wrist_2', 'wrist_3')
_LOW = np.array([-0.6, -0.4, 0.76])
_HIGH = np.array([0.6, 0.4, 1.65])


def _number(value, name):
    if isinstance(value, bool) or not isinstance(value, numbers.Real):
        raise ValueError(name + ' must be a finite number')
    try:
        value = float(value)
    except OverflowError:
        raise ValueError(name + ' must be a finite number') from None
    if not math.isfinite(value):
        raise ValueError(name + ' must be a finite number')
    return value


def _bounded(value, name, low, high):
    if value is None:
        raise ValueError(f'{name} is required in every keyframe')
    value = _number(value, name)
    if not low <= value <= high:
        raise ValueError(f'{name} must be between {low:g} and {high:g}')
    return value


def _schema(properties=None, required=()):
    return {'type': 'object', 'properties': properties or {}, 'required': list(required),
            'additionalProperties': False}


def _scalar(low, high, default=None):
    out = {'type': 'number', 'minimum': low, 'maximum': high}
    if default is not None:
        out['default'] = default
    return out


def _smooth(t):
    return t ** 3 * (10 - 15 * t + 6 * t * t)


def _grip_control(opening):
    """Convert aperture to the parallel linkage's driver angle, then control units."""
    if opening in (0., 1.):
        return (1. - opening) * 255.
    # Inner pad gap = 2 * (55*cos(theta) - 37.5*sin(theta) - 12.3) mm.
    angle = math.acos((.0123 + .085 * opening / 2) / math.hypot(.055, .0375))
    angle -= math.atan2(.0375, .055)
    return float(np.clip(angle / .8 * 255., 0., 255.))


def _rotation_between(start, end, fraction):
    a, b = np.empty(4), np.empty(4)
    mujoco.mju_mat2Quat(a, start.ravel())
    mujoco.mju_mat2Quat(b, end.ravel())
    dot = float(a @ b)
    if dot < 0:
        b, dot = -b, -dot
    if dot > 0.9995:
        q = (1 - fraction) * a + fraction * b
    else:
        angle = math.acos(min(1., dot))
        q = (math.sin((1 - fraction) * angle) * a + math.sin(fraction * angle) * b) / math.sin(angle)
    q /= np.linalg.norm(q)
    mat = np.empty(9)
    mujoco.mju_quat2Mat(mat, q)
    return mat.reshape(3, 3)


class _ContactStop(Exception):
    pass


class ArmRobot(Robot):
    id = 'arm'
    name = {'en': 'Arm', 'es': 'Brazo'}
    world_kind = 'table'
    default_camera = 'robot/wrist'

    def __init__(self):
        # Cards are also requested by the registry before a world is compiled.
        self.limits = np.array([[-6.2831, 6.2831]] * 6)
        self.limits[2] = [-3.1415, 3.1415]
        self._home_position = np.array([-.15, 0., 1.15])
        self._home_q = np.deg2rad([16.858, -80.4347, -112.6922, -76.8731, 90., 16.858])

    def attach(self, spec, spawn: Spawn, menagerie):
        self.spawn = spawn
        attach_model(spec, spawn, menagerie)

    def bind(self, model, data):
        self.model, self.data = model, data
        joints = [model.joint('robot/' + name + '_joint').id for name in _JOINTS]
        self.qadr = model.jnt_qposadr[joints].copy()
        self.dofs = model.jnt_dofadr[joints].copy()
        self.limits = model.jnt_range[joints].copy()
        self.act = np.array([model.actuator('robot/' + name).id for name in _JOINTS])
        self.limits[:, 0] = np.maximum(self.limits[:, 0], model.actuator_ctrlrange[self.act, 0])
        self.limits[:, 1] = np.minimum(self.limits[:, 1], model.actuator_ctrlrange[self.act, 1])
        self.grip_act = model.actuator('robot/gripper/fingers_actuator').id
        self.grip_dofs = [model.joint('robot/gripper/' + side + '_driver_joint').dofadr[0]
                          for side in ('left', 'right')]
        self.site = model.site('robot/grasp').id
        self._ik = ArmIK(model, self.site, self.qadr, self.dofs, self.limits)
        self._table_geoms = {g for g in range(model.ngeom)
                             if model.geom(g).name in ('table', 'obj/table', 'table/top')}
        self._robot_geoms = {g for g in range(model.ngeom)
                             if model.body(model.geom_bodyid[g]).name.startswith('robot/')
                             and model.geom(g).name != 'robot/pedestal'}
        self._finger_geoms = {g for g in self._robot_geoms
                              if '/gripper/' in model.body(model.geom_bodyid[g]).name
                              and model.body(model.geom_bodyid[g]).name.rsplit('/', 1)[-1]
                              not in ('base', 'base_mount')}
        self._pads = {side: {model.geom(f'robot/gripper/{side}_pad{i}').id for i in (1, 2)}
                      for side in ('left', 'right')}
        self._objects = {}
        for b in range(model.nbody):
            if not model.body(b).name.startswith('obj/'):
                continue
            geoms = np.flatnonzero(model.geom_bodyid == b)
            if not len(geoms):
                continue
            g = geoms[0]
            kind, size = model.geom_type[g], model.geom_size[g]
            if kind == mujoco.mjtGeom.mjGEOM_SPHERE:
                full = [2 * size[0]]
            elif kind in (mujoco.mjtGeom.mjGEOM_CYLINDER, mujoco.mjtGeom.mjGEOM_CAPSULE):
                full = [2 * size[0], 2 * size[1] + (2 * size[0] if kind == mujoco.mjtGeom.mjGEOM_CAPSULE else 0)]
            else:
                low = np.min(model.geom_pos[geoms] - model.geom_size[geoms], axis=0)
                high = np.max(model.geom_pos[geoms] + model.geom_size[geoms], axis=0)
                full = (high - low).tolist()
            self._objects[model.body(b).name[4:]] = {
                'body': b, 'size': [round(float(v), 3) for v in full],
                'dynamic': bool(model.body_jntnum[b])}
        self._velocity = np.zeros(6)
        self._holding = None

    def reset(self, model, data):
        # These controller fields are already included in core snapshots.
        self._holding, self._gripper, self._grip_target = None, {'opening': 1., 'force': 1.}, 0.
        self._ik.data.qpos[:] = data.qpos
        self._home_position = np.array([-0.15, 0., 1.15])
        seeds = ([0, -math.pi / 2, -math.pi / 2, -math.pi / 2, math.pi / 2, 0],
                 [-math.pi / 2, -math.pi / 2, math.pi / 2, -math.pi / 2, -math.pi / 2, 0],
                 [0, -2., 2., -1.5, 1.5, 0])
        home = None
        for seed in seeds:
            home = self._ik.solve(self._home_position, down_rotation(0), seed, 300)
            if home is not None:
                break
        if home is None:
            raise ValueError('arm spawn cannot reach the home position over the table')
        self._home_q = home.copy()
        self._target = home.copy()
        data.qpos[self.qadr] = home
        data.qvel[self.dofs] = 0
        self.control(model, data)

    def control(self, model, data):
        if isinstance(self._gripper, str):
            # Older failure snapshots stored the command as open/closed.
            self._gripper = {'opening': 1. if self._gripper == 'open' else 0., 'force': 1.}
        data.ctrl[self.act] = self._target
        data.ctrl[self.grip_act] = self._grip_target
        model.actuator_forcerange[self.grip_act] = np.array([-5., 5.]) * self._gripper['force']
        data.qfrc_applied[self.dofs] = data.qfrc_bias[self.dofs]

    def _contact(self, data, penetration=0.):
        return any(c.dist <= -penetration and (not penetration or c.dist < -penetration)
                   and ((c.geom1 in self._table_geoms and c.geom2 in self._robot_geoms)
                        or (c.geom2 in self._table_geoms and c.geom1 in self._robot_geoms))
                   for c in data.contact)

    def fallen(self, model, data):
        return 'collision' if self._contact(data, 0.02) else None

    def safety(self, model, data):
        if self._contact(data):
            return 'table_collision'
        if not self._in_workspace(data.site_xpos[self.site]):
            return 'workspace'
        mujoco.mj_objectVelocity(model, data, mujoco.mjtObj.mjOBJ_SITE, self.site, self._velocity, 0)
        if np.linalg.norm(self._velocity[3:]) > 0.5:
            return 'tcp_speed'
        return None

    def _pad_contacts(self, data):
        contacts = {side: set() for side in self._pads}
        for c in data.contact:
            if c.dist > 0:
                continue
            for side, pads in self._pads.items():
                other = c.geom2 if c.geom1 in pads else c.geom1 if c.geom2 in pads else None
                if other is not None:
                    body = self.model.body(self.model.geom_bodyid[other]).name
                    if body.startswith('obj/'):
                        contacts[side].add(body[4:])
        return contacts

    def _opening_m(self, data):
        centers = [np.mean(data.geom_xpos[list(self._pads[side])], axis=0)
                   for side in ('left', 'right')]
        # Pad collision boxes have 4 mm half-thickness along the closing axis.
        axis = data.site_xmat[self.site].reshape(3, 3)[:, 1]
        return float(np.clip(abs((centers[0] - centers[1]) @ axis) - .008, 0., .085))

    def _unexpected_contacts(self, data):
        out = set()
        for c in data.contact:
            if c.dist > 0:
                continue
            for robot, other in ((c.geom1, c.geom2), (c.geom2, c.geom1)):
                if (robot in self._robot_geoms and robot not in self._finger_geoms
                        and other not in self._robot_geoms
                        and self.model.geom(other).name != 'robot/pedestal'):
                    out.add((int(robot), int(other)))
        return out

    def state(self, model, data):
        yaw, pitch, roll = grasp_angles(data.site_xmat[self.site].reshape(3, 3))
        pads = self._pad_contacts(data)
        opening = self._opening_m(data)
        objects = []
        for oid, info in self._objects.items():
            rot = data.xmat[info['body']].reshape(3, 3)
            y, p, r = grasp_angles(rot @ down_rotation(0))
            objects.append({'id': oid, 'pos': data.xpos[info['body']].round(3).tolist(),
                            'rpy_deg': [round(v, 1) for v in (r, p, y)],
                            'size': info['size'], 'dynamic': info['dynamic']})
        contacts = [{'link': model.body(model.geom_bodyid[a]).name,
                     'object': model.body(model.geom_bodyid[b]).name.removeprefix('obj/')
                     or model.geom(b).name}
                    for a, b in sorted(self._unexpected_contacts(data))]
        return {'joints_deg': dict(zip(_JOINTS, np.rad2deg(data.qpos[self.qadr]).round(1).tolist())),
                'gripper': {'pos': data.site_xpos[self.site].round(3).tolist(),
                            'yaw_deg': round(yaw, 1), 'pitch_deg': round(pitch, 1),
                            'roll_deg': round(roll, 1), 'opening': round(opening / .085, 3),
                            'opening_m': round(opening, 3),
                            'contacts': {side: next(iter(sorted(ids)), None) for side, ids in pads.items()}},
                'held': self._holding, 'objects': objects, 'contacts': contacts}

    def base_xy(self, model, data):
        return self.spawn.x, self.spawn.y

    def _result(self, ctx, error=None, **fields):
        self._holding = ctx.held()
        result = {'state': self.state(ctx.model, ctx.data), **fields}
        return json_fail(error, **result) if error else json_ok(**result)

    @staticmethod
    def _in_workspace(pos):
        return bool(np.all(pos >= _LOW - 1e-9) and np.all(pos <= _HIGH + 1e-9))

    def _freeze(self, ctx):
        self._target = ctx.data.qpos[self.qadr].copy()
        self._holding = ctx.held()
        self._grip_target = float(ctx.data.ctrl[self.grip_act])

    def _tick(self, ctx, baseline=None):
        def stop_contact():
            if baseline is not None and self._unexpected_contacts(ctx.data) - baseline:
                raise _ContactStop()
        stop_contact()
        reason = self.safety(ctx.model, ctx.data)
        if reason:
            raise ActionInterrupted('safety', reason)
        try:
            ctx.step(ctx.model.opt.timestep)
        except ActionInterrupted as exc:
            if exc.reason in ('safety', 'fell'):
                stop_contact()
            raise
        if ctx.held():
            width = self._held_width(ctx)
            if (self._gripper['opening'] * .085 > width
                    and self._opening_m(ctx.data) > width + .001):
                ctx.release()
                self._holding = None
        stop_contact()
        reason = self.safety(ctx.model, ctx.data)
        if reason:
            raise ActionInterrupted('safety', reason)

    def _pose(self, q):
        self._ik.forward(q)
        return (self._ik.data.site_xpos[self.site].copy(),
                self._ik.data.site_xmat[self.site].reshape(3, 3).copy())

    def _cart_target(self, args, start):
        pos = np.array([_number(args.get(a), a) for a in ('x', 'y', 'z')])
        yaw = grasp_angles(start)[0]
        angles = [_number(args.get(k, default), k) for k, default in
                  (('yaw', yaw), ('pitch', 0.), ('roll', 0.))]
        if not self._in_workspace(pos):
            raise ValueError('target outside table workspace')
        return pos, grasp_rotation(*np.deg2rad(angles))

    def _prepare(self, frame, q):
        if not isinstance(frame, dict):
            raise ValueError('keyframe must be an object')
        kind = frame.get('type')
        allowed = {'joints': {'joints'},
                   'gripper_to': {'x', 'y', 'z', 'yaw', 'pitch', 'roll', 'path'},
                   'gripper': {'opening', 'force'}}
        if not isinstance(kind, str) or kind not in allowed:
            raise ValueError('unknown keyframe type')
        if set(frame) - allowed[kind] - {'type', 'duration'}:
            raise ValueError('unknown keyframe fields')
        duration = _bounded(frame.get('duration', 1.2 if kind == 'gripper' else None), 'duration', .002, 30.)
        n = max(1, math.ceil(duration / .02))
        if kind == 'gripper':
            opening = _bounded(frame.get('opening'), 'opening', 0., 1.)
            force = _bounded(frame.get('force', 1.), 'force', 0., 1.)
            return {'type': kind, 'duration': duration, 'opening': opening, 'force': force}, q
        start_pos, start_rot = self._pose(q)
        path = []
        if kind == 'joints':
            joints = frame.get('joints')
            if not isinstance(joints, dict) or not joints or set(joints) - set(_JOINTS):
                raise ValueError('joints must be a nonempty subset of the six joint names')
            target = q.copy()
            for name, value in joints.items():
                j = _JOINTS.index(name)
                target[j] = math.radians(_number(value, name))
                if not self.limits[j, 0] <= target[j] <= self.limits[j, 1]:
                    raise ValueError(name + ' outside joint limits')
            for i in range(1, n + 1):
                qi = q + _smooth(i / n) * (target - q)
                if not self._in_workspace(self._pose(qi)[0]):
                    raise ValueError('joint path outside table workspace')
                path.append(qi)
        else:
            target_pos, target_rot = self._cart_target(frame, start_rot)
            mode = frame.get('path', 'line')
            if mode not in ('line', 'joint'):
                raise ValueError('path must be line or joint')
            if mode == 'joint':
                target = self._ik.solve(target_pos, target_rot, q, 300)
                if target is None:
                    raise ValueError('target unreachable or outside joint limits')
                for i in range(1, n + 1):
                    qi = q + _smooth(i / n) * (target - q)
                    if not self._in_workspace(self._pose(qi)[0]):
                        raise ValueError('joint path outside table workspace')
                    path.append(qi)
            else:
                qi = q.copy()
                for i in range(1, n + 1):
                    s = _smooth(i / n)
                    point = start_pos + s * (target_pos - start_pos)
                    qi = self._ik.solve(point, _rotation_between(start_rot, target_rot, s), qi)
                    if qi is None:
                        raise ValueError('target unreachable along Cartesian path')
                    path.append(qi.copy())
        return {'type': kind, 'duration': duration, 'path': np.array([q.copy()] + path)}, path[-1]

    def _held_width(self, ctx):
        """Projected object width along the actual pad closing axis."""
        oid = ctx.held()
        if not oid or oid not in self._objects:
            return 0.
        b = self._objects[oid]['body']
        axis = ctx.data.site_xmat[self.site].reshape(3, 3)[:, 1]
        low, high = math.inf, -math.inf
        for g in np.flatnonzero(self.model.geom_bodyid == b):
            local_axis = ctx.data.geom_xmat[g].reshape(3, 3).T @ axis
            kind, size = self.model.geom_type[g], self.model.geom_size[g]
            if kind == mujoco.mjtGeom.mjGEOM_BOX:
                radius = float(np.abs(local_axis) @ size)
            elif kind == mujoco.mjtGeom.mjGEOM_SPHERE:
                radius = size[0]
            elif kind == mujoco.mjtGeom.mjGEOM_CYLINDER:
                radius = size[0] * np.linalg.norm(local_axis[:2]) + size[1] * abs(local_axis[2])
            else:
                radius = size[0] + size[1] * abs(local_axis[2])
            center = ctx.data.geom_xpos[g] @ axis
            low, high = min(low, center - radius), max(high, center + radius)
        return float(high - low)

    def _grasp(self, ctx):
        pads = self._pad_contacts(ctx.data)
        common = pads['left'] & pads['right']
        candidates = sorted(oid for oid in common if self._objects[oid]['dynamic'])
        if not ctx.held() and candidates:
            ctx.hold(candidates[0], 'robot/grasp')
        self._holding = ctx.held()

    def _run_frame(self, ctx, plan, baseline):
        n = max(1, round(plan['duration'] / ctx.model.opt.timestep))
        if plan['type'] == 'gripper':
            start = self._grip_target
            target = _grip_control(plan['opening'])
            self._gripper = {'opening': plan['opening'], 'force': plan['force']}
            for i in range(1, n + 1):
                self._grip_target = start + _smooth(i / n) * (target - start)
                self._tick(ctx, baseline)
            if target >= start:
                self._grasp(ctx)
        else:
            path = plan['path']
            for i in range(1, n + 1):
                sample = i / n * (len(path) - 1)
                j = min(int(sample), len(path) - 2)
                self._target = path[j] + (sample - j) * (path[j + 1] - path[j])
                self._tick(ctx, baseline)

    def _motion(self, ctx, keyframes, settle=.2, stop_if=None):
        plans, details = [], []
        try:
            settle = _bounded(settle, 'settle', 0., 2.)
            if not isinstance(keyframes, list) or not 1 <= len(keyframes) <= 16:
                raise ValueError('keyframes must contain 1 to 16 entries')
            if stop_if is not None and (not isinstance(stop_if, dict)
                    or set(stop_if) - {'contact'} or not isinstance(stop_if.get('contact', False), bool)):
                raise ValueError('stop_if accepts only contact: boolean')
            self._ik.data.qpos[:] = ctx.data.qpos
            q = self._target.copy()
            for k, frame in enumerate(keyframes):
                try:
                    plan, q = self._prepare(frame, q)
                    plans.append(plan)
                except ValueError as exc:
                    details.append({'keyframe': k, 'error': str(exc)})
            if details:
                return self._result(ctx, 'invalid motion plan', details=details)
            if sum(p['duration'] for p in plans) + settle > 30. + 1e-9:
                raise ValueError('total duration including settle exceeds 30 seconds')
        except ValueError as exc:
            return self._result(ctx, str(exc), details=details)
        baseline = self._unexpected_contacts(ctx.data) if stop_if and stop_if.get('contact') else None
        k = 0
        try:
            for k, plan in enumerate(plans):
                self._run_frame(ctx, plan, baseline)
            for _ in range(round(settle / ctx.model.opt.timestep)):
                self._tick(ctx, baseline)
        except _ContactStop:
            return self._result(ctx, stopped_at=k, reason='contact')
        except ActionInterrupted:
            self._freeze(ctx)
            raise
        return self._result(ctx)

    def move_joints(self, ctx, joints, duration, settle=.2):
        return self._motion(ctx, [dict(type='joints', joints=joints, duration=duration)], settle)

    def move_gripper_to(self, ctx, x, y, z, yaw=None, pitch=0., roll=0., path='line', duration=None, settle=.2):
        frame = dict(type='gripper_to', x=x, y=y, z=z, pitch=pitch, roll=roll, path=path, duration=duration)
        if yaw is not None:
            frame['yaw'] = yaw
        return self._motion(ctx, [frame], settle)

    def gripper(self, ctx, opening, force=1., duration=1.2):
        return self._motion(ctx, [dict(type='gripper', opening=opening, force=force, duration=duration)])

    def sequence(self, ctx, keyframes, stop_if=None, settle=.2):
        return self._motion(ctx, keyframes, settle, stop_if)

    def ik(self, ctx, x, y, z, yaw=None, pitch=0., roll=0.):
        try:
            self._ik.data.qpos[:] = ctx.data.qpos
            args = dict(x=x, y=y, z=z, pitch=pitch, roll=roll)
            if yaw is not None:
                args['yaw'] = yaw
            pos, rot = self._cart_target(args, ctx.data.site_xmat[self.site].reshape(3, 3))
            q = self._ik.solve(pos, rot, self._target, 300)
            if q is None:
                raise ValueError('target unreachable or outside joint limits')
            margins = np.rad2deg(np.minimum(q - self.limits[:, 0], self.limits[:, 1] - q))
            return self._result(ctx, joints_deg=dict(zip(_JOINTS, np.rad2deg(q).round(1).tolist())),
                                reachable=True, limit_margin_deg=dict(zip(_JOINTS, margins.round(1).tolist())))
        except ValueError as exc:
            return self._result(ctx, str(exc), reachable=False, joints_deg=None, limit_margin_deg=None,
                                details=[{'target': 'grasp', 'error': str(exc)}])

    def wait(self, ctx, seconds):
        try:
            seconds = _bounded(seconds, 'seconds', .05, 5.)
        except ValueError as exc:
            return self._result(ctx, str(exc), details=[])
        try:
            for _ in range(round(seconds / ctx.model.opt.timestep)):
                self._tick(ctx)
        except ActionInterrupted:
            self._freeze(ctx)
            raise
        return self._result(ctx)

    def tools(self):
        timing = {'duration': _scalar(.002, 30), 'settle': _scalar(0, 2, .2)}
        joints = _schema({name: _scalar(*np.rad2deg(self.limits[j])) for j, name in enumerate(_JOINTS)})
        joints['minProperties'] = 1
        cart = {'x': _scalar(-.6, .6), 'y': _scalar(-.4, .4), 'z': _scalar(.76, 1.65),
                'yaw': {'type': 'number'}, 'pitch': _scalar(-90, 90, 0), 'roll': _scalar(-180, 180, 0),
                'path': {'type': 'string', 'enum': ['joint', 'line'], 'default': 'line'}}
        grip = {'opening': _scalar(0, 1), 'force': _scalar(0, 1, 1), 'duration': _scalar(.002, 30, 1.2)}
        frames = []
        for kind, props, required in (
            ('joints', {'joints': joints, 'duration': timing['duration']}, ('joints', 'duration')),
            ('gripper_to', {**cart, 'duration': timing['duration']}, ('x', 'y', 'z', 'duration')),
            ('gripper', grip, ('opening', 'duration'))):
            frames.append(_schema({'type': {'const': kind}, **props}, ('type', *required)))
        definitions = [
            ('move_joints', 'Interpolate joint targets in degrees. / Interpolar objetivos articulares en grados.',
             _schema({'joints': joints, **timing}, ('joints', 'duration')), self.move_joints),
            ('move_gripper_to', 'Move the pinch point; angles are degrees, zero points down. / Mover el punto de agarre; ángulos en grados, cero apunta abajo.',
             _schema({**cart, **timing}, ('x', 'y', 'z', 'duration')), self.move_gripper_to),
            ('gripper', 'Set opening and force fractions; report pad contacts. / Fijar apertura y fuerza relativas; informar contactos de las almohadillas.',
             _schema(grip, ('opening',)), self.gripper),
            ('sequence', 'Validate all keyframes, then execute in order; stop on new non-finger contact. / Validar todos los fotogramas y ejecutarlos en orden; parar ante un contacto nuevo fuera de los dedos.',
             _schema({'keyframes': {'type': 'array', 'minItems': 1, 'maxItems': 16, 'items': {'oneOf': frames}},
                      'stop_if': _schema({'contact': {'type': 'boolean'}}), 'settle': timing['settle']}, ('keyframes',)), self.sequence),
            ('ik', 'Compute joints and limit margins without advancing time. / Calcular articulaciones y márgenes sin avanzar el tiempo.',
             _schema({k: v for k, v in cart.items() if k != 'path'}, ('x', 'y', 'z')), self.ik),
            ('wait', 'Hold current targets for simulation seconds. / Mantener los objetivos durante segundos simulados.',
             _schema({'seconds': _scalar(.05, 5)}, ('seconds',)), self.wait),
        ]
        return {name: ToolSpec(name, desc, params, fn) for name, desc, params, fn in definitions}

    def card(self):
        tools = [{'name': t.name, 'description': t.description, 'params': t.params} for t in self.tools().values()]
        tools.append({'name': 'look', 'description': 'Inspect a robot camera. / Observar una cámara del robot.',
                      'params': _schema({'camera': {'type': 'string', 'enum': ['robot/wrist', 'robot/overhead']}})})
        return {'id': self.id, 'name': self.name,
                'description': 'Table arm with parallel fingers. / Brazo de mesa con dedos paralelos.',
                'sensors': ['joint_position', 'grasp_pose', 'finger_contacts', 'object_tracker', 'held', 'contacts'],
                'cameras': ['robot/wrist', 'robot/overhead'], 'tools': tools,
                'joints': {name: {'limits_deg': np.rad2deg(self.limits[j]).round(1).tolist()}
                           for j, name in enumerate(_JOINTS)},
                'link_lengths_m': {'upper_arm': .6127, 'forearm': .57155, 'gripper': .1558},
                'reference_pose': {'joints_deg': dict(zip(_JOINTS, np.rad2deg(self._home_q).round(1).tolist())),
                                   'grasp_pos': self._home_position.tolist(), 'yaw': 0, 'pitch': 0, 'roll': 0,
                                   'opening': 1},
                'limits': {'workspace_m': {'min': _LOW.tolist(), 'max': _HIGH.tolist()},
                           'max_tcp_speed_m_s': .5, 'table_height_m': .75, 'opening_m': .085,
                           'max_call_duration_s': 30},
                'notes': [
                    'Plan small keyframes and check ik first; every sequence is validated before physics. / Planificar fotogramas pequeños y comprobar ik primero; cada secuencia se valida antes de simular.',
                    'Use line for a straight Cartesian path, joint for joint interpolation. Duration is simulation seconds; settle defaults to 0.2 s. / Usar line para trayectoria cartesiana recta, joint para interpolación articular. duration son segundos simulados; settle vale 0.2 s por defecto.',
                    'Yaw, pitch, roll use world Z-Y-X rotations from fingers down; omitted yaw keeps its target. / yaw, pitch, roll usan rotaciones globales Z-Y-X desde dedos abajo; omitir yaw mantiene su objetivo.',
                    'Opening 1 is 85 mm, 0 is closed. Assist requires both pads touching the same dynamic object at the end of closing. / Apertura 1 son 85 mm, 0 es cerrado. La asistencia exige ambas almohadillas tocando el mismo objeto dinámico al terminar de cerrar.',
                    'Opening beyond the held object width releases it; placement needs a level object centered over its support. / Abrir más que el ancho del objeto lo suelta; colocarlo exige nivelarlo y centrarlo sobre su apoyo.',
                    'stop_if.contact ignores finger and existing contacts; stopped_at counts keyframes from zero. Keep motion below 0.5 m/s. / stop_if.contact ignora dedos y contactos existentes; stopped_at cuenta desde cero. Mantener el movimiento por debajo de 0.5 m/s.',
                ]}
