"""Conservative straight-route clearance checks for fixed scene geometry."""
from __future__ import annotations

import mujoco
import numpy as np


def fixed_obstacles(model, robot_geoms):
    """Cache collidable geometry welded to the world, excluding the floor."""
    return [g for g in range(model.ngeom)
            if g not in robot_geoms and model.geom(g).name != 'floor'
            and model.body_weldid[model.geom_bodyid[g]] == 0
            and (model.geom_contype[g] or model.geom_conaffinity[g])]


def blocked_route(model, data, obstacles, start, target):
    """Intersect a swept rotor envelope with conservative local geom bounds.

    This rejects unsafe routes; it does not plan an alternative. The 0.38 m
    sphere encloses the body and all rotor disks, including flight tilt.
    """
    center = np.array([0.0, 0.0, 0.05])
    for g in obstacles:
        rotation = data.geom_xmat[g].reshape(3, 3)
        p = rotation.T @ (start + center - data.geom_xpos[g])
        delta = rotation.T @ (target - start)
        # Use a Python int: tuple membership on numpy scalars and native enum
        # values does not reliably use the same comparison as scalar equality.
        kind = int(model.geom_type[g])
        size = model.geom_size[g]
        if kind == mujoco.mjtGeom.mjGEOM_PLANE:
            if min(p[2], p[2] + delta[2]) < 0.38:
                return model.geom(g).name or f'geom/{g}'
            continue
        if kind == mujoco.mjtGeom.mjGEOM_SPHERE:
            half = np.full(3, size[0])
        elif kind in (mujoco.mjtGeom.mjGEOM_CYLINDER, mujoco.mjtGeom.mjGEOM_CAPSULE):
            half = np.array([size[0], size[0], size[1]])
            if kind == mujoco.mjtGeom.mjGEOM_CAPSULE:
                half[2] += size[0]
        elif kind in (mujoco.mjtGeom.mjGEOM_BOX, mujoco.mjtGeom.mjGEOM_ELLIPSOID):
            half = size.copy()
        else:
            half = np.full(3, model.geom_rbound[g])
        half += 0.38
        near, far = 0.0, 1.0
        for axis in range(3):
            if abs(delta[axis]) < 1e-12:
                if abs(p[axis]) > half[axis]:
                    near, far = 1.0, 0.0
                    break
            else:
                a = (-half[axis] - p[axis]) / delta[axis]
                b = (half[axis] - p[axis]) / delta[axis]
                near, far = max(near, min(a, b)), min(far, max(a, b))
        if near <= far:
            return model.geom(g).name or f'geom/{g}'
    return None
