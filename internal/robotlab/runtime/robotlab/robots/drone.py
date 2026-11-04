"""Four-rotor classical flight control and deterministic motion tools."""
from __future__ import annotations

import math
from pathlib import Path

import mujoco
import numpy as np

from .base import ActionInterrupted, Robot, Spawn, ToolSpec, json_fail, json_ok
from .drone_safety import blocked_route, fixed_obstacles


def _wrap(angle):
    return (angle + math.pi) % (2 * math.pi) - math.pi


def _number(value, name):
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value):
        raise ValueError(f'{name} must be a finite number')
    return float(value)


class DroneRobot(Robot):
    id = 'drone'
    name = {'en': 'Drone', 'es': 'Dron'}
    world_kind = 'room'
    default_camera = 'robot/front'
    max_speed = 1.5

    def __init__(self):
        self.ceiling = 3.0
        self._motors = False
        self._landing = False
        self._failure = None
        self._safety_reason = None
        self._speed = 0.5

    def attach(self, spec: mujoco.MjSpec, spawn: Spawn, menagerie: str) -> None:
        self._spawn = spawn
        child = mujoco.MjSpec.from_file(str(Path(menagerie) / 'skydio_x2' / 'x2.xml'))
        # The surrounding world owns physics options, not the attached model.
        spec.option.timestep = min(spec.option.timestep, 0.005)
        child.option.timestep = spec.option.timestep
        child.option.density = spec.option.density
        child.option.viscosity = spec.option.viscosity
        body = child.body('x2')
        # The lowest body collision surface is at the body origin.
        body.pos = [0, 0, 0.002]
        a = math.radians(10)
        body.add_camera(name='front', pos=[0.11, 0, 0.055],
                        xyaxes=[0, -1, 0, math.sin(a), 0, math.cos(a)], fovy=65)
        body.add_camera(name='down', pos=[0, 0, -0.01], xyaxes=[0, -1, 0, 1, 0, 0], fovy=65)
        body.add_site(name='grasp', pos=[0, 0, -0.02], size=[0.01] * 3)
        frame = spec.worldbody.add_frame(name='robot/spawn', pos=[spawn.x, spawn.y, 0],
                                         quat=[math.cos(spawn.yaw / 2), 0, 0, math.sin(spawn.yaw / 2)])
        spec.attach(child, prefix='robot/', frame=frame)

    def bind(self, model, data) -> None:
        self._body = model.body('robot/x2').id
        joint = int(model.body_jntadr[self._body])
        self._q = int(model.jnt_qposadr[joint])
        self._v = int(model.jnt_dofadr[joint])
        self._act = np.array([model.actuator(f'robot/thrust{i}').id for i in range(1, 5)])
        self._geoms = set(np.flatnonzero(model.geom_bodyid == self._body).tolist())
        self._obstacles = fixed_obstacles(model, self._geoms)
        self._mass = float(model.body_mass[self._body])
        rotation = np.empty(9)
        mujoco.mju_quat2Mat(rotation, model.body_iquat[self._body])
        rotation = rotation.reshape(3, 3)
        self._inertia = rotation @ np.diag(model.body_inertia[self._body]) @ rotation.T
        mixer = np.ones((4, 4))
        for i, aid in enumerate(self._act):
            sid = model.actuator_trnid[aid, 0]
            arm = model.site_pos[sid] - model.body_ipos[self._body]
            mixer[1:, i] = [arm[1], -arm[0], model.actuator_gear[aid, 5]]
        self._mix = np.linalg.inv(mixer)
        self._ctrl_min = model.actuator_ctrlrange[self._act, 0]
        self._ctrl_max = model.actuator_ctrlrange[self._act, 1]
        # bind precedes the core's first mj_forward.
        mujoco.mj_forward(model, data)
        tops = [float(data.geom_xpos[g, 2]) + float(
            np.abs(data.geom_xmat[g].reshape(3, 3)[2]) @ model.geom_size[g])
                for g in range(model.ngeom) if model.geom(g).name.startswith('wall/')]
        self.ceiling = max(0.0, min(3.0, max(tops) - 0.3)) if tops else 3.0

    def reset(self, model, data) -> None:
        self._target = np.array([self._spawn.x, self._spawn.y, 0.002])
        data.qpos[self._q:self._q + 3] = self._target
        self._yaw = self._spawn.yaw
        data.qpos[self._q + 3:self._q + 7] = [math.cos(self._yaw / 2), 0, 0, math.sin(self._yaw / 2)]
        data.qvel[self._v:self._v + 6] = 0
        data.ctrl[self._act] = 0
        self._integral = np.zeros(3)
        self._motors = self._landing = False
        self._failure = None
        self._safety_reason = None
        self._before_velocity = np.zeros(6)
        self._speed = 0.5

    def _position(self, data):
        return data.qpos[self._q:self._q + 3]

    def _velocity(self, data):
        return data.qvel[self._v:self._v + 3]

    def _yaw_now(self, data):
        r = data.xmat[self._body].reshape(3, 3)
        return math.atan2(r[1, 0], r[0, 0])

    def _contact(self, model, data):
        for c in data.contact:
            a, b = int(c.geom1), int(c.geom2)
            if (a in self._geoms) != (b in self._geoms):
                yield c, b if a in self._geoms else a

    def control(self, model, data) -> None:
        self._before_velocity[:] = data.qvel[self._v:self._v + 6]
        reason = self.safety(model, data)
        if reason:
            self._stop(model, data, reason)
        if not self._motors or self._failure:
            data.ctrl[self._act] = 0
            return
        # Position P -> velocity PI -> attitude P -> body-rate P, with
        # rigid-body feedforward and a physical force/torque rotor mixer.
        velocity = self._velocity(data)
        desired_v = 1.6 * (self._target - self._position(data))
        norm = float(np.linalg.norm(desired_v))
        # Leave braking/PI headroom below the hard measured speed limit.
        speed = min(self._speed, 1.3)
        if norm > speed:
            desired_v *= speed / norm
        error_v = desired_v - velocity
        self._integral += error_v * model.opt.timestep
        np.clip(self._integral, -0.4, 0.4, out=self._integral)
        acceleration = 3.5 * error_v + 0.6 * self._integral
        np.clip(acceleration, -2.0, 2.0, out=acceleration)
        acceleration[2] += -model.opt.gravity[2]
        zaxis = acceleration / np.linalg.norm(acceleration)
        heading = np.array([math.cos(self._yaw), math.sin(self._yaw), 0])
        yaxis = np.cross(zaxis, heading)
        yaxis /= np.linalg.norm(yaxis)
        desired_r = np.column_stack((np.cross(yaxis, zaxis), yaxis, zaxis))
        rotation = data.xmat[self._body].reshape(3, 3)
        skew = rotation.T @ desired_r - desired_r.T @ rotation
        attitude_error = 0.5 * np.array([skew[2, 1], skew[0, 2], skew[1, 0]])
        desired_rates = np.clip(6.0 * attitude_error, -2.0, 2.0)
        rates = data.qvel[self._v + 3:self._v + 6]
        torque = self._inertia @ (22.0 * (desired_rates - rates)) + np.cross(rates, self._inertia @ rates)
        thrust = self._mass * acceleration[2] / max(0.6, float(rotation[2, 2]))
        data.ctrl[self._act] = np.clip(self._mix @ np.r_[thrust, torque], self._ctrl_min, self._ctrl_max)
        # Landing ends only on upward-facing support contact at low speed.
        if self._landing and np.linalg.norm(velocity) < 0.25:
            for c, _ in self._contact(model, data):
                if abs(c.frame[2]) > 0.7:
                    self._motors = False
                    self._landing = False
                    data.ctrl[self._act] = 0
                    break

    def fallen(self, model, data):
        if self._failure:
            return self._failure
        if data.xmat[self._body, 8] < math.cos(math.radians(80)):
            self._failure = 'crashed'
        else:
            # Point velocities include angular speed and moving-object impacts.
            for c, other in self._contact(model, data):
                if self._landing:
                    continue
                point = c.pos - data.xpos[self._body]
                r = data.xmat[self._body].reshape(3, 3)
                own = self._velocity(data) + np.cross(r @ data.qvel[self._v + 3:self._v + 6], point)
                before = self._before_velocity[:3] + np.cross(r @ self._before_velocity[3:], point)
                external = np.zeros(6)
                mujoco.mj_objectVelocity(model, data, mujoco.mjtObj.mjOBJ_BODY,
                                        int(model.geom_bodyid[other]), external, 0)
                arm = c.pos - data.xipos[model.geom_bodyid[other]]
                external_point = external[3:] + np.cross(external[:3], arm)
                if max(np.linalg.norm(own - external_point), np.linalg.norm(before - external_point)) > 1.0:
                    self._failure = 'crashed'
                    break
        return self._failure

    def safety(self, model, data):
        if self._safety_reason:
            return self._safety_reason
        if self._position(data)[2] > self.ceiling + 1e-3:
            return 'altitude limit'
        if np.linalg.norm(self._velocity(data)) > self.max_speed + 1e-3:
            return 'speed limit'
        return None

    def _stop(self, model, data, reason):
        self._safety_reason = reason
        self._motors = False
        data.ctrl[self._act] = 0
        raise ActionInterrupted('safety', reason)

    def state(self, model, data):
        p = self._position(data)
        return dict(x=float(p[0]), y=float(p[1]), z=float(p[2]),
                    yaw_deg=math.degrees(self._yaw_now(data)),
                    speed=float(np.linalg.norm(self._velocity(data))),
                    motors_on=self._motors, landing=self._landing,
                    altitude_limit=self.ceiling, crashed=self._failure == 'crashed')

    def _result(self, ctx, achieved, target=None, message='target not reached'):
        fields = {'state': self.state(ctx.model, ctx.data), 'stopped': 'settled' if achieved else message}
        if target is not None:
            fields['target'] = [float(v) for v in target]
            fields['error_m'] = float(np.linalg.norm(self._position(ctx.data) - target))
        return json_ok(**fields) if achieved else json_fail(message, **fields)

    def _fly(self, ctx, target, speed=0.5):
        speed = min(self.max_speed, max(0.05, _number(speed, 'speed')))
        if not self._motors:
            return self._result(ctx, False, message='takeoff required')
        target = np.array(target, dtype=float)
        np.clip(target[:2], -30, 30, out=target[:2])
        # Keep a small settling margin below the absolute altitude ceiling.
        target[2] = min(self.ceiling - 0.03, max(0.15, target[2]))
        obstacle = blocked_route(ctx.model, ctx.data, self._obstacles, self._position(ctx.data), target)
        if obstacle:
            self._stop(ctx.model, ctx.data, f'route blocked by {obstacle}')
        self._landing = False
        self._speed = speed
        self._target = target.copy()
        distance = float(np.linalg.norm(target - self._position(ctx.data)))
        settled = ctx.run_until(lambda: np.linalg.norm(self._position(ctx.data) - target) < 0.035
                                and np.linalg.norm(self._velocity(ctx.data)) < 0.05,
                                timeout=distance / self._speed + 12)
        return self._result(ctx, settled, target)

    def takeoff(self, ctx, height=1.0):
        height = _number(height, 'height')
        if self._failure or self._safety_reason:
            return self._result(ctx, False, message=self._failure or self._safety_reason)
        if self.ceiling < 0.18:
            return self._result(ctx, False, message='insufficient altitude clearance')
        self._motors = True
        self._integral[:] = 0
        p = self._position(ctx.data)
        return self._fly(ctx, [p[0], p[1], min(self.ceiling, max(0.15, height))], 0.5)

    def fly_to(self, ctx, x, y, z, speed=0.5):
        return self._fly(ctx, [_number(x, 'x'), _number(y, 'y'), _number(z, 'z')], speed)

    def move(self, ctx, forward=0.0, left=0.0, up=0.0):
        forward, left, up = (min(30, max(-30, _number(v, k)))
                             for v, k in ((forward, 'forward'), (left, 'left'), (up, 'up')))
        yaw = self._yaw_now(ctx.data)
        p = self._position(ctx.data).copy()
        p += [math.cos(yaw) * forward - math.sin(yaw) * left,
              math.sin(yaw) * forward + math.cos(yaw) * left, up]
        return self._fly(ctx, p)

    def turn(self, ctx, angle_deg):
        angle = min(360.0, max(-360.0, _number(angle_deg, 'angle_deg')))
        if not self._motors:
            return self._result(ctx, False, message='takeoff required')
        self._target = self._position(ctx.data).copy()
        # Ramp yaw to permit a full revolution without wrapping it away.
        start = self._yaw_now(ctx.data)
        duration = max(0.02, abs(angle) / 45)
        t = 0.0
        while t < duration:
            t = min(duration, t + 0.02)
            self._yaw = start + math.radians(angle) * t / duration
            ctx.step(0.02)
        settled = ctx.run_until(lambda: abs(_wrap(self._yaw_now(ctx.data) - self._yaw)) < math.radians(2)
                                and np.linalg.norm(ctx.data.qvel[self._v + 3:self._v + 6]) < 0.05, 8)
        result = self._result(ctx, settled, self._target)
        result['yaw_error_deg'] = math.degrees(_wrap(self._yaw_now(ctx.data) - self._yaw))
        return result

    def hover(self, ctx, seconds=1.0):
        seconds = min(60.0, max(0.0, _number(seconds, 'seconds')))
        if not self._motors:
            return self._result(ctx, False, message='takeoff required')
        self._target = self._position(ctx.data).copy()
        if seconds:
            ctx.step(seconds)
        achieved = np.linalg.norm(self._position(ctx.data) - self._target) < 0.05
        result = self._result(ctx, achieved, self._target, 'hover drift exceeded 0.05 m')
        result['seconds'] = seconds
        if achieved:
            result['stopped'] = 'duration elapsed'
        return result

    def land(self, ctx):
        if self._failure or self._safety_reason:
            return self._result(ctx, False, message=self._failure or self._safety_reason)
        self._landing = True
        self._speed = 0.18
        self._target = self._position(ctx.data).copy()
        self._target[2] = -0.1
        self._integral[:] = 0
        landed = ctx.run_until(lambda: not self._motors, timeout=max(0, self._position(ctx.data)[2]) / 0.18 + 12)
        if landed:
            ctx.step(0.3)
            self._landing = False
        result = self._result(ctx, landed, message='landing timed out')
        if landed:
            result['stopped'] = 'motors off on contact'
        return result

    def tools(self):
        def schema(properties, required=()):
            return {'type': 'object', 'properties': properties, 'required': list(required), 'additionalProperties': False}

        def number(low, high, default=None):
            out = {'type': 'number', 'minimum': low, 'maximum': high}
            if default is not None:
                out['default'] = default
            return out

        return {s.name: s for s in [
            ToolSpec('takeoff', 'Rise and hold altitude. / Despegar y mantener altura.',
                     schema({'height': number(0, self.ceiling, min(1.0, self.ceiling))}), self.takeoff),
            ToolSpec('fly_to', 'Fly straight to a world position. / Volar recto a una posición global.',
                     schema({'x': number(-30, 30), 'y': number(-30, 30), 'z': number(0, self.ceiling),
                             'speed': number(0.05, 1.5, 0.5)}, ('x', 'y', 'z')), self.fly_to),
            ToolSpec('move', 'Move relative to body heading. / Mover relativo al rumbo del cuerpo.',
                     schema({k: number(-30, 30, 0) for k in ('forward', 'left', 'up')}), self.move),
            ToolSpec('turn', 'Turn in place by degrees. / Girar en el sitio en grados.',
                     schema({'angle_deg': number(-360, 360)}, ('angle_deg',)), self.turn),
            ToolSpec('hover', 'Hold position for simulation seconds. / Mantener posición durante segundos simulados.',
                     schema({'seconds': number(0, 60, 1)}), self.hover),
            ToolSpec('land', 'Descend slowly; stop motors on contact. / Descender despacio y apagar motores al tocar.',
                     schema({}), self.land),
        ]}

    def card(self):
        return dict(id=self.id, name=self.name, world_kind=self.world_kind,
                    description='Four rotors: takeoff, position flight, yaw, hover and soft landing. / '
                                'Cuatro rotores: despegue, vuelo a posición, giro, vuelo estacionario y aterrizaje suave.',
                    sensors=['position / posición', 'velocity / velocidad', 'attitude / actitud', 'body rates / velocidad angular'],
                    cameras=['robot/front', 'robot/down'],
                    tools=[dict(name=s.name, description=s.description, params=s.params) for s in self.tools().values()] +
                          [dict(name='look', description='Observe without advancing time. / Observar sin avanzar el tiempo.',
                                params={'type': 'object', 'properties': {}})],
                    limits=dict(max_speed_m_s=self.max_speed, altitude_m=self.ceiling, max_tilt_deg=80),
                    notes=['Take off before moving. Coordinates/altitude are world meters; turns use degrees. / '
                           'Despegar antes de mover. Coordenadas/altura en metros globales; giros en grados.',
                           'Forward follows body yaw; left is positive. Front camera points ten degrees down. / '
                           'Adelante sigue el rumbo; izquierda es positiva. Cámara frontal diez grados abajo.',
                           'Altitude ceiling: 3 m or highest named wall minus 0.3 m. Landing stops motors on support contact. / '
                           'Altura máxima: 3 m o pared más alta menos 0.3 m. Aterrizar apaga motores al tocar apoyo.',
                           'No gripper or route planning. Fixed obstacles stop unsafe routes; inspect before flying. / '
                           'Sin pinza ni planificación de rutas. Obstáculos fijos detienen rutas inseguras; inspeccionar antes de volar.',
                           'Targets clamp to +/-30 m in x/y and 0.15 m up to the altitude ceiling minus 0.03 m; speed is at most 1.5 m/s. / '
                           'Objetivos limitados a +/-30 m en x/y y 0.15 m hasta altura máxima menos 0.03 m; velocidad máxima 1.5 m/s.'])
