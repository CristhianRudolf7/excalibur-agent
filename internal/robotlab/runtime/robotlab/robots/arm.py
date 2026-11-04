"""Deterministic table-mounted arm tools with Cartesian IK and grasp assist."""
from __future__ import annotations

import math
import numbers

import mujoco
import numpy as np

from .arm_kinematics import ArmIK, down_rotation, rotation_error
from .arm_model import attach_model
from .base import ActionInterrupted, Robot, Spawn, ToolSpec, json_fail, json_ok

_JOINTS = ('shoulder_pan', 'shoulder_lift', 'elbow', 'wrist_1', 'wrist_2', 'wrist_3')
_LOW = np.array([-0.6, -0.4, 0.76])
_HIGH = np.array([0.6, 0.4, 1.65])


def _number(value, name):
    if isinstance(value, bool) or not isinstance(value, numbers.Real) or not math.isfinite(value):
        raise ValueError(name + ' must be a finite number')
    return float(value)


def _speed(value, maximum, minimum):
    value = _number(value, 'speed')
    if value <= 0:
        raise ValueError('speed must be positive')
    return max(minimum, min(value, maximum))


def _schema(properties=None, required=()):
    return {'type': 'object', 'properties': properties or {}, 'required': list(required),
            'additionalProperties': False}


def _scalar(low, high, default=None):
    out = {'type': 'number', 'minimum': low, 'maximum': high}
    if default is not None:
        out['default'] = default
    return out


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


