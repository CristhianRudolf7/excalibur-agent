"""Robot plugin interface shared by the runtime core, the world builder and
every robot controller. This file is the contract: change it only through the
director.

Conventions
- Units: meters, radians in code (tool params may take degrees where stated),
  seconds of *simulation* time. World frame: z up, floor at z=0.
- Every name a robot adds to the world spec starts with ``robot/``.
- Robot cameras are named ``robot/<camera>`` (for example ``robot/head``).
- Tools are deterministic classical controllers. No learning, no randomness
  unless seeded from ``ctx.rng``.
"""
from __future__ import annotations

import abc
import dataclasses
from typing import Any, Callable

import mujoco
import numpy as np


@dataclasses.dataclass(frozen=True)
class Spawn:
    """Robot start pose on the floor (or table base for the arm)."""
    x: float = 0.0
    y: float = 0.0
    yaw: float = 0.0  # radians


@dataclasses.dataclass
class ToolSpec:
    """One predefined robot tool.

    ``params`` is a JSON schema object for the keyword arguments.
    ``fn(ctx, **args)`` runs the tool to completion by calling ``ctx.step``
    and returns a JSON-serializable dict (short, factual, for the agent).
    """
    name: str
    description: str
    params: dict
    fn: Callable[..., dict]


class ActionInterrupted(Exception):
    """Raised by ``ToolContext.step`` when the action must stop now.

    reason: 'fell' | 'safety' | 'stopped' | 'goal' | 'timeout'
    """
    def __init__(self, reason: str, detail: str = ''):
        super().__init__(f'{reason}: {detail}' if detail else reason)
        self.reason = reason
        self.detail = detail


class ToolContext(abc.ABC):
    """What a tool may use while it runs. The core implements it.

    ``step(seconds)`` advances physics in fixed model timesteps. Before every
    ``mj_step`` it calls ``robot.control(model, data)``; afterwards it applies
    held-object constraints, checks safety (``robot.fallen``), user stop and
    the session goal, records frames and paces wall-clock time. It raises
    ``ActionInterrupted`` when the action must stop.
    """
    model: mujoco.MjModel
    data: mujoco.MjData
    rng: np.random.Generator

    @abc.abstractmethod
    def step(self, seconds: float) -> None: ...

    def run_until(self, done: Callable[[], bool], timeout: float, dt: float = 0.02) -> bool:
        """Step until ``done()`` is true or ``timeout`` sim seconds pass."""
        t = 0.0
        while t < timeout:
            if done():
                return True
            self.step(dt)
            t += dt
        return done()

    @abc.abstractmethod
    def object_pose(self, object_id: str) -> tuple[np.ndarray, np.ndarray]:
        """World position (3,) and quaternion wxyz (4,) of an environment object."""

    @abc.abstractmethod
    def objects(self) -> list[dict]:
        """Environment objects: [{id, name, shape, dynamic, pos:[x,y,z], size}]."""

    @abc.abstractmethod
    def hold(self, object_id: str, site: str) -> None:
        """Grasp assist: keep ``object_id`` rigidly attached to ``site`` (a
        robot site name) from now on, with the current relative offset."""

    @abc.abstractmethod
    def release(self) -> str | None:
        """Drop the held object (it becomes dynamic again). Returns its id."""

    @abc.abstractmethod
    def held(self) -> str | None:
        """Id of the held object, if any."""

    def log(self, text: str) -> None:
        """Optional progress note for the activity log."""


class Robot(abc.ABC):
    """A simulated robot. One instance per world build."""
    id: str            # 'arm' | 'dog' | 'drone' | 'humanoid'
    name: dict         # {'en': 'Arm', 'es': 'Brazo'} - generic names only
    world_kind: str    # 'table' (arm) | 'room' (others)
    default_camera: str  # e.g. 'robot/head'

    @abc.abstractmethod
    def attach(self, spec: mujoco.MjSpec, spawn: Spawn, menagerie: str) -> None:
        """Add the robot (Menagerie models from ``menagerie`` dir) to ``spec``
        at ``spawn``. Add cameras ``robot/<name>`` and any sites needed
        (for example a grasp site). May set ``spec.option`` fields the
        controllers rely on (timestep, integrator, cone, impratio)."""

    @abc.abstractmethod
    def bind(self, model: mujoco.MjModel, data: mujoco.MjData) -> None:
        """Resolve joint/actuator/site ids after compilation."""

    @abc.abstractmethod
    def reset(self, model: mujoco.MjModel, data: mujoco.MjData) -> None:
        """Put the robot in its start configuration (standing, gripper open,
        landed) and set controller targets to hold it. Called after
        ``mj_resetData``; the core calls ``mj_forward`` afterwards."""

    @abc.abstractmethod
    def control(self, model: mujoco.MjModel, data: mujoco.MjData) -> None:
        """Low-level controllers: write ``data.ctrl`` (and optionally
        ``data.qfrc_applied``) toward the current targets. Called before
        every ``mj_step``; must be fast and deterministic."""

    @abc.abstractmethod
    def tools(self) -> dict[str, ToolSpec]:
        """Predefined tools (excluding ``look``, which the core provides)."""

    @abc.abstractmethod
    def card(self) -> dict:
        """Body card the agent reads: {id, name, description, sensors:[str],
        cameras:[str], tools:[{name, description, params}], limits:{...},
        notes:[str]}. Must list every tool in ``tools()`` plus ``look``."""

    @abc.abstractmethod
    def fallen(self, model: mujoco.MjModel, data: mujoco.MjData) -> str | None:
        """Unrecoverable-state reason ('fell', 'crashed', ...) or None."""

    @abc.abstractmethod
    def state(self, model: mujoco.MjModel, data: mujoco.MjData) -> dict:
        """Short factual state: {x, y, z, yaw_deg, ...robot specific}."""

    def base_xy(self, model: mujoco.MjModel, data: mujoco.MjData) -> tuple[float, float]:
        """Robot base position on the floor plane (used by goals)."""
        s = self.state(model, data)
        return float(s['x']), float(s['y'])

    def safety(self, model: mujoco.MjModel, data: mujoco.MjData) -> str | None:
        """Optional extra runtime safety check (speed, zones). Reason or None."""
        return None


def json_ok(**fields: Any) -> dict:
    """Tool result helper: {'ok': True, ...}."""
    return {'ok': True, **fields}


def json_fail(message: str, **fields: Any) -> dict:
    """Tool result helper for a tool that ran but did not achieve its aim."""
    return {'ok': False, 'error': message, **fields}
