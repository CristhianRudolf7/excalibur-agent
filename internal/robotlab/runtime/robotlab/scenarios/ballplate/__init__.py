"""A rolling ball controlled by a two-axis tilting plate."""

from __future__ import annotations

import os
from typing import Any

import mujoco
import numpy as np

from ...registry import register
from ...scenario import Actuator, RewardTerm, RobotEnv, Scenario


class BallPlateEnv(RobotEnv):
    """Position-controlled gimbal with targets expressed in plate coordinates."""

    def __init__(self, scenario: Scenario, reward_weights: dict[str, float] | None = None, randomize: bool = False) -> None:
        self.previous_action = np.zeros(2)
        self.target = np.zeros(2)
        super().__init__(scenario, scenario.params, reward_weights, frame_skip=4,
                         max_episode_steps=500, randomize=randomize)
        self._mass = self.model.body_mass.copy()
        self._inertia = self.model.body_inertia.copy()
        self._friction = self.model.geom_friction.copy()

    def _ball_state(self) -> tuple[np.ndarray, np.ndarray]:
        plate = self.model.body("plate").id
        rotation = self.data.xmat[plate].reshape(3, 3)
        position = rotation.T @ (self.data.body("ball").xpos - self.data.xpos[plate])
        velocity = rotation.T @ self.data.qvel[2:5]
        return position, velocity

    def _observation(self) -> np.ndarray:
        position, velocity = self._ball_state()
        size = self.params["plate_size"] / 2
        phase = self.data.time * (2 * np.pi / 8) if self.params["task"] == "circle" else 0.0
        return np.concatenate((position[:2] / size, velocity[:2], self.target / size,
                               (self.target - position[:2]) / size, self.data.qpos[:2],
                               self.data.qvel[:2], self.previous_action,
                               [np.sin(phase), np.cos(phase)])).astype(np.float32)

    def _reward_terms(self, action: np.ndarray) -> dict[str, float]:
        mujoco.mj_forward(self.model, self.data)
        position, _ = self._ball_state()
        clipped = np.clip(action, -1, 1)
        return {"distance": -float(np.linalg.norm(position[:2] - self.target)),
                "on_plate": float(not self._terminated()),
                "smooth": -float(np.square(clipped - self.previous_action).sum()),
                "effort": -float(np.square(clipped).sum())}

    def _terminated(self) -> bool:
        position, _ = self._ball_state()
        return bool(np.any(np.abs(position[:2]) > self.params["plate_size"] / 2 + self.params["ball_radius"])
                    or position[2] < -0.15)

    def _move_target(self) -> None:
        if self.params["task"] == "circle":
            phase = self.data.time * (2 * np.pi / 8)
            self.target[:] = self.params["plate_size"] * 0.22 * np.array([np.cos(phase), np.sin(phase)])
        rotation = self.data.body("plate").xmat.reshape(3, 3)
        self.data.mocap_pos[0] = self.data.body("plate").xpos + rotation @ np.r_[self.target, 0.028]
        self.data.mocap_quat[0] = self.data.body("plate").xquat
        mujoco.mj_forward(self.model, self.data)

    def step(self, action: np.ndarray) -> tuple[np.ndarray, float, bool, bool, dict[str, Any]]:
        # Compute smoothness before storing the current action as history.
        result = super().step(action)
        self.previous_action[:] = np.clip(action, -1, 1)
        self._move_target()
        terms = result[4]["terms"]
        terms["distance"] = -float(np.linalg.norm(self._ball_state()[0][:2] - self.target))
        reward = float(sum(self.weights[k] * value for k, value in terms.items()))
        return self._observation(), reward, result[2], result[3], result[4]

    def _reset_state(self) -> None:
        self.previous_action[:] = 0
        self.target[:] = 0
        self.model.body_mass[:] = self._mass
        self.model.body_inertia[:] = self._inertia
        self.model.geom_friction[:] = self._friction
        if self.randomize:
            factors = self.np_random.uniform(0.85, 1.15, self.model.nbody)
            self.model.body_mass[:] *= factors
            self.model.body_inertia[:] *= factors[:, None]
            self.model.geom_friction[:] *= self.np_random.uniform(0.85, 1.15, (self.model.ngeom, 1))
        mujoco.mj_setConst(self.model, self.data)
        self.data.qpos[:2] = self.np_random.uniform(-0.008, 0.008, 2)
        self.data.qpos[2:4] = self.np_random.uniform(-0.15, 0.15, 2) * self.params["plate_size"]
        self.data.qpos[4] = 0.675 + self.params["ball_radius"] + 0.002
        self.data.qvel[2:4] = self.np_random.uniform(-0.015, 0.015, 2)
        if self.params["task"] == "target":
            self.target[:] = self.np_random.uniform(-0.3, 0.3, 2) * self.params["plate_size"]
        mujoco.mj_forward(self.model, self.data)
        self._move_target()


