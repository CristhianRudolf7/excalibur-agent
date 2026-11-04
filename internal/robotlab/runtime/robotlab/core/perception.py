"""Robot RGB observation plus depth/segmentation-derived world estimates."""
from __future__ import annotations

import base64
import math
import threading

import cv2
import mujoco
import numpy as np

from ..protocol import RPCError

# Offscreen contexts are kept on the calling thread and closed before returning.
# Serializing creation also avoids concurrent backend initialization.
_render_lock = threading.RLock()


def look(world, camera=None, width=640, height=480):
    camera = camera or world.robot.default_camera
    if type(width) is not int or type(height) is not int or not 16 <= width <= 1024 or not 16 <= height <= 1024:
        raise RPCError('invalid_params', 'image dimensions must be integers in 16–1024')
    model, data = world.model, world.data
    cid = mujoco.mj_name2id(model, mujoco.mjtObj.mjOBJ_CAMERA, camera)
    if cid < 0:
        raise RPCError('not_found', f'camera not found: {camera}')
    with _render_lock:
        model.vis.global_.offwidth = max(model.vis.global_.offwidth, width)
        model.vis.global_.offheight = max(model.vis.global_.offheight, height)
        with mujoco.Renderer(model, height=height, width=width) as renderer:
            renderer.update_scene(data, camera=camera)
            rgb = renderer.render().copy()
            renderer.enable_segmentation_rendering()
            segmentation = renderer.render().copy()
            renderer.disable_segmentation_rendering()
            renderer.enable_depth_rendering()
            depth = renderer.render().copy()
    visible = []
    f = .5 * height / math.tan(math.radians(float(model.cam_fovy[cid])) / 2)
    rotation = data.cam_xmat[cid].reshape(3, 3)
    origin = data.cam_xpos[cid]
    for oid in world.object_ids():
        body = world.object_body(oid)
        # Include any descendant geoms if a future world object is articulated.
        bodies = {body}
        for b in range(body + 1, model.nbody):
            if int(model.body_parentid[b]) in bodies:
                bodies.add(b)
        geom_ids = [g for g in range(model.ngeom) if int(model.geom_bodyid[g]) in bodies]
        mask = np.isin(segmentation[:, :, 0], geom_ids) & (segmentation[:, :, 1] == int(mujoco.mjtObj.mjOBJ_GEOM))
        ys, xs = np.nonzero(mask)
        if len(xs) < 30:
            continue
        cx, cy = float(xs.mean()), float(ys.mean())
        px, py = int(round(cx)), int(round(cy))
        # Use the closest actual mask pixel when the centroid lies in a hole.
        if not mask[py, px]:
            nearest = np.argmin((xs-cx)**2 + (ys-cy)**2)
            px, py = int(xs[nearest]), int(ys[nearest])
        z = float(depth[py, px])
        if not math.isfinite(z) or z <= 0:
            continue
        local = np.array([(cx - (width - 1)/2) * z / f,
                          -(cy - (height - 1)/2) * z / f, -z])
        position = origin + rotation @ local
        visible.append({'id': oid, 'name': world.object_info(oid)['name'], 'pixels': len(xs),
                        'distance': round(float(np.linalg.norm(local)), 4),
                        'bearing_deg': round(math.degrees(math.atan2(-local[0], -local[2])), 3),
                        'elevation_deg': round(math.degrees(math.atan2(local[1], math.hypot(local[0], local[2]))), 3),
                        'position': position.round(4).tolist()})
    ok, encoded = cv2.imencode('.png', cv2.cvtColor(rgb, cv2.COLOR_RGB2BGR))
    if not ok:
        raise RPCError('internal', 'image encoding failed')
    return {'png': base64.b64encode(encoded.tobytes()).decode(), 'visible': visible,
            'state': world.robot.state(model, data)}
