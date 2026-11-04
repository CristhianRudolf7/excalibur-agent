"""Damped least-squares inverse kinematics on the world-frame grasp site."""
from __future__ import annotations

import math

import mujoco
import numpy as np


def down_rotation(yaw: float) -> np.ndarray:
    c, s = math.cos(yaw), math.sin(yaw)
    return np.array([[c, s, 0], [s, -c, 0], [0, 0, -1.]])


def grasp_rotation(yaw: float, pitch: float = 0., roll: float = 0.) -> np.ndarray:
    """World Z-Y-X angles relative to the fingers-down reference pose."""
    cy, sy, cp, sp, cr, sr = (math.cos(yaw), math.sin(yaw), math.cos(pitch),
                              math.sin(pitch), math.cos(roll), math.sin(roll))
    return np.array([[cy * cp, cy * sp * sr - sy * cr, cy * sp * cr + sy * sr],
                     [sy * cp, sy * sp * sr + cy * cr, sy * sp * cr - cy * sr],
                     [-sp, cp * sr, cp * cr]]) @ down_rotation(0.)


def grasp_angles(rotation: np.ndarray) -> list[float]:
    """Return yaw, pitch, roll in degrees, with zero meaning fingers down."""
    rot = rotation @ down_rotation(0.).T
    pitch = math.asin(float(np.clip(-rot[2, 0], -1., 1.)))
    if abs(math.cos(pitch)) > 1e-7:
        yaw, roll = math.atan2(rot[1, 0], rot[0, 0]), math.atan2(rot[2, 1], rot[2, 2])
    else:
        yaw, roll = math.atan2(-rot[0, 1], rot[1, 1]), 0.
    return [math.degrees(v) for v in (yaw, pitch, roll)]


def rotation_error(target: np.ndarray, current: np.ndarray) -> np.ndarray:
    tq, cq, inv, delta, vel = (np.empty(4), np.empty(4), np.empty(4),
                               np.empty(4), np.empty(3))
    mujoco.mju_mat2Quat(tq, target.ravel())
    mujoco.mju_mat2Quat(cq, current.ravel())
    mujoco.mju_negQuat(inv, cq)
    mujoco.mju_mulQuat(delta, tq, inv)
    if delta[0] < 0:
        delta *= -1
    mujoco.mju_quat2Vel(vel, delta, 1)
    return vel


class ArmIK:
    def __init__(self, model, site, qadr, dofs, limits):
        self.model, self.site = model, site
        self.qadr, self.dofs, self.limits = qadr, dofs, limits
        self.data = mujoco.MjData(model)
        self.jp = np.zeros((3, model.nv))
        self.jr = np.zeros((3, model.nv))
        self.eye = np.eye(6)

    def forward(self, q):
        self.data.qpos[self.qadr] = q
        mujoco.mj_kinematics(self.model, self.data)
        mujoco.mj_comPos(self.model, self.data)

    def solve(self, pos, rot, seed, iterations=100):
        q = np.asarray(seed, dtype=float).copy()
        for _ in range(iterations):
            self.forward(q)
            ep = pos - self.data.site_xpos[self.site]
            er = rotation_error(rot, self.data.site_xmat[self.site].reshape(3, 3))
            if np.linalg.norm(ep) < 0.0003 and np.linalg.norm(er) < 0.002:
                return q
            mujoco.mj_jac(self.model, self.data, self.jp, self.jr,
                          self.data.site_xpos[self.site], self.model.site_bodyid[self.site])
            jac = np.vstack((self.jp[:, self.dofs], 0.3 * self.jr[:, self.dofs]))
            error = np.concatenate((ep, 0.3 * er))
            dq = jac.T @ np.linalg.solve(jac @ jac.T + 0.000025 * self.eye, error)
            dq *= min(1., 0.18 / max(np.max(np.abs(dq)), 1e-9))
            q = np.clip(q + dq, self.limits[:, 0], self.limits[:, 1])
        return None
