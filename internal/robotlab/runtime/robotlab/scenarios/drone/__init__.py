"""A physically actuated quadcopter with optional assisted human controls."""
from __future__ import annotations

import os
from pathlib import Path
from typing import Any

import mujoco
import numpy as np

from robotlab.registry import register
from robotlab.scenario import Actuator, RewardTerm, RobotEnv, Scenario


def text(value: str, spanish: str) -> dict[str, str]:
    """Return localized UI metadata."""
    return {"en": value, "es": spanish}


@register
class Drone(Scenario):
    """Four thrust motors with mixed training actions and assisted human controls."""

    id = "drone"
    title = text("Quadcopter", "Cuadricóptero")
    description = text("Hover, follow waypoints, or hold position in gusting wind.",
                       "Mantén el vuelo estacionario, sigue una ruta o resiste las ráfagas de viento.")
    category = "flight"
    thumbnail_color = "#29B8C6"
    default_params = {"task": "hover", "wind_strength": 1.0, "mass": 1.0,
                      "waypoints": [[0, 0, 1.5], [2, 0, 1.5], [2, 2, 1.5], [0, 2, 1.5]],
                      "manual_mode": "assisted", "action_mode": "mixed"}
    params_schema = {"type": "object", "additionalProperties": False, "properties": {
        "task": {"type": "string", "enum": ["hover", "waypoints", "wind"]},
        "wind_strength": {"type": "number", "minimum": 0, "maximum": 10},
        "mass": {"type": "number", "minimum": 0.2, "maximum": 5},
        "waypoints": {"type": "array", "minItems": 1, "maxItems": 32, "items": {
            "type": "array", "minItems": 3, "maxItems": 3,
            "items": {"type": "number", "minimum": -4, "maximum": 8}}},
        "action_mode": {"type": "string", "enum": ["mixed", "raw"]},
        "manual_mode": {"type": "string", "enum": ["assisted", "raw"]}}}
    actuators = [Actuator(f"motor{i+1}", text(label, spanish), "torque", [0, 4.905], neg, pos)
                 for i, (label, spanish, neg, pos) in enumerate([
                     ("Throttle / front left", "Acelerador / delantero izquierdo", "KeyS", "KeyW"),
                     ("Yaw / front right", "Guiñada / delantero derecho", "KeyA", "KeyD"),
                     ("Pitch / rear right", "Cabeceo / trasero derecho", "ArrowDown", "ArrowUp"),
                     ("Roll / rear left", "Alabeo / trasero izquierdo", "ArrowLeft", "ArrowRight")])]
    reward_terms = [RewardTerm(name, weight, text(description, spanish)) for name, weight, description, spanish in [
        ("distance", 2.0, "Negative distance to the active goal.", "Penaliza la distancia al objetivo actual."),
        ("upright", 3.0, "Body vertical alignment with world up.", "Premia mantener el cuerpo alineado con la vertical."),
        ("velocity", 0.15, "Negative linear and angular speed.", "Penaliza la velocidad lineal y angular."),
        ("effort", 0.05, "Negative squared motor action.", "Penaliza el esfuerzo de los motores al cuadrado."),
        ("waypoint", 10.0, "Bonus when entering a waypoint radius.", "Bonificación al alcanzar un punto de la ruta."),
        ("crash", 50.0, "Penalty for ground impact, flip, or leaving the flight box.", "Penaliza los impactos, los vuelcos y salir de la zona de vuelo.")]]
    default_total_steps = 1_048_576
    has_pretrained = True
    training_randomize = True

    def __init__(self, params: dict[str, Any] | None = None) -> None:
        super().__init__(params)
        p = self.params
        if p["task"] not in ("hover", "waypoints", "wind") or p["manual_mode"] not in ("assisted", "raw") or p["action_mode"] not in ("mixed", "raw"):
            raise ValueError("invalid drone task, manual_mode or action_mode")
        for name, lo, hi in (("mass", .2, 5), ("wind_strength", 0, 10)):
            if not np.isfinite(p[name]) or not lo <= p[name] <= hi:
                raise ValueError(f"{name} outside supported range")
        goals = np.asarray(p["waypoints"], dtype=float)
        if goals.ndim != 2 or goals.shape[1] != 3 or not 1 <= len(goals) <= 32 or not np.isfinite(goals).all():
            raise ValueError("waypoints must contain 1 to 32 finite xyz positions")
        if np.any(np.abs(goals[:, :2]) > 4) or np.any(goals[:, 2] < .4) or np.any(goals[:, 2] > 8):
            raise ValueError("waypoints must be inside the flight box and above ground")

    def actuators_for(self, params: dict[str, Any]) -> list[Actuator]:
        """Return physical control ranges for the selected mass or gravity."""
        return [Actuator(a.name, a.label, a.kind, [0, float(params["mass"]) * 9.81 / 2],
                         a.key_neg, a.key_pos) for a in self.actuators]

    def build_xml(self, params: dict[str, Any]) -> str:
        """Build the styled world entirely from MuJoCo primitive geoms."""
        mass = float(params["mass"])
        rotors, motors = [], []
        for i, (x, y, spin) in enumerate(((.23, .23, 1), (.23, -.23, -1), (-.23, -.23, 1), (-.23, .23, -1))):
            color = '.15 .85 .85 1' if x > 0 else '.95 .55 .18 1'
            rotors.append(f'''<geom type="capsule" fromto="0 0 0 {x} {y} 0" size=".025" rgba=".18 .24 .32 1"/>
                <geom type="cylinder" pos="{x} {y} .025" size=".115 .009" rgba="{color}"/>
                <geom type="cylinder" pos="{x} {y} 0" size=".035 .035" rgba=".08 .12 .2 1"/>
                <site name="rotor{i}" pos="{x} {y} .03" size=".008"/>''')
            motors.append(f'<general name="motor{i+1}" site="rotor{i}" gear="0 0 1 0 0 {spin*.025}" ctrlrange="0 {mass*9.81/2}" ctrllimited="true"/>')
        markers = ''.join(f'<geom name="waypoint{i}" type="sphere" pos="{x} {y} {z}" size=".07" rgba=".2 .65 1 .3" contype="0" conaffinity="0"/>'
                          for i, (x, y, z) in enumerate(params["waypoints"]))
        return f'''<mujoco model="quadcopter"><option timestep=".005" gravity="0 0 -9.81" integrator="RK4"/>
          <visual><headlight ambient=".4 .4 .45" diffuse=".8 .8 .8"/></visual>
          <worldbody><light pos="0 -3 8"/><geom name="floor" type="plane" size="6 6 .1" rgba=".09 .13 .2 1"/>
          <geom type="cylinder" pos="0 0 .005" size=".6 .005" rgba=".15 .3 .4 1"/>{markers}
          <body name="goal" mocap="true" pos="0 0 1.5"><geom type="sphere" size=".11" rgba=".3 1 .6 .6" contype="0" conaffinity="0"/></body>
          <body name="drone" pos="0 0 1.5"><joint type="free" damping=".03"/>
          <inertial pos="0 0 0" mass="{mass}" diaginertia="{mass*.025} {mass*.025} {mass*.045}"/>
          <geom name="hull" type="box" size=".12 .085 .045" rgba=".83 .9 .96 1"/>
          <geom type="ellipsoid" pos=".09 0 .045" size=".04 .055 .025" rgba=".08 .2 .35 1"/>
          {''.join(rotors)}<geom type="capsule" fromto="-.1 -.08 -.08 .1 -.08 -.08" size=".015"/>
          <geom type="capsule" fromto="-.1 .08 -.08 .1 .08 -.08" size=".015"/>
          </body><camera name="overview" pos="4 -5 3.5" xyaxes=".78 .62 0 -.3 .38 .88"/></worldbody>
          <actuator>{''.join(motors)}</actuator></mujoco>'''

    def make_env(self, reward_weights: dict[str, float] | None = None, randomize: bool = False,
                 render_mode: str | None = None) -> RobotEnv:
        """Create a bounded training or interactive world, honoring the flight switch."""
        if os.getenv("EXCALIBUR_ROBOTLAB_FLIGHT", "on").strip().lower() in ("0", "off", "false", "no"):
            raise RuntimeError("flight scenarios disabled by EXCALIBUR_ROBOTLAB_FLIGHT")
        return DroneEnv(self, self.params, reward_weights, randomize=randomize)

    def manual_action(self, env: DroneEnv, inputs: dict[str, float]) -> np.ndarray:
        """Mix human flight commands into rotor thrust, or pass raw sliders through."""
        if env.params["manual_mode"] == "raw":
            return env.action_from_motors(super().manual_action(env, inputs))
        throttle, yaw, pitch, roll = [float(np.clip(inputs.get(f"motor{i}", 0), -1, 1)) for i in range(1, 5)]
        rotation = env.data.xmat[env.body].reshape(3, 3)
        # Velocity and attitude feedback keep neutral input in a stationary hover.
        env.manual_height = float(np.clip(env.manual_height + throttle * env.dt * 1.5, .4, 8))
        desired_acc = -1.5 * env.data.qvel[:3]
        desired_acc[2] += 3 * (env.manual_height - env.data.qpos[2])
        target_angles = np.array([.22 * roll + desired_acc[1] / -9.81,
                                  .22 * pitch + desired_acc[0] / 9.81])
        angles = np.array([np.arctan2(rotation[2, 1], rotation[2, 2]),
                           np.arcsin(np.clip(-rotation[2, 0], -1, 1))])
        torque = np.r_[.8 * (target_angles - angles) - .22 * env.data.qvel[3:5],
                       .10 * (-yaw - env.data.qvel[5])]
        force = env.params["mass"] * (9.81 + desired_acc[2]) / max(rotation[2, 2], .5)
        mixer = np.array([[1, 1, 1, 1], [.23, -.23, -.23, .23], [-.23, -.23, .23, .23], [.025, -.025, .025, -.025]])
        thrust = np.linalg.solve(mixer, np.r_[force, torque])
        motors = np.clip(thrust / (env.params["mass"] * 9.81 / 4) - 1, -1, 1)
        return env.action_from_motors(motors)

    def idle_action(self, env: DroneEnv) -> np.ndarray:
        """Stabilize the retained altitude target while assisted controls are idle."""
        return self.manual_action(env, {})

    def pretrained_policy(self) -> str | None:
        """Return the hover checkpoint only for its matching action convention."""
        if self.params["action_mode"] != "mixed" or self.params["task"] != "hover":
            return None
        return str(Path(__file__).parent / "pretrained" / "hover.zip")

    def default_camera(self) -> dict[str, list[float]]:
        """Return an overview in world z-up coordinates."""
        return {"pos": [4, -5, 3.5], "target": [0, 0, 1.2]}


