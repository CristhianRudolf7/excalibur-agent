"""Physical state and mutable controller targets for isolated failure replay."""
from __future__ import annotations

import copy

import mujoco
import numpy as np


# Keep model bindings, kinematics scratch buffers and immutable gains out of the
# snapshots. These fields are the targets and controller memory changed by tools.
_CONTROLLER_FIELDS = {
    'arm': ('_target', '_holding', '_gripper', '_grip_target', '_velocity'),
    'dog': ('target', 'mode', 'trunk_target', 'foot_target', 'low_since', 'velocity'),
}


def _plain(value):
    if isinstance(value, np.ndarray):
        return value.tolist()
    if isinstance(value, np.generic):
        return value.item()
    return copy.deepcopy(value)


def capture(ctx):
    data, model = ctx.data, ctx.model
    spec = mujoco.mjtState.mjSTATE_INTEGRATION
    integration = np.empty(mujoco.mj_stateSize(model, spec))
    mujoco.mj_getState(model, data, integration, spec)
    return {'qpos': data.qpos.tolist(), 'qvel': data.qvel.tolist(),
            'ctrl': data.ctrl.tolist(), 'time': float(data.time),
            'integration': integration.tolist(), 'held': ctx.held(),
            'held_state': ctx.held_state(), 'robot_start': list(ctx.robot_start),
            'controller': {name: _plain(getattr(ctx.robot, name))
                           for name in _CONTROLLER_FIELDS.get(ctx.robot.id, ())
                           if hasattr(ctx.robot, name)}}


def restore(ctx, state):
    data, model = ctx.data, ctx.model
    if 'integration' in state:
        mujoco.mj_setState(model, data, np.asarray(state['integration']), mujoco.mjtState.mjSTATE_INTEGRATION)
    else:
        # Earlier evidence has only the original section 5.3 state fields.
        data.qpos[:] = state['qpos']
        data.qvel[:] = state['qvel']
        if 'ctrl' in state:
            data.ctrl[:] = state['ctrl']
        data.time = state['time']
    for name, value in state.get('controller', {}).items():
        if name not in _CONTROLLER_FIELDS.get(ctx.robot.id, ()) or not hasattr(ctx.robot, name):
            continue
        previous = getattr(ctx.robot, name)
        setattr(ctx.robot, name, np.array(value, dtype=previous.dtype) if isinstance(previous, np.ndarray)
                else copy.deepcopy(value))
    ctx.release()
    mujoco.mj_forward(model, data)
    ctx.restore_hold(state.get('held_state'), apply=False)
