"""The scenario contract shared by every Robot Lab world.

A scenario describes a physical world (an MJCF model built from primitive geoms),
how it is controlled (actuators), how it is rewarded (named, weighted reward terms)
and how it is exposed to people (titles, parameters, input devices). The sidecar,
the experiment SDK and the web viewer only talk to scenarios through this module.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Callable, Dict, List, Optional, Sequence

import gymnasium as gym
import mujoco
import numpy as np

Text = Dict[str, str]  # {"en": ..., "es": ...}


@dataclass(frozen=True)
class Actuator:
    name: str  # must equal the MJCF actuator name
    label: Text
    kind: str  # "torque" | "position" | "velocity"
    range: Sequence[float]
    key_neg: Optional[str] = None  # KeyboardEvent.code, e.g. "KeyA"
    key_pos: Optional[str] = None

    def info(self) -> Dict[str, Any]:
        out = {"name": self.name, "label": dict(self.label), "kind": self.kind, "range": [float(v) for v in self.range]}
        if self.key_neg:
            out["key_neg"] = self.key_neg
        if self.key_pos:
            out["key_pos"] = self.key_pos
        return out


@dataclass(frozen=True)
class RewardTerm:
    name: str
    weight: float
    description: Text

    def info(self) -> Dict[str, Any]:
        return {"name": self.name, "weight": float(self.weight), "description": dict(self.description)}


class RobotEnv(gym.Env):
    """Base gymnasium environment over one MuJoCo model.

    Subclasses implement ``_observation``, ``_reward_terms`` and ``_terminated`` and may
    override ``_reset_state``. Actions are normalised to [-1, 1] per actuator and scaled
    to the actuator ctrlrange. ``step`` returns ``info["terms"]`` with every unweighted
    reward term so experiments can reweight or log them.
    """

    metadata = {"render_modes": ["rgb_array"], "render_fps": 30}

    def __init__(self, scenario: "Scenario", params: Dict[str, Any], reward_weights: Optional[Dict[str, float]] = None,
                 frame_skip: int = 1, max_episode_steps: int = 1000, randomize: bool = False):
        super().__init__()
        self.scenario = scenario
        self.params = dict(params)
        self.model = mujoco.MjModel.from_xml_string(scenario.build_xml(self.params))
        self.data = mujoco.MjData(self.model)
        self.frame_skip = frame_skip
        self.max_episode_steps = max_episode_steps
        self.randomize = randomize
        self.weights = {t.name: t.weight for t in scenario.reward_terms}
        if reward_weights:
            unknown = set(reward_weights) - set(self.weights)
            if unknown:
                raise ValueError(f"unknown reward terms: {sorted(unknown)}")
            self.weights.update({k: float(v) for k, v in reward_weights.items()})
        n = self.model.nu
        self.action_space = gym.spaces.Box(-1.0, 1.0, shape=(n,), dtype=np.float32)
        obs = self._observation()
        self.observation_space = gym.spaces.Box(-np.inf, np.inf, shape=obs.shape, dtype=np.float32)
        self.steps = 0

    # --- hooks -----------------------------------------------------------------
    def _observation(self) -> np.ndarray:
        raise NotImplementedError

    def _reward_terms(self, action: np.ndarray) -> Dict[str, float]:
        raise NotImplementedError

    def _terminated(self) -> bool:
        return False

    def _reset_state(self) -> None:
        """Set qpos/qvel for a new episode (use self.np_random; honour self.randomize)."""

    # --- gymnasium API -----------------------------------------------------------
    def scale_action(self, action: np.ndarray) -> np.ndarray:
        a = np.clip(np.asarray(action, dtype=np.float64), -1.0, 1.0)
        lo, hi = self.model.actuator_ctrlrange[:, 0], self.model.actuator_ctrlrange[:, 1]
        return lo + (a + 1.0) * 0.5 * (hi - lo)

    def reset(self, *, seed: Optional[int] = None, options: Optional[dict] = None):
        super().reset(seed=seed)
        mujoco.mj_resetData(self.model, self.data)
        self._reset_state()
        mujoco.mj_forward(self.model, self.data)
        self.steps = 0
        return self._observation().astype(np.float32), {}

    def step(self, action):
        self.data.ctrl[:] = self.scale_action(action)
        for _ in range(self.frame_skip):
            mujoco.mj_step(self.model, self.data)
        self.steps += 1
        terms = self._reward_terms(np.asarray(action))
        reward = float(sum(self.weights.get(k, 0.0) * v for k, v in terms.items()))
        terminated = bool(self._terminated())
        truncated = self.steps >= self.max_episode_steps
        return self._observation().astype(np.float32), reward, terminated, truncated, {"terms": terms}

    @property
    def dt(self) -> float:
        return float(self.model.opt.timestep * self.frame_skip)


class Scenario:
    """Static description + factory for one world. Subclass and decorate with ``@register``."""

    id: str = ""
    title: Text = {"en": "", "es": ""}
    description: Text = {"en": "", "es": ""}
    category: str = "classic"  # classic | manipulation | locomotion | flight
    thumbnail_color: str = "#2F6BFF"
    params_schema: Dict[str, Any] = {"type": "object", "properties": {}, "additionalProperties": False}
    default_params: Dict[str, Any] = {}
    actuators: List[Actuator] = []
    reward_terms: List[RewardTerm] = []
    algorithms: List[str] = ["PPO", "SAC"]
    default_total_steps: int = 100_000
    inputs: List[str] = ["sliders", "keyboard", "gamepad"]
    has_pretrained: bool = False
    has_vision: bool = False
    session_auto_reset: bool = True  # Interactive workcells may retain completed layouts.

    def __init__(self, params: Optional[Dict[str, Any]] = None):
        merged = dict(self.default_params)
        merged.update(params or {})
        self.params = merged

    # --- required ----------------------------------------------------------------
    def build_xml(self, params: Dict[str, Any]) -> str:
        """Return the MJCF for these params. Primitive geoms only (no meshes)."""
        raise NotImplementedError

    def make_env(self, reward_weights: Optional[Dict[str, float]] = None, randomize: bool = False,
                 render_mode: Optional[str] = None) -> RobotEnv:
        raise NotImplementedError

    # --- optional ----------------------------------------------------------------
    def actuators_for(self, params: Dict[str, Any]) -> List[Actuator]:
        """Actuators for these params (override when the count depends on params)."""
        return list(self.actuators)

    def manual_action(self, env: RobotEnv, inputs: Dict[str, float]) -> np.ndarray:
        """Map UI inputs {actuator_name: [-1,1]} to a normalised action (default: direct)."""
        action = np.zeros(env.model.nu, dtype=np.float32)
        for i in range(env.model.nu):
            name = mujoco.mj_id2name(env.model, mujoco.mjtObj.mjOBJ_ACTUATOR, i)
            if name in inputs:
                action[i] = float(np.clip(inputs[name], -1.0, 1.0))
        return action

    def idle_action(self, env: RobotEnv) -> np.ndarray:
        return np.zeros(env.model.nu, dtype=np.float32)

    def pretrained_policy(self) -> Optional[str]:
        """Path to a bundled SB3 .zip policy for this scenario, if any."""
        return None

    def pose_mapping(self) -> Optional[Callable[[Dict[str, Any], Dict[str, Any]], Dict[str, float]]]:
        """Default webcam-pose mapping: (pose, state) -> {actuator_name: normalised target}."""
        return None

    def pose_mapping_source(self) -> Optional[str]:
        """Editable map_pose(pose, state) source for the default mapping, if available."""
        return None

    def marks(self, env: RobotEnv) -> List[Dict[str, Any]]:
        """Objects the vision loop may refer to: [{name, label, body or geom id}]. Vision scenarios only."""
        return []

    def skill(self, env: RobotEnv, name: str, args: Dict[str, Any]):
        """Run a scripted skill (arm). Generator yielding after each control step; returns detail str or an additive result dict."""
        raise NotImplementedError(f"{self.id} has no skill {name!r}")

    def default_camera(self) -> Dict[str, List[float]]:
        return {"pos": [2.0, -2.0, 1.5], "target": [0.0, 0.0, 0.5]}

    @classmethod
    def info(cls) -> Dict[str, Any]:
        s = cls()
        return {
            "id": cls.id, "title": dict(cls.title), "description": dict(cls.description), "category": cls.category,
            "params_schema": cls.params_schema, "default_params": dict(cls.default_params),
            "actuators": [a.info() for a in s.actuators_for(s.params)],
            "reward_terms": [t.info() for t in cls.reward_terms], "algorithms": list(cls.algorithms),
            "default_total_steps": cls.default_total_steps, "inputs": list(cls.inputs),
            "has_pretrained": cls.has_pretrained, "has_vision": cls.has_vision, "thumbnail_color": cls.thumbnail_color,
        }
