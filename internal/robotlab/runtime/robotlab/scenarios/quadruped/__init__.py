"""A primitive-geometry quadruped with learned joint-position control."""

from __future__ import annotations

import math
import os
from pathlib import Path
from typing import Any

import mujoco
import numpy as np

from robotlab.registry import register
from robotlab.scenario import Actuator, RewardTerm, RobotEnv, Scenario


def text(en: str, es: str) -> dict[str, str]:
    """User-facing metadata in English and Spanish."""
    return {"en": en, "es": es}


class QuadrupedEnv(RobotEnv):
    """50 Hz position control over 500 Hz contact dynamics; observations are local."""

    def __init__(self, scenario: Scenario, reward_weights=None, randomize=False, render_mode=None):
        self.last_action = np.zeros(12, dtype=np.float64)
        self.previous_action = self.last_action.copy()
        self.render_mode = render_mode
        self._renderer = None
        self._push_until = 0.0
        self._next_push = 1.0
        super().__init__(scenario, scenario.params, reward_weights, frame_skip=10,
                         max_episode_steps=1000, randomize=randomize)
        self.torso_id = mujoco.mj_name2id(self.model, mujoco.mjtObj.mjOBJ_BODY, "torso")

    def _observation(self) -> np.ndarray:
        # qpos quaternion rotates body to world. Its third row projects gravity.
        rotation = np.empty(9)
        mujoco.mju_quat2Mat(rotation, self.data.qpos[3:7])
        rotation = rotation.reshape(3, 3)
        gravity = rotation.T @ np.array([0.0, 0.0, -1.0])
        speed = float(self.params["target_speed"])
        yaw = 0.6 if self.params["task"] == "turn" else 0.0
        return np.concatenate(([self.data.qpos[2]], gravity,
                               rotation.T @ self.data.qvel[:3], self.data.qvel[3:6],
                               self.data.qpos[7:] - self.scenario.nominal,
                               self.data.qvel[6:], self.last_action, [speed, yaw])).astype(np.float32)

    def _reset_state(self) -> None:
        self.data.qpos[:3] = [0, 0, 0.405]
        self.data.qpos[3:7] = [1, 0, 0, 0]
        self.data.qpos[7:] = self.scenario.nominal
        if self.randomize:
            self.data.qpos[7:] += self.np_random.uniform(-0.03, 0.03, 12)
            self.data.qvel[:] = self.np_random.normal(0, 0.015, self.model.nv)
        self.last_action[:] = 0
        self.previous_action[:] = 0
        self.data.ctrl[:] = self.scenario.nominal
        self._next_push = float(self.np_random.uniform(1.0, 2.0))
        self._push_until = 0.0

    def scale_action(self, action: np.ndarray) -> np.ndarray:
        return self.scenario.nominal + self.scenario.action_scale * np.clip(action, -1, 1)

    def step(self, action):
        action = np.asarray(action, dtype=np.float64)
        if action.shape != (12,) or not np.isfinite(action).all():
            raise ValueError("quadruped action must contain 12 finite values")
        self.previous_action[:] = self.last_action
        self.last_action[:] = np.clip(action, -1, 1)
        self.data.xfrc_applied[:] = 0
        if self.params["task"] == "push_recovery":
            if self.data.time >= self._next_push:
                angle = float(self.np_random.uniform(0, 2 * math.pi))
                force = float(self.params["push_force"])
                self._push = force * np.array([math.cos(angle), math.sin(angle)])
                self._push_until = self.data.time + 0.12
                self._next_push = self.data.time + float(self.np_random.uniform(1.5, 3.0))
            if self.data.time < self._push_until:
                self.data.xfrc_applied[self.torso_id, :2] = self._push
        obs, reward, terminated, truncated, info = super().step(self.last_action)
        info.update(speed=float(self.data.qvel[0]), distance=float(self.data.qpos[0]),
                    height=float(self.data.qpos[2]), yaw_rate=float(self.data.qvel[5]))
        return obs, reward, terminated, truncated, info

    def _reward_terms(self, action: np.ndarray) -> dict[str, float]:
        obs = self._observation()
        return {
            "forward": math.exp(-((float(self.data.qvel[0]) - self.params["target_speed"]) / 0.35) ** 2)
                       - 0.3 * float(self.data.qvel[1] ** 2),
            "upright": float(-obs[3]) - 0.1 * float(np.square(self.data.qvel[3:5]).sum()),
            "height": math.exp(-((float(self.data.qpos[2]) - 0.39) / 0.07) ** 2),
            "energy": -float(np.mean(np.square(self.data.actuator_force * self.data.qvel[6:]))) / 1000,
            "smooth": -float(np.mean(np.square(action - self.previous_action))),
            "alive": 0.0 if self._terminated() else 1.0,
            "turn": (math.exp(-((float(self.data.qvel[5]) - 0.6) / 0.3) ** 2)
                     if self.params["task"] == "turn" else -float(self.data.qvel[5] ** 2)),
        }

    def _terminated(self) -> bool:
        return bool(self.data.qpos[2] < 0.22 or self._observation()[3] > -0.45
                    or not np.isfinite(self.data.qpos).all())

    def render(self):
        if self._renderer is None:
            self._renderer = mujoco.Renderer(self.model, height=480, width=640)
        camera = mujoco.MjvCamera()
        camera.lookat[:] = self.data.qpos[:3]
        camera.distance, camera.azimuth, camera.elevation = 2.2, 135, -22
        self._renderer.update_scene(self.data, camera=camera)
        return self._renderer.render()

    def close(self):
        if self._renderer is not None:
            self._renderer.close()
            self._renderer = None


