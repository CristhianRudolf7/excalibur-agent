"""Finite-fuel vertical landing with a physical two-axis engine gimbal."""
from __future__ import annotations

import os
from typing import Any

import mujoco
import numpy as np

from robotlab.registry import register
from robotlab.scenario import Actuator, RewardTerm, RobotEnv, Scenario


def text(value: str, spanish: str) -> dict[str, str]:
    """Return localized UI metadata."""
    return {"en": value, "es": spanish}


@register
class Rocket(Scenario):
    """A reusable lander whose controls spend a finite impulse budget."""

    id = "rocket"
    title = text("Rocket landing", "Aterrizaje de cohete")
    description = text("Land upright on the pad using thrust, gimbal and attitude jets.", "Aterriza en vertical usando el motor orientable y los propulsores de actitud.")
    category = "flight"
    thumbnail_color = "#F29B50"
    default_params = {"start_height": 5.0, "wind": 0.0, "fuel": 180.0, "gravity": "earth"}
    params_schema = {"type": "object", "additionalProperties": False, "properties": {
        "start_height": {"type": "number", "minimum": 1.5, "maximum": 20},
        "wind": {"type": "number", "minimum": 0, "maximum": 10},
        "fuel": {"type": "number", "minimum": 0, "maximum": 1000},
        "gravity": {"type": "string", "enum": ["earth", "moon", "mars"]}}}
    actuators = [Actuator("thrust", text("Main throttle", "Potencia del motor principal"), "torque", [0, 36.7875], "KeyS", "KeyW"),
                 Actuator("gimbal_x", text("Engine roll gimbal", "Inclinación lateral del motor"), "position", [-.25, .25], "ArrowLeft", "ArrowRight"),
                 Actuator("gimbal_y", text("Engine pitch gimbal", "Inclinación longitudinal del motor"), "position", [-.25, .25], "ArrowDown", "ArrowUp"),
                 Actuator("attitude_x", text("Roll attitude jets", "Propulsores de alabeo"), "torque", [-.35, .35], "KeyQ", "KeyE"),
                 Actuator("attitude_y", text("Pitch attitude jets", "Propulsores de cabeceo"), "torque", [-.35, .35], "KeyA", "KeyD")]
    reward_terms = [RewardTerm(name, weight, text(description, spanish)) for name, weight, description, spanish in [
        ("distance_to_pad", .15, "Negative distance from the landing configuration.", "Penaliza la distancia a la posición de aterrizaje."),
        ("speed", .15, "Negative linear and angular speed.", "Penaliza la velocidad lineal y angular."),
        ("tilt", 2.0, "Negative loss of vertical alignment.", "Penaliza la desviación de la vertical."),
        ("fuel", .1, "Negative fraction of full thrust used this step.", "Penaliza la fracción de potencia consumida en este paso."),
        ("landed", 200.0, "Soft upright touchdown inside the pad.", "Bonificación por aterrizar suavemente y en vertical sobre la plataforma."),
        ("crash", 100.0, "Hard touchdown, pad miss, boundary exit, or fuel exhaustion aloft.", "Penaliza los impactos, aterrizar fuera de la plataforma, salir de los límites o agotar el combustible en vuelo.")]]
    default_total_steps = 500_000

    def __init__(self, params: dict[str, Any] | None = None) -> None:
        super().__init__(params)
        for name, lo, hi in (("start_height", 1.5, 20), ("wind", 0, 10), ("fuel", 0, 1000)):
            if not np.isfinite(self.params[name]) or not lo <= self.params[name] <= hi:
                raise ValueError(f"{name} outside supported range")
        if self.params["gravity"] not in ("earth", "moon", "mars"):
            raise ValueError("unknown gravity")

    def actuators_for(self, params: dict[str, Any]) -> list[Actuator]:
        """Return physical control ranges for the selected mass or gravity."""
        thrust = max(10., 1.5 * {"earth": 9.81, "moon": 1.62, "mars": 3.71}[params["gravity"]] * 2.5)
        return [Actuator(a.name, a.label, a.kind, [0, thrust] if i == 0 else a.range,
                         a.key_neg, a.key_pos) for i, a in enumerate(self.actuators)]

    def build_xml(self, params: dict[str, Any]) -> str:
        """Build the styled world entirely from MuJoCo primitive geoms."""
        gravity = {"earth": 9.81, "moon": 1.62, "mars": 3.71}[params["gravity"]]
        thrust = self.actuators_for(params)[0].range[1]
        legs = ''.join(f'<geom name="leg{i}" type="capsule" fromto="{x*.18} {y*.18} -.4 {x*.43} {y*.43} -.87" size=".035" rgba=".25 .35 .45 1"/>'
                       for i, (x, y) in enumerate(((1, 1), (1, -1), (-1, 1), (-1, -1))))
        return f'''<mujoco model="landing rocket"><option timestep=".005" gravity="0 0 -{gravity}" integrator="implicitfast"/>
          <visual><headlight ambient=".4 .4 .45" diffuse=".8 .8 .8"/></visual>
          <default><geom friction=".9 .01 .001"/><joint damping=".04"/></default>
          <worldbody><light pos="-3 -4 12"/><geom name="ground" type="plane" pos="0 0 -.002" size="15 15 .1" rgba=".09 .13 .2 1"/>
          <geom name="pad" type="cylinder" pos="0 0 -.025" size="1.3 .025" rgba=".2 .35 .44 1"/>
          <geom type="box" pos="0 0 .001" size=".65 .07 .001" rgba=".4 .95 .8 1" contype="0" conaffinity="0"/>
          <geom type="box" pos="0 0 .001" size=".07 .65 .001" rgba=".4 .95 .8 1" contype="0" conaffinity="0"/>
          <body name="rocket" pos="0 0 {params['start_height']}"><freejoint/>
          <inertial pos="0 0 0" mass="1.48" diaginertia=".32 .32 .045"/>
          <geom name="fuselage" type="cylinder" size=".18 .6" rgba=".86 .91 .96 1"/>
          <geom type="ellipsoid" pos="0 0 .6" size=".18 .18 .28" rgba=".95 .55 .2 1"/>
          <geom type="cylinder" pos="0 0 .32" size=".184 .045" rgba=".12 .45 .65 1"/>
          <geom type="cylinder" pos="0 0 -.3" size=".184 .035" rgba=".12 .45 .65 1"/>{legs}
          <site name="attitude" pos="0 0 .35" size=".01"/>
          <geom type="capsule" fromto="-.25 0 .35 .25 0 .35" size=".035" rgba=".2 .3 .4 1"/>
          <body name="engine_x" pos="0 0 -.58"><joint name="hinge_x" type="hinge" axis="1 0 0" range="-18 18"/>
          <inertial pos="0 0 0" mass=".01" diaginertia=".0001 .0001 .0001"/>
          <body name="engine_y"><joint name="hinge_y" type="hinge" axis="0 1 0" range="-18 18"/>
          <inertial pos="0 0 0" mass=".01" diaginertia=".0001 .0001 .0001"/>
          <geom type="cylinder" pos="0 0 -.06" size=".1 .08" rgba=".18 .24 .3 1" contype="0" conaffinity="0"/>
          <site name="engine" pos="0 0 -.12" size=".02" rgba="1 .6 .1 1"/>
          </body></body></body>
          <camera name="overview" pos="9 -12 7" xyaxes=".8 .6 0 -.22 .3 .93"/></worldbody>
          <actuator><general name="thrust" site="engine" gear="0 0 1 0 0 0" ctrlrange="0 {thrust}" ctrllimited="true"/>
          <position name="gimbal_x" joint="hinge_x" kp="3" kv=".1" ctrlrange="-.25 .25" ctrllimited="true"/>
          <position name="gimbal_y" joint="hinge_y" kp="3" kv=".1" ctrlrange="-.25 .25" ctrllimited="true"/>
          <general name="attitude_x" site="attitude" gear="0 0 0 1 0 0" ctrlrange="-.35 .35" ctrllimited="true"/>
          <general name="attitude_y" site="attitude" gear="0 0 0 0 1 0" ctrlrange="-.35 .35" ctrllimited="true"/>
          </actuator></mujoco>'''

    def make_env(self, reward_weights: dict[str, float] | None = None, randomize: bool = False,
                 render_mode: str | None = None) -> RobotEnv:
        """Create a bounded training or interactive world, honoring the flight switch."""
        if os.getenv("EXCALIBUR_ROBOTLAB_FLIGHT", "on").strip().lower() in ("0", "off", "false", "no"):
            raise RuntimeError("flight scenarios disabled by EXCALIBUR_ROBOTLAB_FLIGHT")
        return RocketEnv(self, self.params, reward_weights, randomize=randomize)

    def default_camera(self) -> dict[str, list[float]]:
        """Return an overview in world z-up coordinates."""
        return {"pos": [9, -12, 7], "target": [0, 0, 2.5]}


