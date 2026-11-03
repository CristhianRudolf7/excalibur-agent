"""Primitive MuJoCo scenes and world poses for the web renderer."""
from __future__ import annotations

import mujoco
import numpy as np
from .scenario import RobotEnv

GEOMS = {int(getattr(mujoco.mjtGeom, 'mjGEOM_' + name.upper())): name
         for name in ('plane', 'box', 'sphere', 'capsule', 'cylinder', 'ellipsoid')}


def export_scene(env: RobotEnv) -> dict:
    """Export model-local geometry and static camera configuration."""
    m, s = env.model, env.scenario
    geoms = []
    for i in range(m.ngeom):
        kind = GEOMS.get(int(m.geom_type[i]))
        if kind is None:
            raise ValueError('scene contains unsupported nonprimitive geom')
        size = m.geom_size[i].copy()
        if kind == 'plane':
            size[:2] = np.maximum(size[:2], 5.0)
        geom = {'body': int(m.geom_bodyid[i]), 'type': kind, 'size': size.tolist(),
                'pos': m.geom_pos[i].tolist(), 'quat': m.geom_quat[i].tolist(),
                'rgba': m.geom_rgba[i].tolist()}
        name = mujoco.mj_id2name(m, mujoco.mjtObj.mjOBJ_GEOM, i)
        if name:
            geom['name'] = name
        geoms.append(geom)
    return {'scenario': s.id, 'params': dict(s.params), 'timestep': env.dt,
            'bodies': [{'name': mujoco.mj_id2name(m, mujoco.mjtObj.mjOBJ_BODY, i) or f'body_{i}',
                        'parent': int(m.body_parentid[i])} for i in range(m.nbody)],
            'geoms': geoms,
            'cameras': [{'name': mujoco.mj_id2name(m, mujoco.mjtObj.mjOBJ_CAMERA, i) or f'camera_{i}',
                         'pos': env.data.cam_xpos[i].tolist(),
                         'quat': matrix_quat(env.data.cam_xmat[i]), 'fovy': float(m.cam_fovy[i])}
                        for i in range(m.ncam)],
            'actuators': [a.info() for a in s.actuators_for(s.params)],
            'camera_default': s.default_camera()}


def matrix_quat(matrix: np.ndarray) -> list:
    """Convert a camera rotation to a wxyz quaternion."""
    quat = np.zeros(4)
    mujoco.mju_mat2Quat(quat, matrix)
    return quat.tolist()


def body_poses(env: RobotEnv) -> list:
    """Return world poses in model body order, including the world body."""
    return np.concatenate((env.data.xpos, env.data.xquat), axis=1).tolist()


def frame(env: RobotEnv, session: str, step: int, episode: int, reward: float,
          episode_return: float, terminated: bool, info: dict, mode: str, **extra) -> dict:
    """Build a protocol frame, retaining only scalar info values."""
    scalar = {k: v.item() if isinstance(v, np.generic) else v for k, v in info.items()
              if isinstance(v, (str, int, float, bool, np.number)) and
              (not isinstance(v, (float, np.floating)) or np.isfinite(v))}
    return {'session': session, 't': float(env.data.time), 'step': step, 'episode': episode,
            'bodies': body_poses(env), 'reward': float(reward), 'episode_return': float(episode_return),
            'terminated': bool(terminated), 'info': scalar, 'mode': mode, **extra}
