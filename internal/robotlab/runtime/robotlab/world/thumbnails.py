"""Headless overview PNGs without advancing physics."""
from __future__ import annotations

from io import BytesIO
import os

os.environ.setdefault('MUJOCO_GL', 'osmesa')

import mujoco
from PIL import Image


def render_overview(world, width=640, height=400) -> bytes:
    if any(not isinstance(value, int) or isinstance(value, bool) or value <= 0 for value in (width, height)):
        raise ValueError('thumbnail width and height must be positive integers')
    if mujoco.mj_name2id(world.model, mujoco.mjtObj.mjOBJ_CAMERA, 'overview') < 0:
        raise KeyError('overview')
    world.model.vis.global_.offwidth = max(width, world.model.vis.global_.offwidth)
    world.model.vis.global_.offheight = max(height, world.model.vis.global_.offheight)
    with mujoco.Renderer(world.model, height=height, width=width) as renderer:
        renderer.update_scene(world.data, camera='overview')
        pixels = renderer.render()
    output = BytesIO()
    Image.fromarray(pixels).save(output, format='PNG')
    return output.getvalue()