class DroneEnv(RobotEnv):
    """Hover and waypoint flight at 50 control steps per second."""

    def __init__(self, *args: Any, **kwargs: Any) -> None:
        self.goal = np.array([0., 0., 1.5])
        self.goal_index = 0
        self.waypoint_bonus = 0.0
        self.manual_height = 1.5
        super().__init__(*args, frame_skip=4, max_episode_steps=750, **kwargs)
        self.body = self.model.body("drone").id

    # Columns are collective, roll, pitch and yaw. No feedback is applied here.
    allocation = np.array([[1, .015, -.015, .05], [1, -.015, -.015, -.05],
                           [1, -.015, .015, .05], [1, .015, .015, -.05]])

    def action_from_motors(self, motors: np.ndarray) -> np.ndarray:
        """Encode physical motor commands for manual and idle playback.

        Assisted commands can require more torque than the learning allocation;
        retaining the inverse outside the Box avoids clipping away stabilization.
        The physics step always clips the resulting motor commands.
        """
        if self.params["action_mode"] == "raw":
            return np.asarray(motors, dtype=np.float32)
        return np.linalg.solve(self.allocation, motors).astype(np.float32)

    def _observation(self) -> np.ndarray:
        """Expose relative position, attitude, velocities and scenario-specific state."""
        return np.r_[self.goal - self.data.qpos[:3], self.data.qpos[3:7], self.data.qvel[:6]].astype(np.float32)

    def _reset_state(self) -> None:
        """Reset targets and disturbances using only the environment random generator."""
        self.goal_index = 0
        self.goal = np.array(self.params["waypoints"][0] if self.params["task"] == "waypoints" else [0, 0, 1.5], dtype=float)
        self.data.qpos[:3] = [0, 0, 1.5]
        if self.randomize:
            self.data.qpos[:3] += self.np_random.uniform(-.1, .1, 3)
            self.data.qvel[:3] = self.np_random.uniform(-.1, .1, 3)
        self.manual_height = float(self.data.qpos[2])
        self.waypoint_bonus = 0.0
        self.data.mocap_pos[0] = self.goal
        self.wind_phase = self.np_random.uniform(0, 2*np.pi)

    def step(self, action: np.ndarray) -> tuple[np.ndarray, float, bool, bool, dict[str, Any]]:
        """Advance bounded physics and update goals, fuel or terminal outcomes."""
        self.waypoint_bonus = 0.0
        self.data.xfrc_applied[self.body] = 0
        if self.params["task"] == "wind":
            t = self.data.time + self.wind_phase
            self.data.xfrc_applied[self.body, :2] = self.params["wind_strength"] * np.array([np.sin(t), .6*np.cos(.7*t)])
        motors = (self.allocation @ np.asarray(action)
                  if self.params["action_mode"] == "mixed" else action)
        result = super().step(np.clip(motors, -1, 1))
        if self.params["task"] == "waypoints" and not result[2] and np.linalg.norm(self.goal-self.data.qpos[:3]) < .25:
            self.waypoint_bonus = 1.0
            self.goal_index = (self.goal_index + 1) % len(self.params["waypoints"])
            self.goal = np.array(self.params["waypoints"][self.goal_index], dtype=float)
            self.data.mocap_pos[0] = self.goal
            obs, reward, terminal, truncated, info = result
            info["terms"]["waypoint"] = 1.0
            result = (self._observation(), reward+self.weights["waypoint"], terminal, truncated, info)
        result[4].update(goal_index=self.goal_index, crashed=self._terminated())
        return result

    def _reward_terms(self, action: np.ndarray) -> dict[str, float]:
        """Return unweighted terms so experiments can inspect or replace weights."""
        return {"distance": -float(np.linalg.norm(self.goal-self.data.qpos[:3])),
                "upright": float(self.data.xmat[self.body, 8]),
                "velocity": -float(np.linalg.norm(self.data.qvel[:3]) + .1*np.linalg.norm(self.data.qvel[3:6])),
                "effort": -float(np.mean(np.square(action))), "waypoint": self.waypoint_bonus,
                "crash": -float(self._terminated())}

    def _terminated(self) -> bool:
        """Report a physical failure or a completed landing."""
        return bool(self.data.qpos[2] < .12 or self.data.qpos[2] > 10 or
                    np.max(np.abs(self.data.qpos[:2])) > 5 or
                    self.data.xmat[self.body, 8] < .25)