@register
class QuadrupedScenario(Scenario):
    """Dog-like, twelve-position-actuator locomotion world."""

    id = "quadruped"
    title = text("Quadruped", "Perro robot")
    description = text("Teach a four-legged robot to walk, turn and recover from pushes.",
                       "Enseña a un robot de cuatro patas a caminar, girar y recuperarse de empujones.")
    category = "locomotion"
    thumbnail_color = "#2F6BFF"
    default_params = {"task": "walk", "target_speed": 0.5, "terrain": "flat", "push_force": 35.0}
    params_schema = {"type": "object", "additionalProperties": False, "properties": {
        "task": {"type": "string", "enum": ["walk", "turn", "push_recovery"]},
        "target_speed": {"type": "number", "minimum": 0, "maximum": 2},
        "terrain": {"type": "string", "enum": ["flat", "bumpy", "stairs"]},
        "push_force": {"type": "number", "minimum": 0, "maximum": 150}}}
    nominal = np.tile([0.0, -0.6, 1.2], 4)
    action_scale = np.tile([0.3, 0.6, 0.7], 4)
    actuators = [Actuator(f"{leg}_{joint}", text(f"{label} {joint}", f"{joint_es} {label_es}"), "position", [-1, 1])
                 for leg, label, label_es in [("fl", "Front left", "delantera izquierda"),
                                              ("fr", "Front right", "delantera derecha"),
                                              ("rl", "Rear left", "trasera izquierda"),
                                              ("rr", "Rear right", "trasera derecha")]
                 for joint, joint_es in [("abduction", "Abducción"), ("hip", "Cadera"), ("knee", "Rodilla")]]
    reward_terms = [RewardTerm(name, weight, text(en, es)) for name, weight, en, es in [
        ("forward", 3.0, "Track commanded forward speed; discourage sideways drift.", "Seguir la velocidad de avance pedida y evitar desviarse de lado."),
        ("upright", 1.0, "Keep the torso level and angular motion controlled.", "Mantener el torso nivelado y sin giros bruscos."),
        ("height", 1.0, "Keep a standing torso height.", "Mantener la altura del torso de pie."),
        ("energy", 0.05, "Penalize squared mechanical joint power.", "Penalizar la potencia mecánica de las articulaciones."),
        ("smooth", 0.15, "Penalize changes in joint targets.", "Penalizar los cambios bruscos de las articulaciones."),
        ("alive", 0.3, "Remain standing.", "Seguir de pie."),
        ("turn", 0.5, "Track turn yaw rate; otherwise discourage yaw drift.", "Seguir la velocidad de giro pedida; si no, evitar girar.")]]
    default_total_steps = 2_000_000
    has_pretrained = True

    def __init__(self, params: dict[str, Any] | None = None):
        super().__init__(params)
        self._validate(self.params)

    @classmethod
    def _validate(cls, params):
        if set(params) - set(cls.default_params):
            raise ValueError("unknown quadruped parameter")
        for name in ("task", "terrain"):
            if params[name] not in cls.params_schema["properties"][name]["enum"]:
                raise ValueError(f"invalid quadruped {name}")
        for name, upper in [("target_speed", 2), ("push_force", 150)]:
            value = params[name]
            if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or not 0 <= value <= upper:
                raise ValueError(f"invalid quadruped {name}")

    def build_xml(self, params: dict[str, Any]) -> str:
        p = dict(self.default_params)
        p.update(params)
        self._validate(p)
        terrain = []
        if p["terrain"] == "bumpy":
            for i in range(12):
                for j in range(7):
                    height = 0.008 + 0.005 * ((i * 7 + j * 3) % 5)
                    terrain.append(f'<geom name="bump_{i}_{j}" type="box" pos="{0.9+i*.28} {(j-3)*.28} {height/2}" size=".10 .10 {height/2}" rgba=".22 .38 .65 1"/>')
        elif p["terrain"] == "stairs":
            for i in range(8):
                height = 0.025 * (i + 1)
                terrain.append(f'<geom name="step_{i}" type="box" pos="{1+i*.35} 0 {height/2}" size=".175 1.2 {height/2}" rgba=".25 .43 .72 1"/>')
        legs, actuators = [], []
        for name, x, y in [("fl", .23, .16), ("fr", .23, -.16), ("rl", -.23, .16), ("rr", -.23, -.16)]:
            legs.append(f'''<body name="{name}_abduction" pos="{x} {y} 0">
              <joint name="{name}_abduction" axis="1 0 0" range="-.6 .6"/>
              <geom type="sphere" size=".045" mass=".18" rgba=".12 .27 .65 1"/>
              <body name="{name}_upper"><joint name="{name}_hip" axis="0 1 0" range="-1.5 1.2"/>
                <geom type="capsule" fromto="0 0 0 0 0 -.22" size=".027" mass=".45"/>
                <body name="{name}_lower" pos="0 0 -.22"><joint name="{name}_knee" axis="0 1 0" range=".15 2.5"/>
                  <geom type="capsule" fromto="0 0 0 0 0 -.22" size=".022" mass=".25" rgba=".35 .64 1 1"/>
                  <geom name="{name}_foot" type="sphere" pos="0 0 -.22" size=".032" mass=".08" rgba=".08 .15 .30 1"/>
                </body></body></body>''')
            for joint, lo, hi in [("abduction", -.3, .3), ("hip", -1.2, 0), ("knee", .5, 1.9)]:
                actuators.append(f'<position name="{name}_{joint}" joint="{name}_{joint}" kp="65" kv="3" ctrlrange="{lo} {hi}" forcerange="-25 25"/>')
        return f'''<mujoco model="blue_quadruped"><compiler angle="radian"/>
          <option timestep=".002" integrator="implicitfast" iterations="30"/>
          <default><joint damping=".3" armature=".015"/><geom friction=".9 .02 .002" condim="3" rgba=".16 .40 .95 1"/></default>
          <worldbody><light pos="0 -2 4"/><geom name="floor" type="plane" size="20 20 .1" rgba=".80 .87 .96 1"/>
          {''.join(terrain)}<camera name="overview" pos="2 -2 1.5" xyaxes=".707 .707 0 -.3 .3 .9"/>
          <body name="torso" pos="0 0 .405"><freejoint/>
            <geom name="torso_shell" type="box" size=".28 .14 .065" mass="5"/>
            <geom name="head" type="box" pos=".32 0 .025" size=".065 .09 .06" mass=".3" rgba=".10 .25 .65 1"/>
            {''.join(legs)}</body></worldbody><actuator>{''.join(actuators)}</actuator></mujoco>'''

    def make_env(self, reward_weights=None, randomize=False, render_mode=None) -> QuadrupedEnv:
        if os.getenv("EXCALIBUR_ROBOTLAB_QUADRUPED", "").strip().lower() in {"0", "off", "false", "no"}:
            raise ValueError("quadruped disabled by EXCALIBUR_ROBOTLAB_QUADRUPED")
        return QuadrupedEnv(self, reward_weights, randomize, render_mode)

    def manual_action(self, env: RobotEnv, inputs: dict[str, float]) -> np.ndarray:
        # Keys are KeyboardEvent.code values; sliders override individual targets.
        forward = float(inputs.get("ArrowUp", 0)) - float(inputs.get("ArrowDown", 0))
        turn = float(inputs.get("ArrowLeft", 0)) - float(inputs.get("ArrowRight", 0))
        action = np.zeros(12)
        if forward or turn:
            ramp = min(1.0, env.data.time / 0.4)
            for i, phase in enumerate([0, math.pi, math.pi, 0]):
                cycle = (2 * math.pi * 1.8 * env.data.time + phase) % (2 * math.pi)
                stride = .08 * ramp * np.clip(forward + (1 if i % 2 else -1) * .5 * turn, -1, 1)
                # Swing the foot forward above the floor; return along the floor
                # during stance. Solve the two equal-length leg links directly.
                x = stride * (-math.cos(cycle) if cycle < math.pi
                              else 1 - 2 * (cycle - math.pi) / math.pi)
                z = -.44 * math.cos(.6) + .055 * ramp * max(0, math.sin(cycle))
                knee = 2 * math.acos(min(1.0, math.hypot(x, z) / .44))
                hip = math.atan2(-x, -z) - knee / 2
                action[3*i+1] = (hip - self.nominal[3*i+1]) / self.action_scale[3*i+1]
                action[3*i+2] = (knee - self.nominal[3*i+2]) / self.action_scale[3*i+2]
        for i, actuator in enumerate(self.actuators):
            if actuator.name in inputs:
                action[i] = float(inputs[actuator.name])
        return np.clip(action, -1, 1).astype(np.float32)

    def pretrained_policy(self) -> str:
        return str(Path(__file__).with_name("pretrained") / "walk.zip")

    def default_camera(self):
        return {"pos": [2, -2, 1.2], "target": [0, 0, .35]}
