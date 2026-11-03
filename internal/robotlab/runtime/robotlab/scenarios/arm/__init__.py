"""Robot Lab's six-axis manipulation workcell."""
from __future__ import annotations

import os
from typing import Any

import numpy as np

from robotlab.registry import register
from robotlab.scenario import Actuator, RewardTerm, Scenario
from .env import ArmEnv
from .model import BINS, HOME, JOINTS, LIMITS, build_xml, validate
from .pose import DEFAULT_MAPPING_SOURCE, map_pose


def text(value: str, spanish: str) -> dict[str, str]:
    """Localised user-facing metadata."""
    return {"en": value, "es": spanish}


@register
class ArmScenario(Scenario):
    """Reach, grasp and inspect small manufactured parts using shared arm controls."""

    id = "arm"
    title = text("Robot arm", "Brazo robótico")
    description = text("A six-axis arm for reaching, pick and place, visual sorting and webcam control.",
                       "Un brazo de seis ejes para alcanzar objetivos, recoger y colocar piezas, clasificarlas visualmente y controlarlo con la cámara web.")
    category = "manipulation"
    thumbnail_color = "#568AC4"
    default_params = {"task": "reach", "objects": 5, "seed": 0, "randomize": False}
    params_schema = {"type": "object", "additionalProperties": False, "properties": {
        "task": {"type": "string", "enum": ["reach", "pick_place", "sort"]},
        "objects": {"type": "integer", "minimum": 3, "maximum": 8},
        "seed": {"type": "integer", "minimum": 0}, "randomize": {"type": "boolean"}}}
    actuators = [Actuator(name, text(name.replace("_", " ").title(),
                          ("Giro del hombro", "Elevación del hombro", "Codo", "Muñeca 1", "Muñeca 2", "Muñeca 3")[i]), "position", LIMITS[i],
                          *( ("KeyA", "KeyD") if i == 0 else ("KeyS", "KeyW") if i == 1 else
                             ("KeyF", "KeyR") if i == 2 else (None, None)))
                 for i, name in enumerate(JOINTS)] + [Actuator("gripper", text("Gripper opening", "Apertura de la pinza"), "position", [0, .045])]
    reward_terms = [RewardTerm(name, weight, text(description, spanish)) for name, weight, description, spanish in (
        ("distance", 4., "Negative tool to target distance in metres", "Distancia negativa de la herramienta al objetivo, en metros"),
        ("smooth", .02, "Negative squared change in action", "Cambio de acción al cuadrado, con signo negativo"),
        ("effort", .005, "Negative squared control magnitude", "Magnitud del control al cuadrado, con signo negativo"),
        ("success", 10., "Target reached or part released into the bin", "Objetivo alcanzado o pieza soltada en el contenedor"),
        ("reach", 2., "Negative tool to part distance", "Distancia negativa de la herramienta a la pieza"),
        ("grasp", 1., "Both fingertips contact the part", "Ambas puntas de la pinza están en contacto con la pieza"),
        ("lift", 2., "Part lifted above the tabletop", "Pieza elevada sobre la mesa"),
        ("place", 2., "Negative part to bin distance", "Distancia negativa de la pieza al contenedor"))]
    algorithms = ["PPO", "SAC", "TD3"]
    default_total_steps = 300_000
    inputs = ["sliders", "keyboard", "gamepad", "pose"]
    has_vision = True
    session_auto_reset = False

    def __init__(self, params=None):
        super().__init__(params)
        self.params = validate(self.params)

    def build_xml(self, params: dict) -> str:
        return build_xml(params)

    def make_env(self, reward_weights=None, randomize=False, render_mode=None) -> ArmEnv:
        if os.getenv("EXCALIBUR_ROBOTLAB_ARM", "on").strip().lower() in ("0", "off", "false", "no"):
            raise RuntimeError("arm scenario disabled by EXCALIBUR_ROBOTLAB_ARM")
        return ArmEnv(self, reward_weights, randomize)

    def idle_action(self, env: ArmEnv) -> np.ndarray:
        return env.normalized(env.data.ctrl)

    def manual_action(self, env: ArmEnv, inputs: dict[str, float]) -> np.ndarray:
        action = self.idle_action(env)
        for i, actuator in enumerate(self.actuators):
            if actuator.name in inputs:
                value = float(inputs[actuator.name])
                if not np.isfinite(value):
                    raise ValueError("manual joint targets must be finite")
                action[i] = np.clip(value, -1, 1)
            else:
                delta = float(inputs.get(actuator.key_pos, 0)) - float(inputs.get(actuator.key_neg, 0))
                if not np.isfinite(delta):
                    raise ValueError("keyboard input must be finite")
                action[i] = np.clip(action[i] + .015 * delta, -1, 1)
        return action

    def pose_mapping(self):
        return map_pose

    def pose_mapping_source(self) -> str:
        return DEFAULT_MAPPING_SOURCE

    def marks(self, env: ArmEnv) -> list[dict[str, Any]]:
        return [{"id": i + 1, "name": part["name"],
                 "label": f'{part["kind"]}:{("red", "green", "blue", "yellow")[part["color"]]}:{"defect" if part["defect"] else "good"}',
                 "body": env.model.body(part["name"]).id,
                 "world": env.data.body(part["name"]).xpos.tolist()}
                for i, part in enumerate(env.parts)]

    def default_camera(self):
        return {"pos": [1.5, -1.7, 1.8], "target": [0, 0, .9]}

    def _drive(self, env: ArmEnv, joints: np.ndarray, opening: float | None = None, steps: int = 70):
        """Smoothstep interpolation in actuator space; never teleport the live arm."""
        start = env.data.ctrl.copy()
        end = np.r_[joints, start[6] if opening is None else opening]
        for i in range(steps):
            t = (i + 1) / steps
            target = start + (end - start) * (t * t * (3 - 2 * t))
            env.step(env.normalized(target))
            yield
        for _ in range(15):
            env.step(env.normalized(end))
            yield

    def _move(self, env: ArmEnv, xyz, yaw=0.0, steps=70):
        joints = env.ik(xyz, yaw)
        yield from self._drive(env, joints, steps=steps)
        residual = float(np.linalg.norm(env.tcp - xyz))
        if residual > .01:
            raise RuntimeError(f"move_to failed: tracking residual {residual:.4f} m")

    def skill(self, env: ArmEnv, name: str, args: dict):
        """Yield each 20 ms control step; errors carry the failed stage and residual."""
        if name == "home":
            yield from self._drive(env, HOME)
        elif name in ("open_gripper", "close_gripper"):
            yield from self._drive(env, env.data.ctrl[:6], .045 if name == "open_gripper" else 0., steps=25)
        elif name == "move_to":
            yield from self._move(env, args.get("xyz"), float(args.get("yaw", 0)))
        elif name == "pick_place":
            value = args.get("object")
            part = next((p for i, p in enumerate(env.parts) if value == p["name"] or value == i + 1 or value == str(i + 1)), None)
            target = args.get("target")
            if part is None:
                raise ValueError(f"unknown loose object {value!r}")
            if target not in BINS:
                raise ValueError(f"unknown bin {target!r}; choose A, B or reject")
            part_name = part["name"]
            pos = env.data.body(part_name).xpos.copy()
            if pos[2] < .74 or np.any(np.abs(pos[:2]) > .7):
                raise RuntimeError(f"pick_place failed: {part_name} is outside the work surface")
            yield from self.skill(env, "open_gripper", {})
            yield from self._move(env, pos + [0, 0, .18])
            # Fingers extend 10 mm below the TCP; keep them clear of the table.
            grasp = pos.copy()
            grasp[2] = max(.762, pos[2])
            yield from self._move(env, grasp, steps=55)
            yield from self.skill(env, "close_gripper", {})
            if not env.grasped(part_name):
                raise RuntimeError(f"pick_place failed at grasp: no bilateral finger contact on {part_name}")
            yield from self._move(env, grasp + [0, 0, .20], steps=65)
            if env.data.body(part_name).xpos[2] < .86:
                raise RuntimeError(f"pick_place failed at lift: {part_name} slipped")
            destination = np.array(BINS[target]) + [0, 0, .23]
            yield from self._move(env, destination, steps=100)
            yield from self._move(env, destination - [0, 0, .10], steps=45)
            yield from self.skill(env, "open_gripper", {})
            yield from self._move(env, destination)
            yield from self.skill(env, "home", {})
            if not env.in_bin(part_name, target):
                raise RuntimeError(f"pick_place failed at verification: {part_name} did not settle in bin {target}")
            return {"in_target": True, "bin": target,
                    "detail": f"Placed {part_name} in bin {target} and returned home"}
        else:
            raise ValueError(f"unknown arm skill {name!r}")
        return f"Completed {name}"
