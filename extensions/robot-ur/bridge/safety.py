"""Fail-closed limits. All positions are base-frame meters and axis-angle radians."""
import json
import math
from pathlib import Path


class Rejected(ValueError):
    pass


def number(value, label):
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value):
        raise Rejected(f"{label} must be a finite number")
    return float(value)


def vector(value, size, label):
    if not isinstance(value, list) or len(value) != size:
        raise Rejected(f"{label} must contain {size} numbers")
    return [number(v, label) for v in value]


class Safety:
    def __init__(self, values=None):
        values = {} if values is None else values
        if not isinstance(values, dict):
            raise Rejected("robot config must be an object")
        self.speed = self.limit(values, "speed_max", .25, .5)
        self.joint_speed = self.limit(values, "joint_speed_max", .6, .6)
        self.accel = self.limit(values, "accel_max", .5, .5)
        self.step_m = self.limit(values, "max_step_m", .15, .15)
        self.step_rad = self.limit(values, "max_step_rad", .5, .5)
        box = values.get("workspace", {"min": [-.6, -.6, .1], "max": [.6, .6, 1.]})
        if not isinstance(box, dict):
            raise Rejected("workspace must be an object")
        self.low = vector(box.get("min"), 3, "workspace.min")
        self.high = vector(box.get("max"), 3, "workspace.max")
        if any(a >= b for a, b in zip(self.low, self.high)):
            raise Rejected("workspace.min must be below workspace.max")
        self.approval = values.get("require_approval", True)
        self.gripper_do = values.get("gripper_do", 1)
        self.close_value = values.get("gripper_close_value", True)
        if type(self.approval) is not bool or type(self.close_value) is not bool:
            raise Rejected("approval and gripper polarity must be booleans")
        if type(self.gripper_do) is not int or not 0 <= self.gripper_do <= 7:
            raise Rejected("gripper_do must be a standard output from 0 to 7")

    @classmethod
    def load(cls, path):
        path = Path(path)
        return cls(json.loads(path.read_text()) if path.exists() else {})

    @staticmethod
    def limit(values, key, default, ceiling):
        value = number(values.get(key, default), key)
        if value <= 0 or value > ceiling:
            raise Rejected(f"{key} must be positive and at most {ceiling}")
        return value

    def approved(self, body):
        if self.approval and body.get("approved") is not True:
            raise Rejected("explicit motion approval required (approved:true)")

    def modes(self, state):
        if state["safety_mode"] != "NORMAL":
            raise Rejected("motion refused: safety_mode is " + state["safety_mode"])
        if state["robot_mode"] != "RUNNING":
            raise Rejected("motion refused: robot_mode is " + state["robot_mode"])

    def pose(self, pose):
        pose = vector(pose, 6, "pose")
        if any(v < a or v > b for v, a, b in zip(pose[:3], self.low, self.high)):
            raise Rejected("target outside base-frame workspace box")
        return pose

    def delta(self, delta):
        if not isinstance(delta, dict) or set(delta) - {"dx", "dy", "dz", "drx", "dry", "drz"}:
            raise Rejected("invalid relative delta")
        result = [number(delta.get(k, 0), k) for k in ("dx", "dy", "dz", "drx", "dry", "drz")]
        if math.hypot(*result[:3]) > self.step_m + 1e-12:
            raise Rejected("relative translation exceeds max_step_m")
        if math.hypot(*result[3:]) > self.step_rad + 1e-12:
            raise Rejected("relative rotation exceeds max_step_rad")
        return result

    def rates(self, body, joint=False):
        maximum = self.joint_speed if joint else self.speed
        speed = number(body.get("speed", maximum), "speed")
        accel = number(body.get("accel", self.accel), "accel")
        if not 0 < speed <= maximum or not 0 < accel <= self.accel:
            raise Rejected("speed or acceleration exceeds configured cap")
        return speed, accel
