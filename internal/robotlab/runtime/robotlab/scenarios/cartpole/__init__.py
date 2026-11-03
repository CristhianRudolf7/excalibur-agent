"""A force-driven cart balancing one to three serial pendulums."""

from __future__ import annotations

import os
from typing import Any

import mujoco
import numpy as np

from ...registry import register
from ...scenario import Actuator, RewardTerm, RobotEnv, Scenario


class CartPoleEnv(RobotEnv):
    """Continuous, one-dimensional control suitable for the SDK DQN wrapper."""

    def __init__(self, scenario: Scenario, reward_weights: dict[str, float] | None = None, randomize: bool = False) -> None:
        super().__init__(scenario, scenario.params, reward_weights, frame_skip=4,
                         max_episode_steps=1000, randomize=randomize)
        self._mass = self.model.body_mass.copy()
        self._inertia = self.model.body_inertia.copy()
        self._friction = self.model.geom_friction.copy()

    def _observation(self) -> np.ndarray:
        angles = np.cumsum(self.data.qpos[1:])
        return np.concatenate((self.data.qpos[:1], np.sin(angles), np.cos(angles),
                               self.data.qvel)).astype(np.float32)

    def _reward_terms(self, action: np.ndarray) -> dict[str, float]:
        angles = np.cumsum(self.data.qpos[1:])
        return {"upright": float(np.cos(angles).mean()),
                "centered": float(np.exp(-self.data.qpos[0] ** 2)),
                "effort": -float(np.square(np.clip(action, -1, 1)).sum()), "alive": 1.0}

    def _terminated(self) -> bool:
        if self.params["start"] == "hanging":
            return False
        angles = np.cumsum(self.data.qpos[1:])
        return bool(abs(self.data.qpos[0]) > 2.4 or np.any(np.cos(angles) < np.cos(np.pi / 4)))

    def _reset_state(self) -> None:
        self.model.body_mass[:] = self._mass
        self.model.body_inertia[:] = self._inertia
        self.model.geom_friction[:] = self._friction
        if self.randomize:
            factors = self.np_random.uniform(0.85, 1.15, self.model.nbody)
            self.model.body_mass[:] *= factors
            self.model.body_inertia[:] *= factors[:, None]
            self.model.geom_friction[:] *= self.np_random.uniform(0.85, 1.15, (self.model.ngeom, 1))
        mujoco.mj_setConst(self.model, self.data)
        self.data.qpos[:] = self.np_random.uniform(-0.025, 0.025, self.model.nq)
        if self.params["start"] == "hanging":
            self.data.qpos[1] += np.pi
        self.data.qvel[:] = self.np_random.uniform(-0.025, 0.025, self.model.nv)