@register
class BallPlate(Scenario):
    """Keep a ball centered, reach a target, or follow a moving circle."""

    id = "ballplate"
    title = {"en": "Ball on a plate", "es": "Bola sobre una plataforma"}
    description = {
        "en": "Tilt a plate to keep a rolling ball at the center, reach a marker or follow a circle.",
        "es": "Inclina una plataforma para centrar una bola, alcanzar una marca o seguir un círculo.",
    }
    default_params = {"task": "center", "plate_size": 1.2, "ball_radius": 0.045, "friction": 0.6}
    params_schema = {"type": "object", "additionalProperties": False, "properties": {
        "task": {"type": "string", "enum": ["center", "target", "circle"]},
        "plate_size": {"type": "number", "minimum": 0.6, "maximum": 2.0},
        "ball_radius": {"type": "number", "minimum": 0.02, "maximum": 0.09},
        "friction": {"type": "number", "minimum": 0.1, "maximum": 1.2},
    }}
    actuators = [
        Actuator("tilt_x", {"en": "Tilt around X", "es": "Inclinación en X"}, "position", [-0.2, 0.2], "KeyS", "KeyW"),
        Actuator("tilt_y", {"en": "Tilt around Y", "es": "Inclinación en Y"}, "position", [-0.2, 0.2], "KeyA", "KeyD"),
    ]
    reward_terms = [
        RewardTerm("distance", 4.0, {"en": "Negative distance to the marker in meters.", "es": "Distancia negativa a la marca en metros."}),
        RewardTerm("on_plate", 1.0, {"en": "Keep the ball on the plate.", "es": "Mantén la bola sobre la plataforma."}),
        RewardTerm("smooth", 0.01, {"en": "Penalize changes in tilt commands.", "es": "Penaliza cambios en la inclinación."}),
        RewardTerm("effort", 0.001, {"en": "Penalize squared tilt commands.", "es": "Penaliza la inclinación al cuadrado."}),
    ]
    algorithms = ["PPO", "SAC", "A2C"]
    default_total_steps = 200_000
    thumbnail_color = "#12A6C4"

    def __init__(self, params: dict[str, Any] | None = None) -> None:
        super().__init__(params)
        if set(self.params) - set(self.default_params):
            raise ValueError("unknown ballplate parameter")
        if self.params["task"] not in ("center", "target", "circle"):
            raise ValueError("task must be center, target or circle")
        for name in ("plate_size", "ball_radius", "friction"):
            spec = self.params_schema["properties"][name]
            value = self.params[name]
            if isinstance(value, bool) or not isinstance(value, (int, float)) or not spec["minimum"] <= value <= spec["maximum"]:
                raise ValueError(f"invalid {name}")

    def build_xml(self, params: dict[str, Any]) -> str:
        p = BallPlate(params).params
        half = p["plate_size"] / 2
        return f'''<mujoco model="ballplate"><compiler angle="radian"/>
          <option timestep="0.005" integrator="implicitfast" cone="elliptic" iterations="30"/>
          <default><joint damping="0.3"/><geom friction="{p['friction']} 0.005 0.001" solref="0.015 1" solimp="0.95 0.99 0.001"/></default>
          <visual><headlight ambient="0.4 0.4 0.4" diffuse="0.7 0.7 0.7"/></visual>
          <worldbody><light pos="0 -2 4"/>
            <geom name="floor" type="plane" size="4 4 0.1" rgba="0.93 0.95 0.98 1"/>
            <geom name="pedestal" type="cylinder" pos="0 0 0.27" size="0.12 0.27" contype="0" conaffinity="0" rgba="0.13 0.23 0.4 1"/>
            <body name="gimbal" pos="0 0 0.65"><joint name="axis_x" type="hinge" axis="1 0 0" range="-0.23 0.23"/>
              <geom type="cylinder" size="0.07 0.04" mass="0.2" contype="0" conaffinity="0" rgba="0.08 0.36 0.85 1"/>
              <body name="plate"><joint name="axis_y" type="hinge" axis="0 1 0" range="-0.23 0.23"/>
                <geom name="surface" type="box" size="{half} {half} 0.025" mass="2" rgba="0.15 0.5 0.85 1" condim="6"/>
              </body></body>
            <body name="ball" pos="0 0 {0.675 + p['ball_radius']}"><freejoint/>
              <geom name="ball_geom" type="sphere" size="{p['ball_radius']}" mass="0.08" rgba="0.1 0.85 0.9 1" condim="6"/>
            </body>
            <body name="target" mocap="true" pos="0 0 0.678"><geom name="target_marker" type="cylinder" size="0.035 0.002" contype="0" conaffinity="0" rgba="1 0.72 0.16 0.85"/></body>
          </worldbody><actuator>
            <position name="tilt_x" joint="axis_x" kp="40" kv="6" ctrlrange="-0.2 0.2" forcerange="-8 8"/>
            <position name="tilt_y" joint="axis_y" kp="40" kv="6" ctrlrange="-0.2 0.2" forcerange="-8 8"/>
          </actuator></mujoco>'''

    def make_env(self, reward_weights: dict[str, float] | None = None, randomize: bool = False,
                 render_mode: str | None = None) -> BallPlateEnv:
        if os.getenv("EXCALIBUR_ROBOTLAB_CLASSIC", "on").strip().lower() in {"0", "off", "false", "no"}:
            raise RuntimeError("classic scenarios disabled by EXCALIBUR_ROBOTLAB_CLASSIC")
        return BallPlateEnv(self, reward_weights, randomize)

    def default_camera(self) -> dict[str, list[float]]:
        size = self.params["plate_size"]
        return {"pos": [1.7 * size, -1.9 * size, 0.8 + 1.5 * size], "target": [0.0, 0.0, 0.65]}
