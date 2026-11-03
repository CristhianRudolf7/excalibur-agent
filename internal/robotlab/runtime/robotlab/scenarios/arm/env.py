"""Gym environments and bounded damped-least-squares arm kinematics."""
from __future__ import annotations

import mujoco
import numpy as np

from robotlab.scenario import RobotEnv
from .model import BINS, COLORS, HOME, LIMITS, part_specs


class ArmEnv(RobotEnv):
    """Joint position control at 50 Hz; all simulation state belongs to this instance."""

    def __init__(self, scenario, reward_weights=None, randomize=False):
        self.target = np.array([.18, 0, 1.05])
        self.previous_action = np.zeros(7)
        self.parts = part_specs(scenario.params["objects"]) if scenario.params["task"] != "reach" else []
        self.goal_bin = "A"
        super().__init__(scenario, scenario.params, reward_weights, frame_skip=10,
                         max_episode_steps=200, randomize=randomize or scenario.params["randomize"])
        self.tcp_id = self.model.site("tcp").id
        self.reset(seed=self.params["seed"])

    @property
    def tcp(self) -> np.ndarray:
        """World position of the centre between the fingertips."""
        return self.data.site_xpos[self.model.site("tcp").id].copy()

    def normalized(self, targets: np.ndarray) -> np.ndarray:
        """Convert physical joint targets to the public normalised action space."""
        lo, hi = self.model.actuator_ctrlrange.T
        return np.clip(2 * (np.asarray(targets) - lo) / (hi - lo) - 1, -1, 1).astype(np.float32)

    def _observation(self) -> np.ndarray:
        q = self.data.qpos[:6]
        v = self.data.qvel[:6]
        tcp = self.tcp
        if self.params["task"] == "reach":
            return np.concatenate((q, v, self.target, self.target - tcp)).astype(np.float32)
        obj = self.data.body("part_1").xpos
        return np.concatenate((q, v, [self.data.qpos[6]], obj, obj - tcp,
                               np.array(BINS[self.goal_bin]) - obj)).astype(np.float32)

    def _reset_state(self) -> None:
        self.data.qpos[:6] = HOME
        self.data.qpos[6:8] = .045
        self.data.ctrl[:] = np.r_[HOME, .045]
        self.previous_action = self.normalized(self.data.ctrl)
        self.target = self.np_random.uniform([.03, -.22, .91], [.32, .22, 1.23])
        self.data.mocap_pos[0] = self.target
        for i, part in enumerate(self.parts):
            body = self.model.body(part["name"])
            adr = self.model.jnt_qposadr[body.jntadr[0]]
            # Stratified positions guarantee that even eight parts do not overlap.
            self.data.qpos[adr:adr + 3] = [-.02 + (i % 4) * .125 + self.np_random.uniform(-.008, .008),
                                          -.12 + (i // 4) * .24 + self.np_random.uniform(-.008, .008), .775]
            self.data.qpos[adr + 3:adr + 7] = [1, 0, 0, 0]
        if self.randomize:
            self.model.light_diffuse[:] = self.np_random.uniform(.6, 1, self.model.light_diffuse.shape)
            for part in self.parts:
                body = self.model.body(part["name"])
                color = np.clip(np.array(COLORS[part["color"]]) * self.np_random.uniform(.85, 1.15, 3), .05, 1)
                for gid in range(body.geomadr[0], body.geomadr[0] + body.geomnum[0]):
                    # Preserve the dark hole and defect marks while varying the painted surfaces.
                    if np.max(self.model.geom_rgba[gid, :3]) > .2:
                        self.model.geom_rgba[gid, :3] = color
        mujoco.mj_forward(self.model, self.data)
        # Settle loose objects and actuator transients before returning a reset frame.
        mujoco.mj_step(self.model, self.data, nstep=100)
        self.data.time = 0

    def step(self, action):
        """Advance the substeps in MuJoCo rather than repeatedly crossing Python/C."""
        action = np.asarray(action, dtype=np.float64)
        if action.shape != (7,) or not np.all(np.isfinite(action)):
            raise ValueError("arm action must contain seven finite targets")
        self.data.ctrl[:] = self.scale_action(action)
        mujoco.mj_step(self.model, self.data, nstep=self.frame_skip)
        self.steps += 1
        terms = self._reward_terms(action)
        reward = float(sum(self.weights.get(k, 0.) * value for k, value in terms.items()))
        return (self._observation().astype(np.float32), reward, bool(self._terminated()),
                self.steps >= self.max_episode_steps, {"terms": terms})

    def _reward_terms(self, action: np.ndarray) -> dict[str, float]:
        effort = -float(np.mean(np.square(action)))
        if self.params["task"] == "reach":
            dist = float(np.linalg.norm(self.target - self.tcp))
            terms = {"distance": -dist, "effort": effort,
                     "smooth": -float(np.mean(np.square(action - self.previous_action))),
                     "success": float(dist < .025)}
        else:
            pos = self.data.body("part_1").xpos
            dist = float(np.linalg.norm(pos - self.tcp))
            terms = {"reach": -dist, "grasp": float(self.grasped("part_1")),
                     "lift": float(np.clip((pos[2] - .77) / .15, 0, 1)),
                     "place": -float(np.linalg.norm(pos - BINS[self.goal_bin])),
                     "success": float(self.in_bin("part_1", self.goal_bin)), "effort": effort}
        self.previous_action = np.asarray(action).copy()
        return terms

    def grasped(self, name: str) -> bool:
        """Both fingers must contact the requested body; proximity alone is insufficient."""
        bid = self.model.body(name).id
        pads = {self.model.geom("left_pad").id, self.model.geom("right_pad").id}
        touched = set()
        for c in self.data.contact:
            if self.model.geom_bodyid[c.geom1] == bid and c.geom2 in pads:
                touched.add(c.geom2)
            if self.model.geom_bodyid[c.geom2] == bid and c.geom1 in pads:
                touched.add(c.geom1)
        return touched == pads

    def in_bin(self, name: str, target: str) -> bool:
        """Require the part to settle inside the bin, with the gripper released."""
        pos = self.data.body(name).xpos
        center = np.array(BINS[target])
        return bool(np.all(np.abs(pos[:2] - center[:2]) < [.067, .057]) and
                    .78 < pos[2] < .85 and self.data.qpos[6] > .03)

    def _terminated(self) -> bool:
        if self.params["task"] == "reach":
            return bool(np.linalg.norm(self.target - self.tcp) < .025)
        return self.in_bin("part_1", self.goal_bin)

    def ik(self, xyz, yaw: float = 0.0) -> np.ndarray:
        """Solve position and downward tool orientation without mutating the live simulation.

        Raises ValueError after a fixed iteration budget for an unreachable target.
        """
        target = np.asarray(xyz, dtype=float)
        if target.shape != (3,) or not np.all(np.isfinite(target)) or not np.isfinite(yaw):
            raise ValueError("move_to requires finite xyz[3] and yaw")
        if target[2] < .755:
            raise ValueError("target is below the safe table plane")
        scratch = mujoco.MjData(self.model)
        scratch.qpos[:] = self.data.qpos
        c, s = np.cos(yaw), np.sin(yaw)
        rotation = np.array([[c, s, 0], [s, -c, 0], [0, 0, -1.]])
        jp, jr = np.zeros((3, self.model.nv)), np.zeros((3, self.model.nv))
        best = float("inf")
        for start in (self.data.qpos[:6], HOME):
            scratch.qpos[:6] = start
            for _ in range(180):
                mujoco.mj_forward(self.model, scratch)
                pos_error = target - scratch.site_xpos[self.tcp_id]
                current = scratch.site_xmat[self.tcp_id].reshape(3, 3)
                # Quaternion logarithm remains informative at a 180-degree yaw error.
                quat = np.zeros(4)
                mujoco.mju_mat2Quat(quat, (rotation @ current.T).ravel())
                if quat[0] < 0:
                    quat *= -1
                length = np.linalg.norm(quat[1:])
                rot_error = (2 * np.arctan2(length, quat[0]) * quat[1:] / length
                             if length > 1e-10 else np.zeros(3))
                best = min(best, float(np.linalg.norm(pos_error)))
                if np.linalg.norm(pos_error) < .0005 and np.linalg.norm(rot_error) < .003 and np.trace(current.T @ rotation) > 2.9:
                    return scratch.qpos[:6].copy()
                mujoco.mj_jacSite(self.model, scratch, jp, jr, self.tcp_id)
                jac = np.vstack((jp[:, :6], .4 * jr[:, :6]))
                error = np.r_[pos_error, .4 * rot_error]
                dq = jac.T @ np.linalg.solve(jac @ jac.T + .002 ** 2 * np.eye(6), error)
                scratch.qpos[:6] = np.clip(scratch.qpos[:6] + np.clip(dq, -.15, .15), LIMITS[:, 0] + .001, LIMITS[:, 1] - .001)
        raise ValueError(f"IK target unreachable: position residual {best:.4f} m")
