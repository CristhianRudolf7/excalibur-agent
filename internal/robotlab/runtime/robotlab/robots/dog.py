"""Go2 quadruped: bounded joint PD and explicit Cartesian keyframes."""
from __future__ import annotations

import math
from pathlib import Path

import mujoco
import numpy as np

from .base import Robot, Spawn
from . import dog_sensors as sensors
from .dog_tools import DogTools

LEGS = ('FL', 'FR', 'RL', 'RR')
PARTS = ('hip', 'thigh', 'calf')
HIPS = np.array([[.1934, .0465, 0], [.1934, -.0465, 0],
                 [-.1934, .0465, 0], [-.1934, -.0465, 0]])
SIDES = (1, -1, 1, -1)


def rotation(pose):
    r, p, y = pose[3:]
    cr, sr, cp, sp, cy, sy = math.cos(r), math.sin(r), math.cos(p), math.sin(p), math.cos(y), math.sin(y)
    return np.array([[cy*cp, cy*sp*sr-sy*cr, cy*sp*cr+sy*sr],
                     [sy*cp, sy*sp*sr+cy*cr, sy*sp*cr-cy*sr], [-sp, cp*sr, cp*cr]])


def leg_ik(x, y, z, side):
    """Exact foot-geom-center IK, without clipping unreachable points."""
    offset, upper, lower = side*.0955, .213, math.hypot(.213, .002)
    radius2 = y*y+z*z-offset*offset
    if radius2 <= 0:
        raise ValueError('Inside hip offset')
    vertical = math.sqrt(radius2)
    cosine = (x*x+vertical*vertical-upper*upper-lower*lower)/(2*upper*lower)
    if not -1 <= cosine <= 1:
        raise ValueError('Outside leg workspace')
    knee = -math.acos(cosine)
    hip = math.atan2(z, y)-math.atan2(-vertical, offset)
    hip = (hip+math.pi) % (2*math.pi)-math.pi
    thigh = math.atan2(-x, vertical)-math.atan2(lower*math.sin(knee), upper+lower*math.cos(knee))
    return np.array([hip, thigh, knee-math.atan2(.002, .213)])


