"""Viewer scene export from compiled MuJoCo geometry and material frames."""
from __future__ import annotations

import mujoco
import numpy as np

from .meshes import register_mesh

_TYPES = {int(getattr(mujoco.mjtGeom, 'mjGEOM_' + name.upper())): name
          for name in ('plane', 'sphere', 'capsule', 'ellipsoid', 'cylinder', 'box', 'mesh')}
_SIZES = {'plane': 2, 'sphere': 1, 'capsule': 2, 'cylinder': 2, 'box': 3, 'ellipsoid': 3}


def _bounds(model, data, gid, kind):
    rotation = data.geom_xmat[gid].reshape(3, 3)
    center = data.geom_xpos[gid].copy()
    size = model.geom_size[gid]
    if kind == 'mesh':
        mid = model.geom_dataid[gid]
        address, count = model.mesh_vertadr[mid], model.mesh_vertnum[mid]
        vertices = model.mesh_vert[address:address + count]
        low, high = vertices.min(axis=0), vertices.max(axis=0)
        center += rotation @ ((low + high) / 2)
        extent = np.abs(rotation) @ ((high - low) / 2)
    elif kind == 'sphere':
        extent = np.full(3, size[0])
    elif kind == 'capsule':
        extent = size[0] + np.abs(rotation[:, 2]) * size[1]
    elif kind == 'cylinder':
        axis = rotation[:, 2]
        extent = size[0] * np.sqrt(np.maximum(0, 1 - axis * axis)) + size[1] * np.abs(axis)
    elif kind == 'ellipsoid':
        extent = np.sqrt((rotation**2) @ (size**2))
    else:
        half = size.copy()
        if kind == 'plane':
            half[2] = 0
        extent = np.abs(rotation) @ half
    return center - extent, center + extent


def export_scene(world) -> tuple[dict, list[int]]:
    """Return the wire scene and corresponding compiled body ids in frame order."""
    model, data = world.model, world.data
    bodies, body_ids = [], []
    lows, highs = [], []
    robot_id = world.robot.id if world.robot is not None else None
    for bid in range(model.nbody):
        geoms = []
        start, count = int(model.body_geomadr[bid]), int(model.body_geomnum[bid])
        for gid in range(start, start + count):
            material = int(model.geom_matid[gid])
            color = model.mat_rgba[material] if material >= 0 else model.geom_rgba[gid]
            if model.geom_group[gid] > 2 or color[3] <= 0:
                continue
            kind = _TYPES.get(int(model.geom_type[gid]))
            if kind is None:
                raise ValueError(f'geom {model.geom(gid).name!r}: unsupported viewer geom type {model.geom_type[gid]}')
            geom = {'type': kind, 'pos': model.geom_pos[gid].tolist(),
                    'quat': model.geom_quat[gid].tolist(), 'rgba': color.tolist()}
            if kind == 'mesh':
                geom['mesh'] = register_mesh(model, int(model.geom_dataid[gid]), robot_id)
                # The compiled mesh vertices already contain MuJoCo mesh scaling.
                geom['scale'] = [1, 1, 1]
            else:
                geom['size'] = model.geom_size[gid, :_SIZES[kind]].tolist()
            geoms.append(geom)
            low, high = _bounds(model, data, gid, kind)
            lows.append(low)
            highs.append(high)
        if geoms:
            bodies.append({'name': model.body(bid).name or f'body/{bid}', 'geoms': geoms})
            body_ids.append(bid)
    order = {bid: index for index, bid in enumerate(body_ids)}
    scene = {'version': 1, 'bodies': bodies,
             'objects': [{'id': oid, 'name': world.object_info(oid)['name'],
                          'body': order[world.object_body(oid)],
                          'dynamic': world.object_info(oid)['dynamic']} for oid in world.object_ids()],
             'cameras': [{'name': model.camera(cid).name, 'fovy': float(model.cam_fovy[cid])}
                         for cid in range(model.ncam)],
             'bounds': {'min': np.min(lows, axis=0).tolist() if lows else [0, 0, 0],
                        'max': np.max(highs, axis=0).tolist() if highs else [0, 0, 0]},
             'robot': robot_id, 'environment': world.env['id']}
    return scene, body_ids