@register
class CartPole(Scenario):
    """Balance upright poles or learn to swing them up from below the rail."""

    id = "cartpole"
    title = {"en": "Cart and poles", "es": "Carro y péndulos"}
    description = {
        "en": "Push a cart to balance one, two or three poles. Start below the rail to learn swing-up.",
        "es": "Empuja un carro para equilibrar uno, dos o tres péndulos. Empieza abajo para aprender a levantarlos.",
    }
    default_params = {"links": 1, "pole_length": 0.6, "cart_mass": 1.0,
                      "start": "upright", "max_force": 10.0}
    params_schema = {"type": "object", "additionalProperties": False, "properties": {
        "links": {"type": "integer", "enum": [1, 2, 3]},
        "pole_length": {"type": "number", "minimum": 0.3, "maximum": 1.2},
        "cart_mass": {"type": "number", "minimum": 0.3, "maximum": 5.0},
        "start": {"type": "string", "enum": ["upright", "hanging"]},
        "max_force": {"type": "number", "minimum": 1.0, "maximum": 30.0},
    }}
    actuators = [Actuator("cart_force", {"en": "Cart force", "es": "Fuerza del carro"},
                          "torque", [-10, 10], "ArrowLeft", "ArrowRight")]
    reward_terms = [
        RewardTerm("upright", 1.0, {"en": "Average pole height: +1 upright, −1 hanging.", "es": "Altura media: +1 arriba, −1 abajo."}),
        RewardTerm("centered", 0.1, {"en": "Keep the cart near the rail center.", "es": "Mantén el carro cerca del centro."}),
        RewardTerm("effort", 0.001, {"en": "Penalize squared force.", "es": "Penaliza la fuerza al cuadrado."}),
        RewardTerm("alive", 1.0, {"en": "Reward each balancing step.", "es": "Premia cada paso de equilibrio."}),
    ]
    algorithms = ["PPO", "SAC", "A2C", "DQN"]
    default_total_steps = 60_000
    thumbnail_color = "#167CBD"

    def __init__(self, params: dict[str, Any] | None = None) -> None:
        super().__init__(params)
        if set(self.params) - set(self.default_params):
            raise ValueError("unknown cartpole parameter")
        p = self.params
        if type(p["links"]) is not int or p["links"] not in (1, 2, 3):
            raise ValueError("links must be 1, 2 or 3")
        if p["start"] not in ("upright", "hanging"):
            raise ValueError("start must be upright or hanging")
        for name in ("pole_length", "cart_mass", "max_force"):
            spec = self.params_schema["properties"][name]
            value = p[name]
            if isinstance(value, bool) or not isinstance(value, (int, float)) or not spec["minimum"] <= value <= spec["maximum"]:
                raise ValueError(f"invalid {name}")

    def actuators_for(self, params: dict[str, Any]) -> list[Actuator]:
        return [Actuator("cart_force", self.actuators[0].label, "torque",
                         [-params["max_force"], params["max_force"]], "ArrowLeft", "ArrowRight")]

    def build_xml(self, params: dict[str, Any]) -> str:
        p = CartPole(params).params
        length = p["pole_length"]
        rail_height = max(1.5, p["links"] * length + 0.25)
        colors = ["0.08 0.36 0.85 1", "0.02 0.65 0.9 1", "0.2 0.85 0.9 1"]
        poles = ""
        for i in range(p["links"]):
            poles += f'''<body name="pole_{i}" pos="0 0 {0.1 if i == 0 else length}">
              <joint name="hinge_{i}" type="hinge" axis="0 1 0" damping="0.015"/>
              <geom name="pole_geom_{i}" type="capsule" fromto="0 0 0 0 0 {length}" size="0.025" mass="0.1" rgba="{colors[i]}"/>
              <geom type="sphere" size="0.04" mass="0.01" rgba="0.04 0.2 0.45 1"/>'''
        poles += "</body>" * p["links"]
        return f'''<mujoco model="cartpole"><compiler angle="radian"/>
          <option timestep="0.005" integrator="implicitfast"/>
          <default><geom contype="0" conaffinity="0"/></default>
          <visual><headlight ambient="0.4 0.4 0.4" diffuse="0.7 0.7 0.7"/></visual>
          <worldbody><light pos="0 -3 6"/>
            <geom name="floor" type="plane" size="6 4 0.1" rgba="0.93 0.95 0.98 1"/>
            <geom name="rail" type="box" pos="0 0 {rail_height}" size="2.8 0.08 0.05" rgba="0.13 0.23 0.4 1"/>
            <body name="cart" pos="0 0 {rail_height + 0.15}">
              <joint name="slide" type="slide" axis="1 0 0" range="-2.65 2.65" damping="0.1" solreflimit="0.04 1"/>
              <geom name="cart_geom" type="box" size="0.18 0.13 0.1" mass="{p['cart_mass']}" rgba="0.15 0.5 0.85 1"/>
              {poles}
            </body></worldbody>
          <actuator><motor name="cart_force" joint="slide" ctrlrange="-{p['max_force']} {p['max_force']}"/></actuator>
        </mujoco>'''

    def make_env(self, reward_weights: dict[str, float] | None = None, randomize: bool = False,
                 render_mode: str | None = None) -> CartPoleEnv:
        if os.getenv("EXCALIBUR_ROBOTLAB_CLASSIC", "on").strip().lower() in {"0", "off", "false", "no"}:
            raise RuntimeError("classic scenarios disabled by EXCALIBUR_ROBOTLAB_CLASSIC")
        return CartPoleEnv(self, reward_weights, randomize)

    def default_camera(self) -> dict[str, list[float]]:
        height = self.params["links"] * self.params["pole_length"]
        rail_height = max(1.5, height + 0.25)
        return {"pos": [3.8 + height, -5.0 - height, rail_height + 1.5 + height],
                "target": [0.0, 0.0, rail_height + 0.15 + height / 3]}
