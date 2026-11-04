"""Minimal world and tool context for robot controller tests.

The core's real ToolContext adds recording, pacing, goals and logging; this
one only steps physics, applies the grasp assist and checks ``fallen``.
"""
from __future__ import annotations

import mujoco
import numpy as np

from . import base
from ..assets import menagerie_dir

_SHAPES = {'box': mujoco.mjtGeom.mjGEOM_BOX, 'sphere': mujoco.mjtGeom.mjGEOM_SPHERE,
           'cylinder': mujoco.mjtGeom.mjGEOM_CYLINDER}


def build(robot: base.Robot, objects: list[dict] | None = None, spawn: base.Spawn = base.Spawn()):
    """Floor + simple objects + robot. objects: [{id, shape, size, pos, dynamic, mass, rgba}]
    (size uses MuJoCo half-sizes: box [hx,hy,hz], sphere [r], cylinder [r, hz])."""
    spec = mujoco.MjSpec()
    spec.worldbody.add_light(pos=[0, 0, 4], dir=[0, 0, -1], type=mujoco.mjtLightType.mjLIGHT_DIRECTIONAL)
    spec.worldbody.add_geom(name='floor', type=mujoco.mjtGeom.mjGEOM_PLANE, size=[20, 20, 0.1], rgba=[0.8, 0.8, 0.8, 1])
    for o in objects or []:
        body = spec.worldbody.add_body(name='obj/' + o['id'], pos=o['pos'])
        if o.get('dynamic', True):
            body.add_freejoint()
        body.add_geom(name='obj/' + o['id'], type=_SHAPES[o['shape']], size=list(o['size']) + [0] * (3 - len(o['size'])),
                      rgba=o.get('rgba', [0.9, 0.2, 0.2, 1]), mass=o.get('mass', 0.2) if o.get('dynamic', True) else 0,
                      friction=[1.5, 0.02, 0.002])
    robot.attach(spec, spawn, menagerie_dir())
    model = spec.compile()
    data = mujoco.MjData(model)
    robot.bind(model, data)
    mujoco.mj_resetData(model, data)
    robot.reset(model, data)
    mujoco.mj_forward(model, data)
    return model, data


class SimpleContext(base.ToolContext):
    def __init__(self, robot: base.Robot, model, data, seed: int = 0):
        self.robot, self.model, self.data = robot, model, data
        self.rng = np.random.default_rng(seed)
        self._held: tuple[str, int, np.ndarray, np.ndarray] | None = None
        self.notes: list[str] = []

    def _body(self, object_id):
        return mujoco.mj_name2id(self.model, mujoco.mjtObj.mjOBJ_BODY, 'obj/' + object_id)

    def step(self, seconds: float) -> None:
        n = max(1, int(round(seconds / self.model.opt.timestep)))
        for _ in range(n):
            self.robot.control(self.model, self.data)
            mujoco.mj_step(self.model, self.data)
            self._apply_hold()
            reason = self.robot.fallen(self.model, self.data)
            if reason:
                raise base.ActionInterrupted('fell', reason)

    def _apply_hold(self):
        if not self._held:
            return
        oid, site, rel_p, rel_q = self._held
        b = self._body(oid)
        jnt = self.model.body_jntadr[b]
        if jnt < 0:
            return
        sp, sm = self.data.site_xpos[site], self.data.site_xmat[site].reshape(3, 3)
        sq = np.zeros(4); mujoco.mju_mat2Quat(sq, sm.flatten())
        q = np.zeros(4); mujoco.mju_mulQuat(q, sq, rel_q)
        adr, vadr = self.model.jnt_qposadr[jnt], self.model.jnt_dofadr[jnt]
        self.data.qpos[adr:adr + 3] = sp + sm @ rel_p
        self.data.qpos[adr + 3:adr + 7] = q
        self.data.qvel[vadr:vadr + 6] = 0
        mujoco.mj_kinematics(self.model, self.data)

    def object_pose(self, object_id):
        b = self._body(object_id)
        return self.data.xpos[b].copy(), self.data.xquat[b].copy()

    def objects(self):
        out = []
        for b in range(self.model.nbody):
            name = self.model.body(b).name
            if name.startswith('obj/'):
                out.append({'id': name[4:], 'name': name[4:], 'pos': self.data.xpos[b].round(3).tolist(),
                            'dynamic': bool(self.model.body_jntnum[b])})
        return out

    def hold(self, object_id, site):
        sid = mujoco.mj_name2id(self.model, mujoco.mjtObj.mjOBJ_SITE, site)
        b = self._body(object_id)
        sm = self.data.site_xmat[sid].reshape(3, 3)
        rel_p = sm.T @ (self.data.xpos[b] - self.data.site_xpos[sid])
        sq = np.zeros(4); mujoco.mju_mat2Quat(sq, sm.flatten())
        inv = np.zeros(4); mujoco.mju_negQuat(inv, sq)
        rel_q = np.zeros(4); mujoco.mju_mulQuat(rel_q, inv, self.data.xquat[b])
        self._held = (object_id, sid, rel_p, rel_q)

    def release(self):
        oid = self._held[0] if self._held else None
        self._held = None
        return oid

    def held(self):
        return self._held[0] if self._held else None

    def log(self, text):
        self.notes.append(text)