class RocketEnv(RobotEnv):
    """Landing episodes stop on the first physical contact with the ground."""

    def __init__(self, *args: Any, **kwargs: Any) -> None:
        self.fuel_remaining = float(args[1]["fuel"])
        self.outcome = "flying"
        self.fuel_used = 0.0
        super().__init__(*args, frame_skip=4, max_episode_steps=750, **kwargs)
        self.body = self.model.body("rocket").id
        self.ground = {self.model.geom("ground").id, self.model.geom("pad").id}

    def _observation(self) -> np.ndarray:
        """Expose relative position, attitude, velocities and scenario-specific state."""
        return np.r_[self.data.qpos[:3] - [0, 0, .905], self.data.qpos[3:7], self.data.qvel[:6],
                     self.data.qpos[7:9], self.data.qvel[6:8], self.fuel_remaining / max(self.params["fuel"], 1e-9)].astype(np.float32)

    def _reset_state(self) -> None:
        """Reset targets and disturbances using only the environment random generator."""
        self.data.qpos[:3] = [0, 0, self.params["start_height"]]
        span = .5 if self.randomize else .12
        self.data.qpos[:2] = self.np_random.uniform(-span, span, 2)
        self.data.qvel[:2] = self.np_random.uniform(-span, span, 2)
        axis = self.np_random.normal(size=3)
        axis[2] = 0
        axis /= max(np.linalg.norm(axis), 1e-9)
        angle = self.np_random.uniform(-.12 if self.randomize else -.025, .12 if self.randomize else .025)
        self.data.qpos[3:7] = np.r_[np.cos(angle/2), axis*np.sin(angle/2)]
        self.fuel_remaining = float(self.params["fuel"])
        self.fuel_used = 0.0
        self.outcome = "flying"
        self.wind_phase = self.np_random.uniform(0, 2*np.pi)

    def step(self, action: np.ndarray) -> tuple[np.ndarray, float, bool, bool, dict[str, Any]]:
        """Advance bounded physics and update goals, fuel or terminal outcomes."""
        action = np.clip(np.asarray(action, dtype=float), -1, 1)
        controls = self.scale_action(action)
        self.fuel_used = 0.0
        for _ in range(self.frame_skip):
            dt = self.model.opt.timestep
            demand = controls[0] + (abs(controls[3])+abs(controls[4])) / .35
            fraction = min(1., self.fuel_remaining / max(demand*dt, 1e-12))
            self.data.ctrl[:] = controls
            self.data.ctrl[[0, 3, 4]] *= fraction
            spent = min(self.fuel_remaining, demand*dt)
            self.fuel_remaining = max(0., self.fuel_remaining-spent)
            self.fuel_used += spent
            self.data.xfrc_applied[self.body, :2] = self.params["wind"] * np.array([
                np.sin(self.data.time+self.wind_phase), .3*np.cos(self.data.time)])
            approach_velocity = self.data.qvel[:6].copy()
            mujoco.mj_step(self.model, self.data)
            contact = any(int(c.geom1) in self.ground or int(c.geom2) in self.ground
                          for c in self.data.contact[:self.data.ncon])
            if contact:
                self.outcome = self.classify_touchdown(approach_velocity)
                break
            if np.max(np.abs(self.data.qpos[:2])) > 15 or self.data.qpos[2] > 30:
                self.outcome = "out_of_bounds"
                break
            if self.fuel_remaining <= 1e-9 and self.data.qpos[2] > 1.5:
                self.outcome = "out_of_fuel"
                break
        self.steps += 1
        terms = self._reward_terms(action)
        return (self._observation(), float(sum(self.weights[k]*v for k, v in terms.items())),
                self._terminated(), self.steps >= self.max_episode_steps,
                {"terms": terms, "outcome": self.outcome, "fuel_remaining": self.fuel_remaining})

    def classify_touchdown(self, velocity: np.ndarray | None = None) -> str:
        """Use pre-impact speeds so a solver impulse cannot disguise a crash."""
        v = self.data.qvel[:6] if velocity is None else velocity
        if (np.linalg.norm(self.data.qpos[:2]) <= .65 and self.data.xmat[self.body, 8] >= .97
                and abs(v[2]) <= .8 and np.linalg.norm(v[:2]) <= .5 and np.linalg.norm(v[3:6]) <= .5):
            return "landed"
        return "crash"

    def _reward_terms(self, action: np.ndarray) -> dict[str, float]:
        """Return unweighted terms so experiments can inspect or replace weights."""
        return {"distance_to_pad": -float(np.linalg.norm(self.data.qpos[:3]-[0, 0, .905])),
                "speed": -float(np.linalg.norm(self.data.qvel[:3]) + .2*np.linalg.norm(self.data.qvel[3:6])),
                "tilt": -float(1-self.data.xmat[self.body, 8]),
                "fuel": -self.fuel_used / max(self.dt*self.model.actuator_ctrlrange[0, 1], 1e-9),
                "landed": float(self.outcome == "landed"),
                "crash": -float(self.outcome not in ("flying", "landed"))}

    def _terminated(self) -> bool:
        """Report a physical failure or a completed landing."""
        return self.outcome != "flying"