class DogRobot(DogTools, Robot):
    id = 'dog'
    name = {'en': 'Dog', 'es': 'Perro'}
    world_kind = 'room'
    default_camera = 'robot/head'
    legs, parts = LEGS, PARTS

    def attach(self, spec, spawn: Spawn, menagerie: str):
        self.spawn = spawn
        dog = mujoco.MjSpec.from_file(str(Path(menagerie)/'unitree_go2/go2.xml'))
        for key in list(dog.keys):
            dog.delete(key)
        trunk = dog.body('base')
        trunk.add_camera(name='head', pos=[.31, 0, .035],
                         xyaxes=[0, -1, 0, .258819, 0, .965926], fovy=65)
        trunk.add_camera(name='rear', pos=[-.18, 0, .025],
                         xyaxes=[0, 1, 0, -.939693, 0, .342020], fovy=80)
        # Group 3 is collision geometry, group 2 visuals. Terrain rays exclude both.
        for geom in dog.geoms:
            if geom.group == 3:
                geom.contype, geom.conaffinity = 2, 1
        frame = spec.worldbody.add_frame(pos=[spawn.x, spawn.y, 0],
                                         quat=[math.cos(spawn.yaw/2), 0, 0, math.sin(spawn.yaw/2)])
        dog.option.integrator = spec.option.integrator
        dog.option.impratio = spec.option.impratio
        dog.option.cone = spec.option.cone
        spec.attach(dog, prefix='robot/', frame=frame)
        spec.option.timestep = .002
        spec.option.integrator = mujoco.mjtIntegrator.mjINT_IMPLICITFAST
        spec.option.cone = mujoco.mjtCone.mjCONE_ELLIPTIC
        spec.option.impratio = 100

    def bind(self, model, data):
        self.base = model.body('robot/base').id
        root = model.body_jntadr[self.base]
        self.rq, self.rv = int(model.jnt_qposadr[root]), int(model.jnt_dofadr[root])
        self.joint_names = [leg+'_'+part for leg in LEGS for part in PARTS]
        joints = [model.joint('robot/'+name+'_joint').id for name in self.joint_names]
        self.lq, self.lv = model.jnt_qposadr[joints], model.jnt_dofadr[joints]
        self.lrange = model.jnt_range[joints].copy()
        self.la = np.array([model.actuator('robot/'+name).id for name in self.joint_names])
        self.feet = [model.geom('robot/'+leg).id for leg in LEGS]
        self.robot_geoms = {g for g in range(model.ngeom) if model.body(model.geom_bodyid[g]).name.startswith('robot/')}
        self.mass = float(model.body_subtreemass[self.base])
        self.jac = np.zeros((3, model.nv))

    def reset(self, model, data):
        data.qpos[self.rq:self.rq+7] = [self.spawn.x, self.spawn.y, .27,
                                       math.cos(self.spawn.yaw/2), 0, 0, math.sin(self.spawn.yaw/2)]
        self.target = np.tile([0., .9, -1.8], 4)
        data.qpos[self.lq] = self.target
        self.mode = 'joints'
        self.low_since = None
        self.trunk_target = np.array([self.spawn.x, self.spawn.y, .27, 0., 0., self.spawn.yaw])
        self.foot_target = np.zeros((4, 3))

    def pose(self, data):
        rot = data.xmat[self.base].reshape(3, 3)
        return np.r_[data.xpos[self.base], math.atan2(rot[2, 1], rot[2, 2]),
                     math.asin(np.clip(-rot[2, 0], -1, 1)), math.atan2(rot[1, 0], rot[0, 0])]

    def solve(self, feet, trunk):
        rot = rotation(trunk)
        angles, details = [], {}
        for i, leg in enumerate(LEGS):
            p = rot.T @ (feet[i]-trunk[:3])-HIPS[i]
            try:
                q = leg_ik(*p, SIDES[i])
                margins = np.rad2deg(np.minimum(q-self.lrange[i*3:i*3+3, 0], self.lrange[i*3:i*3+3, 1]-q))
                reachable = bool(np.min(margins) >= -1e-7)
                details[leg] = dict(joints_deg=dict(zip(PARTS, np.rad2deg(q).round(1).tolist())),
                                    reachable=reachable, limit_margin_deg=dict(zip(PARTS, margins.round(1).tolist())))
            except ValueError as exc:
                q = np.zeros(3)
                details[leg] = dict(joints_deg=None, reachable=False, limit_margin_deg=None, error=str(exc))
            angles.extend(q)
        return np.array(angles), details

    def control(self, model, data):
        if self.mode == 'feet':
            self.target, _ = self.solve(self.foot_target, self.trunk_target)
        torque = 220.*(self.target-data.qpos[self.lq])-7.*data.qvel[self.lv]
        torque += data.qfrc_bias[self.lv]
        # Gravity feedforward acts only through feet actually touching terrain.
        # Nonnegative vertical loads cannot support a COM outside the polygon.
        contacts, _ = sensors.external_contacts(self, model, data)
        stance = [i for i, leg in enumerate(LEGS) if contacts[leg]]
        if stance:
            points = data.geom_xpos[np.array(self.feet)[stance]]
            com = data.subtree_com[self.base]
            matrix = np.vstack((np.ones(len(stance)), (points[:, :2]-com[:2]).T))
            weights = np.linalg.lstsq(matrix, np.array([self.mass*9.81, 0., 0.]), rcond=None)[0]
            weights = np.maximum(weights, 0.)
            if weights.sum() > 0:
                weights *= self.mass*9.81/weights.sum()
            for i, load in zip(stance, weights):
                mujoco.mj_jac(model, data, self.jac, None, data.geom_xpos[self.feet[i]], model.geom_bodyid[self.feet[i]])
                torque -= self.jac[2, self.lv]*load
        data.ctrl[self.la] = np.clip(torque, model.actuator_ctrlrange[self.la, 0], model.actuator_ctrlrange[self.la, 1])

    def feet_contacts(self, model, data):
        return sensors.external_contacts(self, model, data)[0]

    def fallen(self, model, data):
        rot = data.xmat[self.base].reshape(3, 3)
        if math.acos(float(np.clip(rot[2, 2], -1, 1))) > math.radians(50):
            return 'trunk tilt exceeds 50 degrees'
        p = data.xpos[self.base]
        if p[2]-sensors.terrain(model, data, *p[:2]) < .12:
            if self.low_since is None:
                self.low_since = float(data.time)
            if data.time-self.low_since >= .3-1e-9:
                return 'trunk below terrain clearance for 0.3 s'
        else:
            self.low_since = None
        _, pairs = sensors.external_contacts(self, model, data)
        for rg, eg in pairs:
            body = int(model.geom_bodyid[rg])
            link = model.body(body).name
            if body != self.base and not link.endswith('_hip'):
                continue
            # Dynamic objects do not trigger the fixed-object fall condition.
            if model.body_weldid[model.geom_bodyid[eg]] == 0:
                return 'trunk/head/hip contact: '+(model.geom(eg).name or str(eg))
        return None

    def state(self, model, data):
        return sensors.observation(self, model, data)
