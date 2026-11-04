"""Small deterministic kinematics helpers for the humanoid controller."""
from __future__ import annotations

import math

import mujoco
import numpy as np


def rotation(yaw: float) -> np.ndarray:
    c, s = math.cos(yaw), math.sin(yaw)
    return np.array([[c, -s, 0.0], [s, c, 0.0], [0.0, 0.0, 1.0]])


def smooth(t: float) -> float:
    t = min(1.0, max(0.0, t))
    return t * t * (3.0 - 2.0 * t)


def attitude(matrix: np.ndarray) -> tuple[float, float, float]:
    r = matrix.reshape(3, 3)
    return math.atan2(r[2, 1], r[2, 2]), math.asin(max(-1.0, min(1.0, -r[2, 0]))), math.atan2(r[1, 0], r[0, 0])


class LegIK:
    """Six joint damped inverse kinematics; uses a private, nonphysical data copy."""
    def __init__(self, model, qpos, root, sites, joints):
        self.model = model
        self.data = mujoco.MjData(model)
        self.data.qpos[:] = qpos
        self.root = root
        self.sites = sites
        self.qa = [model.jnt_qposadr[j] for j in joints]
        self.va = [model.jnt_dofadr[j] for j in joints]
        self.lower = model.jnt_range[joints, 0].copy()
        self.upper = model.jnt_range[joints, 1].copy()
        self.jp = np.zeros((3, model.nv))
        self.jr = np.zeros((3, model.nv))
        self.regularizer = np.eye(6) * 0.00001

    def solve(self, pelvis, yaw, feet, foot_yaws, seed, iterations=3, foot_index=None, base_quat=None):
        m, d = self.model, self.data
        d.qpos[:] = seed
        d.qpos[self.root:self.root + 3] = pelvis
        d.qpos[self.root + 3:self.root + 7] = [math.cos(yaw / 2), 0, 0, math.sin(yaw / 2)]
        if base_quat is not None:
            d.qpos[self.root + 3:self.root + 7] = base_quat
        goals = [rotation(y) for y in foot_yaws]
        for _ in range(iterations):
            converged = True
            mujoco.mj_kinematics(m, d)
            mujoco.mj_comPos(m, d)
            for side, site in enumerate(self.sites):
                if foot_index is not None and side != foot_index:
                    continue
                mujoco.mj_jacSite(m, d, self.jp, self.jr, site)
                r = d.site_xmat[site].reshape(3, 3)
                delta_r = goals[side] @ r.T
                er = 0.5 * np.array([delta_r[2, 1] - delta_r[1, 2], delta_r[0, 2] - delta_r[2, 0],
                                     delta_r[1, 0] - delta_r[0, 1]])
                error = np.concatenate((feet[side] - d.site_xpos[site], er))
                if max(abs(error)) < 1e-7:
                    continue
                converged = False
                ix = slice(side * 6, side * 6 + 6)
                q, v = self.qa[ix], self.va[ix]
                jac = np.concatenate((self.jp[:, v], self.jr[:, v]))
                delta = jac.T @ np.linalg.solve(jac @ jac.T + self.regularizer, error)
                d.qpos[q] += np.clip(delta, -0.15, 0.15)
                d.qpos[q] = np.clip(d.qpos[q], self.lower[ix], self.upper[ix])
            if converged:
                break
        return d.qpos.copy()
