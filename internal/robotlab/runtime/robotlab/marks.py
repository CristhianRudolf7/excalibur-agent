"""Headless camera snapshots with segmentation-derived object marks."""
from __future__ import annotations

import base64
import io

import mujoco
import numpy as np
from PIL import Image, ImageDraw, ImageFont
from .scenario import RobotEnv


def snapshot(env: RobotEnv, camera: str | None = None, width: int = 640, height: int = 480,
             marks: bool = False) -> dict:
    """Render RGB and object masks under the caller's environment lock."""
    width, height = int(width), int(height)
    if not 16 <= width <= 1920 or not 16 <= height <= 1080:
        raise ValueError('snapshot dimensions must be between 16x16 and 1920x1080')
    if camera is not None and mujoco.mj_name2id(env.model, mujoco.mjtObj.mjOBJ_CAMERA, camera) < 0:
        raise ValueError(f'unknown camera {camera!r}')
    m = env.model
    m.vis.global_.offwidth = max(m.vis.global_.offwidth, width)
    m.vis.global_.offheight = max(m.vis.global_.offheight, height)
    cam = camera if camera is not None else -1
    if camera is None:
        default = env.scenario.default_camera()
        pos, target = np.asarray(default['pos']), np.asarray(default['target'])
        offset = pos - target
        cam = mujoco.MjvCamera()
        cam.lookat[:] = target
        cam.distance = float(np.linalg.norm(offset))
        cam.azimuth = float(np.degrees(np.arctan2(offset[1], offset[0])))
        cam.elevation = float(-np.degrees(np.arctan2(offset[2], np.linalg.norm(offset[:2]))))
    with mujoco.Renderer(m, height=height, width=width) as renderer:
        renderer.update_scene(env.data, camera=cam)
        rgb = renderer.render().copy()
        renderer.enable_segmentation_rendering()
        segmentation = renderer.render().copy()
    image = Image.fromarray(rgb)
    draw = ImageDraw.Draw(image)
    result = []
    for index, obj in enumerate(env.scenario.marks(env)):
        body = obj.get('body')
        geom = obj.get('geom')
        if isinstance(body, str):
            body = mujoco.mj_name2id(m, mujoco.mjtObj.mjOBJ_BODY, body)
        if isinstance(geom, str):
            geom = mujoco.mj_name2id(m, mujoco.mjtObj.mjOBJ_GEOM, geom)
        if geom is not None:
            ids = [int(geom)] if int(geom) >= 0 else []
        elif body is not None and int(body) >= 0:
            ids = [i for i in range(m.ngeom) if int(m.geom_bodyid[i]) == int(body)]
        else:
            continue
        mask = np.isin(segmentation[:, :, 0], ids) & (segmentation[:, :, 1] == int(mujoco.mjtObj.mjOBJ_GEOM))
        ys, xs = np.where(mask)
        if not len(xs):
            continue
        if body is None:
            body = int(m.geom_bodyid[ids[0]])
        bbox = [int(xs.min()), int(ys.min()), int(xs.max()) + 1, int(ys.max()) + 1]
        number = int(obj.get('id', index + 1))
        result.append({'id': number, 'name': obj['name'], 'label': obj.get('label', obj['name']),
                       'bbox': bbox, 'world': env.data.xpos[int(body)].tolist()})
    if marks:
        occupied = [tuple(mark['bbox']) for mark in result]
        for mark in result:
            x0, y0, x1, y1 = mark['bbox']
            draw.rectangle((x0, y0, x1 - 1, y1 - 1), outline='#2f6bff', width=1)
            # Reserve all silhouettes so badges cannot obscure neighboring parts.
            badge = badge_position(mark['bbox'], occupied, width, height)
            if badge is None:
                continue
            cx, cy = badge
            occupied.append((cx - 9, cy - 9, cx + 9, cy + 9))
            edge_x = min(x1, max(x0, cx))
            edge_y = min(y1, max(y0, cy))
            draw.line((cx, cy, edge_x, edge_y), fill='#2f6bff', width=1)
            draw.ellipse((cx - 8, cy - 8, cx + 8, cy + 8), fill='#2f6bff')
            draw.text((cx, cy), str(mark['id']), fill='white', anchor='mm', font=ImageFont.load_default(size=12))
    output = io.BytesIO()
    image.save(output, format='PNG')
    return {'image_png_b64': base64.b64encode(output.getvalue()).decode(),
            'width': width, 'height': height, 'marks': result}


def badge_position(bbox: list[int], occupied: list[tuple], width: int, height: int) -> tuple[int, int] | None:
    """Find a free badge center without obscuring any object or badge."""
    x0, y0, x1, y1 = bbox
    cx, cy = (x0 + x1) // 2, (y0 + y1) // 2
    for distance in range(14, max(width, height), 12):
        for x, y in ((cx, y0 - distance), (cx, y1 + distance),
                     (x0 - distance, cy), (x1 + distance, cy)):
            if not (9 <= x < width - 9 and 9 <= y < height - 9):
                continue
            if all(x + 10 < left or x - 10 > right or y + 10 < top or y - 10 > bottom
                   for left, top, right, bottom in occupied):
                return x, y
    return None