class ArmRobot(Robot):
    id = 'arm'
    name = {'en': 'Arm', 'es': 'Brazo'}
    world_kind = 'table'
    default_camera = 'robot/wrist'

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
        self.ik = ArmIK(model, self.site, self.qadr, self.dofs, self.limits)
        self._table_geoms = {g for g in range(model.ngeom)
                             if model.geom(g).name in ('table', 'obj/table')}
        self._robot_geoms = {g for g in range(model.ngeom)
                             if model.body(model.geom_bodyid[g]).name.startswith('robot/')
                             and model.geom(g).name != 'robot/pedestal'}
        self._finger_geoms = {g for g in self._robot_geoms
                              if '/gripper/' in model.body(model.geom_bodyid[g]).name}
        self._object_geoms = {g for g in range(model.ngeom)
                              if model.body(model.geom_bodyid[g]).name.startswith('obj/')}
        self._velocity = np.zeros(6)
        self._holding = None
        self._gripper = 'open'

    def reset(self, model, data):
        self._holding, self._gripper, self._grip_target = None, 'open', 0.
        self.ik.data.qpos[:] = data.qpos
        self._home_position = np.array([-0.15, 0., 1.15])
        seeds = ([0, -math.pi / 2, -math.pi / 2, -math.pi / 2, math.pi / 2, 0],
                 [-math.pi / 2, -math.pi / 2, math.pi / 2, -math.pi / 2, -math.pi / 2, 0],
                 [0, -2., 2., -1.5, 1.5, 0])
        for seed in seeds:
            home = self.ik.solve(self._home_position, down_rotation(0), seed, 300)
            if home is not None:
                break
        if home is None:
            raise ValueError('arm spawn cannot reach the home position over the table')
        self._home_q = home.copy()
        self._target = home.copy()
        data.qpos[self.qadr] = home
        data.qvel[self.dofs] = 0
        data.ctrl[self.act] = home
        data.ctrl[self.grip_act] = 0

    def control(self, model, data):
        data.ctrl[self.act] = self._target
        data.ctrl[self.grip_act] = self._grip_target
        data.qfrc_applied[self.dofs] = data.qfrc_bias[self.dofs]

    def _contact(self, data, penetration=0.):
        for c in data.contact:
            if c.dist > -penetration or (penetration and c.dist == -penetration):
                continue
            if ((c.geom1 in self._table_geoms and c.geom2 in self._robot_geoms) or
                    (c.geom2 in self._table_geoms and c.geom1 in self._robot_geoms)):
                return True
        return False

    def fallen(self, model, data):
        return 'collision' if self._contact(data, 0.02) else None

    def safety(self, model, data):
        if self._contact(data):
            return 'table_collision'
        if not self._in_workspace(data.site_xpos[self.site]):
            return 'workspace'
        mujoco.mj_objectVelocity(model, data, mujoco.mjtObj.mjOBJ_SITE,
                                 self.site, self._velocity, 0)
        if np.linalg.norm(self._velocity[3:]) > 0.5:
            return 'tcp_speed'
        return None

    def state(self, model, data):
        pos = data.site_xpos[self.site]
        rot = data.site_xmat[self.site].reshape(3, 3)
        return {'x': float(pos[0]), 'y': float(pos[1]), 'z': float(pos[2]),
                'yaw_deg': math.degrees(math.atan2(rot[1, 0], rot[0, 0])),
                'q_deg': np.rad2deg(data.qpos[self.qadr]).round(3).tolist(),
                'gripper': self._gripper, 'holding': self._holding}

    def base_xy(self, model, data):
        return self.spawn.x, self.spawn.y

    def _step(self, ctx, seconds):
        try:
            for _ in range(max(1, round(seconds / ctx.model.opt.timestep))):
                if self._contact(ctx.data):
                    raise ActionInterrupted('safety', 'table_collision')
                ctx.step(ctx.model.opt.timestep)
                if self._contact(ctx.data):
                    raise ActionInterrupted('safety', 'table_collision')
                reason = self.safety(ctx.model, ctx.data)
                if reason:
                    raise ActionInterrupted('safety', reason)
        except BaseException:
            self._target = ctx.data.qpos[self.qadr].copy()
            raise

    def _result(self, ctx, target=None, **fields):
        state = self.state(ctx.model, ctx.data)
        result = {'pose': {k: state[k] for k in ('x', 'y', 'z', 'yaw_deg')},
                  'q_deg': state['q_deg'], 'holding': ctx.held(), **fields}
        if target is not None:
            result['error_m'] = float(np.linalg.norm(ctx.data.site_xpos[self.site] - target))
        return result

    @staticmethod
    def _in_workspace(pos):
        return bool(np.all(pos >= _LOW - 1e-9) and np.all(pos <= _HIGH + 1e-9))

    def _execute(self, ctx, path, dt, target=None, rotation=None):
        for q in path:
            self._target = q
            self._step(ctx, dt)
        settled = False
        for _ in range(125):
            ep = (np.linalg.norm(ctx.data.site_xpos[self.site] - target)
                  if target is not None else np.max(np.abs(ctx.data.qpos[self.qadr] - path[-1])))
            er = (np.linalg.norm(rotation_error(rotation, ctx.data.site_xmat[self.site].reshape(3, 3)))
                  if rotation is not None else 0.)
            if ep < 0.003 and er < 0.015 and np.max(np.abs(ctx.data.qvel[self.dofs])) < 0.03:
                settled = True
                break
            self._step(ctx, 0.02)
        result = self._result(ctx, target, stopped='target' if settled else 'timeout')
        return json_ok(**result) if settled else json_fail('motion did not settle', **result)

    def move_to(self, ctx, x, y, z, yaw_deg=0, speed=0.25):
        try:
            target = np.array([_number(x, 'x'), _number(y, 'y'), _number(z, 'z')])
            yaw = math.radians(np.clip(_number(yaw_deg, 'yaw_deg'), -180, 180))
            speed = _speed(speed, 0.45, 0.001)
        except ValueError as error:
            return json_fail(str(error))
        if not self._in_workspace(target):
            return json_fail('target outside table workspace')
        start = ctx.data.site_xpos[self.site].copy()
        initial_rot = ctx.data.site_xmat[self.site].reshape(3, 3).copy()
        final_rot = down_rotation(yaw)
        angle = np.linalg.norm(rotation_error(final_rot, initial_rot))
        duration = max(0.25, 1.875 * np.linalg.norm(target - start) / speed, 1.875 * angle / 0.8)
        n = max(1, math.ceil(duration / 0.02))
        q = ctx.data.qpos[self.qadr].copy()
        self.ik.data.qpos[:] = ctx.data.qpos
        path = []
        for i in range(1, n + 1):
            t = i / n
            s = t ** 3 * (10 - 15 * t + 6 * t * t)
            point = start + s * (target - start)
            if not self._in_workspace(point):
                return json_fail('path outside table workspace')
            qnext = self.ik.solve(point, _rotation_between(initial_rot, final_rot, s), q)
            if qnext is None:
                return json_fail('target unreachable along Cartesian path', **self._result(ctx, target))
            if np.max(np.abs(qnext - q)) > 0.15:
                return json_fail('path too close to a joint singularity', **self._result(ctx, target))
            path.append(qnext)
            q = qnext
        peak_joint_step = max(np.max(np.abs(b - a)) for a, b in zip(
            [ctx.data.qpos[self.qadr]] + path[:-1], path))
        dt = max(duration / n, float(peak_joint_step) / 1.2)
        return self._execute(ctx, path, dt, target, final_rot)

    def move_joints(self, ctx, q_deg, speed=30):
        try:
            if not isinstance(q_deg, (list, tuple)) or len(q_deg) != 6:
                raise ValueError('q_deg must contain six angles')
            raw = np.deg2rad([_number(v, 'q_deg') for v in q_deg])
            q = np.clip(raw, self.limits[:, 0], self.limits[:, 1])
            speed = math.radians(_speed(speed, 90, 0.1))
        except ValueError as error:
            return json_fail(str(error))
        start = ctx.data.qpos[self.qadr].copy()
        duration = max(0.25, 1.875 * float(np.max(np.abs(q - start))) / speed)
        n = max(1, math.ceil(duration / 0.02))
        path, positions = [], [ctx.data.site_xpos[self.site].copy()]
        self.ik.data.qpos[:] = ctx.data.qpos
        for i in range(1, n + 1):
            t = i / n
            s = t ** 3 * (10 - 15 * t + 6 * t * t)
            waypoint = start + s * (q - start)
            self.ik.forward(waypoint)
            pos = self.ik.data.site_xpos[self.site].copy()
            if not self._in_workspace(pos):
                return json_fail('joint path outside table workspace')
            path.append(waypoint)
            positions.append(pos)
        dt = max(duration / n, max(np.linalg.norm(b - a)
                                   for a, b in zip(positions, positions[1:])) / 0.4)
        result = self._execute(ctx, path, dt)
        result['clamped'] = bool(np.any(q != raw))
        return result

    def home(self, ctx):
        return self.move_to(ctx, *self._home_position, yaw_deg=0)

    def lift(self, ctx, dz):
        try:
            dz = _number(dz, 'dz')
            if abs(dz) > 0.6:
                raise ValueError('dz must be between -0.6 and 0.6 m')
        except ValueError as error:
            return json_fail(str(error))
        s = self.state(ctx.model, ctx.data)
        return self.move_to(ctx, s['x'], s['y'], s['z'] + dz, s['yaw_deg'])

    def open_gripper(self, ctx):
        released = ctx.release()
        self._holding, self._gripper, self._grip_target = None, 'open', 0.
        self._step(ctx, 0.7)
        return json_ok(**self._result(ctx, released=released, stopped='open'))

    def close_gripper(self, ctx):
        self._grip_target, self._gripper = 255., 'closed'
        stopped = 'timeout'
        for i in range(60):
            self._step(ctx, 0.02)
            if any(c.dist <= 0 and
                   ((c.geom1 in self._finger_geoms and c.geom2 in self._object_geoms) or
                    (c.geom2 in self._finger_geoms and c.geom1 in self._object_geoms))
                   for c in ctx.data.contact):
                stopped = 'contact'
                break
            if i >= 10 and np.max(np.abs(ctx.data.qvel[self.grip_dofs])) < 0.015:
                stopped = 'stall'
                break
        candidates = []
        pos = ctx.data.site_xpos[self.site]
        rot = ctx.data.site_xmat[self.site].reshape(3, 3)
        if not ctx.held():
            for obj in ctx.objects():
                if not obj.get('dynamic', False):
                    continue
                point, _ = ctx.object_pose(obj['id'])
                offset = rot.T @ (point - pos)
                distance = float(np.linalg.norm(offset))
                if distance <= 0.04 and abs(offset[0]) <= 0.03 and abs(offset[1]) <= 0.0425 and abs(offset[2]) <= 0.035:
                    candidates.append((distance, obj['id']))
            if candidates:
                ctx.hold(min(candidates)[1], 'robot/grasp')
        self._holding = ctx.held()
        if candidates:
            # Let the context apply the attachment and evaluate grasp goals.
            self._step(ctx, 0.02)
        return json_ok(**self._result(ctx, stopped=stopped, gripper='closed'))

    def tools(self):
        cart = {'x': _scalar(-0.6, 0.6), 'y': _scalar(-0.4, 0.4), 'z': _scalar(0.76, 1.65),
                'yaw_deg': _scalar(-180, 180, 0), 'speed': _scalar(0.001, 0.5, 0.25)}
        definitions = [
            ('move_to', 'Move straight in world coordinates, fingers down. / Mover en línea recta en coordenadas globales, dedos abajo.',
             _schema(cart, ('x', 'y', 'z')), self.move_to),
            ('move_joints', 'Move six joint angles in degrees; speed in degrees/s. / Mover seis articulaciones en grados; velocidad en grados/s.',
             _schema({'q_deg': {'type': 'array', 'minItems': 6, 'maxItems': 6, 'items': _scalar(-360, 360)},
                      'speed': _scalar(0.1, 90, 30)}, ('q_deg',)), self.move_joints),
            ('home', 'Return above the table with fingers down. / Volver sobre la mesa con los dedos abajo.', _schema(), self.home),
            ('open_gripper', 'Open the fingers and release the object. / Abrir los dedos y soltar el objeto.', _schema(), self.open_gripper),
            ('close_gripper', 'Close until contact or stall; assist a nearby grasp. / Cerrar hasta contacto o bloqueo; asistir el agarre cercano.', _schema(), self.close_gripper),
            ('lift', 'Move vertically by dz meters. / Mover verticalmente dz metros.',
             _schema({'dz': _scalar(-0.6, 0.6)}, ('dz',)), self.lift),
        ]
        return {name: ToolSpec(name, desc, params, fn) for name, desc, params, fn in definitions}

    def card(self):
        tools = [{'name': t.name, 'description': t.description, 'params': t.params}
                 for t in self.tools().values()]
        tools.append({'name': 'look', 'description': 'Inspect a robot camera. / Observar una cámara del robot.',
                      'params': _schema({'camera': {'type': 'string', 'enum': ['robot/wrist', 'robot/overhead']}})})
        return {'id': self.id, 'name': self.name,
                'description': 'Fixed arm for tabletop pick and place. / Brazo fijo para recoger y colocar objetos en la mesa.',
                'sensors': ['joint_position', 'joint_velocity', 'grasp_pose', 'contact', 'holding'],
                'cameras': ['robot/wrist', 'robot/overhead'], 'tools': tools,
                'limits': {'workspace_m': {'min': _LOW.tolist(), 'max': _HIGH.tolist()},
                           'max_tcp_speed_m_s': 0.5, 'max_joint_speed_deg_s': 90,
                           'table_height_m': 0.75, 'grasp_tolerance_m': 0.04},
                'notes': [
                    'Approach above an object, lower to its center, close, then lift. / Acercarse por arriba, bajar al centro, cerrar y levantar.',
                    'Keep the grasp site within 3 cm of the object before closing. / Situar el punto de agarre a menos de 3 cm del objeto antes de cerrar.',
                    'Grasp assist holds a dynamic object within 4 cm between the fingers; opening releases it. / La asistencia sujeta un objeto dinámico a menos de 4 cm entre los dedos; abrir lo libera.',
                    'Speed for move_to is m/s; move_joints uses degrees/s. TCP motion is capped below 0.5 m/s. / move_to usa m/s; move_joints usa grados/s. El movimiento se limita por debajo de 0.5 m/s.',
                    'Some far table corners are unreachable; route around obstacles with separate moves. / Algunas esquinas lejanas son inalcanzables; rodear obstáculos con movimientos separados.',
                ]}
