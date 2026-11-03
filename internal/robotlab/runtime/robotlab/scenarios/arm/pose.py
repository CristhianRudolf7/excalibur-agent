"""Stateful webcam retargeting without retaining video or process-global state."""
from __future__ import annotations

import inspect

import numpy as np

from .model import HOME, JOINTS, LIMITS


def _angles(pose: dict, arm: str) -> np.ndarray | None:
    """Extract anatomical angles from world points or normalised image points."""
    indices = (11, 13, 15) if arm == "left" else (12, 14, 16)
    landmarks = np.asarray(pose.get("world", pose.get("landmarks", [])), dtype=float)
    visible = np.asarray(pose.get("landmarks", []), dtype=float)
    if landmarks.ndim != 2 or landmarks.shape[0] < 33 or landmarks.shape[1] < 3:
        return None
    if visible.ndim == 2 and visible.shape[0] >= 33 and visible.shape[1] >= 4:
        if not np.all(np.isfinite(visible[list(indices), 3])) or np.any(visible[list(indices), 3] < .5):
            return None
    shoulder, elbow, wrist = landmarks[list(indices), :3]
    upper, lower = elbow - shoulder, wrist - elbow
    lengths = np.linalg.norm(upper), np.linalg.norm(lower)
    if min(lengths) < 1e-6 or not np.all(np.isfinite(np.r_[upper, lower])):
        return None
    # MediaPipe y points down; yaw uses horizontal depth, pitch uses elevation.
    yaw = np.arctan2(upper[2], upper[0] if arm == "right" else -upper[0])
    pitch = np.arctan2(-upper[1], np.hypot(upper[0], upper[2]))
    flex = np.arccos(np.clip(np.dot(upper, lower) / np.prod(lengths), -1, 1))
    wrist_pitch = np.arctan2(-lower[1], np.hypot(lower[0], lower[2])) - pitch
    wrist_yaw = np.arctan2(lower[2], np.hypot(lower[0], lower[1]))
    return np.array([yaw, pitch, flex, wrist_pitch, wrist_yaw, 0.])


def map_pose(pose: dict, state: dict) -> dict[str, float]:
    """Return bounded normalised position targets and update caller-owned smoothing state.

    Optional state configuration: gains[6], offsets[6] in radians, smoothing (0..1),
    dead_zone (radians). Invalid or occluded landmarks hold the previous target.
    """
    previous = np.asarray(state.get("targets", np.r_[HOME, .045]), dtype=float)
    if previous.shape != (7,) or not np.all(np.isfinite(previous)):
        previous = np.r_[HOME, .045]
    targets = previous.copy()
    arm = pose.get("arm", "right")
    try:
        raw = _angles(pose, arm)
        if raw is not None:
            for hand in pose.get("hands", []):
                if str(hand.get("handedness", "")).lower() != arm:
                    continue
                points = np.asarray(hand["landmarks"], dtype=float)
                if points.ndim != 2 or points.shape[0] < 21 or points.shape[1] < 3 or not np.all(np.isfinite(points)):
                    continue
                palm = np.linalg.norm(points[9, :3] - points[0, :3])
                if palm < 1e-6:
                    continue
                pinch = np.linalg.norm(points[4, :3] - points[8, :3]) / palm
                targets[6] = .045 * np.clip((pinch - .15) / .65, 0, 1)
                across = points[17, :3] - points[5, :3]
                raw[5] = np.arctan2(across[1], across[0])
                break
            calibration = pose.get("calibration")
            neutral = _angles(calibration, arm) if calibration else None
            if neutral is not None:
                raw -= neutral
            gains = np.asarray(state.get("gains", [1., -.8, 1., 1., 1., 1.]), dtype=float)
            offsets = np.asarray(state.get("offsets", HOME), dtype=float)
            targets[:6] = offsets + gains * raw
    except (ValueError, TypeError, IndexError, KeyError):
        targets = previous.copy()
    if not np.all(np.isfinite(targets)):
        targets = previous.copy()
    lo = np.r_[LIMITS[:, 0], 0.]
    hi = np.r_[LIMITS[:, 1], .045]
    previous = np.clip(previous, lo, hi)
    targets = np.clip(targets, lo, hi)
    dead_zone = float(np.clip(state.get("dead_zone", .025), 0, .5))
    threshold = np.r_[np.full(6, dead_zone), .001]
    targets = np.where(np.abs(targets - previous) < threshold, previous, targets)
    alpha = float(np.clip(state.get("smoothing", .22), 0, 1))
    if not np.isfinite(alpha) or not np.isfinite(dead_zone):
        alpha = 0.
    targets = np.clip(previous + alpha * (targets - previous), lo, hi)
    state["targets"] = targets.tolist()
    normalised = 2 * (targets - lo) / (hi - lo) - 1
    return dict(zip((*JOINTS, "gripper"), map(float, normalised)))


# Export the same implementation with literal constants and no package imports.
DEFAULT_MAPPING_SOURCE = (
    "import numpy as np\n\n"
    f"HOME = np.array({HOME.tolist()!r})\n"
    f"JOINTS = {JOINTS!r}\n"
    f"LIMITS = np.array({LIMITS.tolist()!r})\n\n"
    + inspect.getsource(_angles) + "\n\n" + inspect.getsource(map_pose)
)
