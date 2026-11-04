"""Compile environment documents and robot plugins into a single MuJoCo world."""
from __future__ import annotations

from copy import deepcopy
import math

import mujoco
import numpy as np

from ..robots.base import Robot, Spawn
from .schema import normalize

_GEOMS = {name: getattr(mujoco.mjtGeom, 'mjGEOM_' + name.upper())
          for name in ('box', 'sphere', 'cylinder', 'capsule')}


def rgba(color: str) -> list[float]:
    return [int(color[i:i + 2], 16) / 255 for i in (1, 3, 5)] + [1.0]


def _yaw(degrees):
    angle = math.radians(degrees) / 2
    return [math.cos(angle), 0, 0, math.sin(angle)]


def _overview(spec, size, table):
    span = max(size)
    distance = max(span, 2.5)
    target = np.array([0, 0, 0.55 if table else 0.3])
    pos = np.array([distance * .65, -distance * .85, distance * 1.15]) + target
    back = pos - target
    back /= np.linalg.norm(back)
    right = np.cross([0, 0, 1], back)
    right /= np.linalg.norm(right)
    up = np.cross(back, right)
    quat = np.empty(4)
    mujoco.mju_mat2Quat(quat, np.column_stack([right, up, back]).ravel())
    spec.worldbody.add_camera(name='overview', pos=pos, quat=quat, fovy=52)


def _lighting(spec, env):
    # The camera fill keeps interiors readable, including behind furniture.
    spec.visual.headlight.active = True
    spec.visual.headlight.ambient = [.4, .4, .4]
    spec.visual.headlight.diffuse = [.55, .55, .55]
    spec.visual.headlight.specular = [.05, .05, .05]
    x, y = [side / 2 for side in env['size']]
    ceiling = max(2, env['wall_height'] + .5,
                  max((wall['height'] + .5 for wall in env['walls']), default=0))
    for name, pos, direction, intensity in (
        ('key', [-.5 * x, -.5 * y, ceiling], [.25, .2, -1], .10),
        ('fill', [.5 * x, -.25 * y, ceiling], [-.25, .1, -1], .07),
        ('ceiling', [0, .5 * y, ceiling], [0, -.2, -1], .05),
    ):
        spec.worldbody.add_light(name=name, pos=pos, dir=direction,
                                 ambient=[0, 0, 0], diffuse=[intensity] * 3,
                                 specular=[.02] * 3, castshadow=False,
                                 type=mujoco.mjtLightType.mjLIGHT_DIRECTIONAL)


def _wall(spec, index, start, end, height, thickness, color):
    dx, dy = end[0] - start[0], end[1] - start[1]
    spec.worldbody.add_geom(name=f'wall/{index}', type=mujoco.mjtGeom.mjGEOM_BOX,
                            pos=[(start[0] + end[0]) / 2, (start[1] + end[1]) / 2, height / 2],
                            quat=_yaw(math.degrees(math.atan2(dy, dx))),
                            size=[math.hypot(dx, dy) / 2, thickness / 2, height / 2],
                            rgba=rgba(color), friction=[1, .005, .0001])


def _table(spec, size):
    body = spec.worldbody.add_body(name='table')
    body.add_geom(name='table/top', type=mujoco.mjtGeom.mjGEOM_BOX,
                  pos=[0, 0, .725], size=[size[0] / 2, size[1] / 2, .025],
                  rgba=rgba('#b78357'), friction=[1, .005, .0001])
    # Legs stay under even small custom tops.
    lx, ly = [min(.025, side / 8) for side in size]
    for i, (sx, sy) in enumerate(((-1, -1), (-1, 1), (1, -1), (1, 1))):
        body.add_geom(name=f'table/leg/{i}', type=mujoco.mjtGeom.mjGEOM_BOX,
                      pos=[sx * (size[0] / 2 - lx), sy * (size[1] / 2 - ly), .35],
                      size=[lx, ly, .35], rgba=rgba('#64554b'), friction=[1, .005, .0001])


def _object(spec, obj):
    name = 'obj/' + obj['id']
    body = spec.worldbody.add_body(name=name, pos=obj['pos'], quat=_yaw(obj['yaw']))
    if obj['dynamic']:
        body.add_freejoint(name=name + '/free')
    size, shape = obj['size'], obj['shape']
    if shape == 'box':
        half = [s / 2 for s in size]
    elif shape == 'sphere':
        half = [size[0] / 2, 0, 0]
    else:
        half = [size[0] / 2, (size[1] - (size[0] if shape == 'capsule' else 0)) / 2, 0]
    parts = [(name, [0, 0, 0], half)]
    if obj.get('compound'):
        x, y, z = half
        t = min(.01, x / 2, y / 2, z / 2)
        parts = [(name, [0, 0, -z + t / 2], [x, y, t / 2]),
                 (name + '/left', [-x + t / 2, 0, t / 2], [t / 2, y, z - t / 2]),
                 (name + '/right', [x - t / 2, 0, t / 2], [t / 2, y, z - t / 2]),
                 (name + '/front', [0, -y + t / 2, t / 2], [x - t, t / 2, z - t / 2]),
                 (name + '/back', [0, y - t / 2, t / 2], [x - t, t / 2, z - t / 2])]
    total_volume = sum(math.prod(part[2]) for part in parts)
    for geom_name, pos, half_size in parts:
        mass = obj['mass'] if len(parts) == 1 else obj['mass'] * math.prod(half_size) / total_volume
        body.add_geom(name=geom_name, type=_GEOMS[shape], size=half_size, pos=pos,
                      rgba=rgba(obj['color']), mass=mass if obj['dynamic'] else 0,
                      friction=[1, .005, .0001])


class World:
    """Compiled model and live data; scene order is shared with every pose frame."""

    def __init__(self, model, data, env, robot):
        self.model, self.data, self.env, self.robot = model, data, env, robot
        self._objects = {obj['id']: obj for obj in env['objects']}
        self._scene = None
        self._scene_body_ids = None

    def object_ids(self) -> list[str]:
        return list(self._objects)

    def object_body(self, object_id) -> int:
        if object_id not in self._objects:
            raise KeyError(object_id)
        return int(self.model.body('obj/' + object_id).id)

    def object_info(self, object_id) -> dict:
        obj = self._objects[object_id]
        return deepcopy({field: obj[field] for field in ('id', 'name', 'shape', 'size', 'dynamic', 'color')})

    def scene(self) -> dict:
        from .scene import export_scene
        if self._scene is None:
            self._scene, self._scene_body_ids = export_scene(self)
        return deepcopy(self._scene)

    def poses(self) -> list[float]:
        if self._scene_body_ids is None:
            self.scene()
        return np.column_stack((self.data.xpos[self._scene_body_ids],
                                self.data.xquat[self._scene_body_ids])).round(4).ravel().tolist()

    def camera_pose(self, camera: str) -> list[float]:
        cid = mujoco.mj_name2id(self.model, mujoco.mjtObj.mjOBJ_CAMERA, camera)
        if cid < 0:
            raise KeyError(camera)
        quat = np.empty(4)
        mujoco.mju_mat2Quat(quat, self.data.cam_xmat[cid])
        return np.concatenate((self.data.cam_xpos[cid], quat)).tolist()


def build_world(env: dict, robot: Robot | None, menagerie: str) -> World:
    env = normalize(env)
    if robot is not None and robot.world_kind != env['kind']:
        raise ValueError(f"robot {robot.id!r} requires environment kind {robot.world_kind!r}")
    spec = mujoco.MjSpec()
    spec.modelname = 'robotlab'
    spec.option.timestep = .002
    spec.visual.global_.offwidth = 640
    spec.visual.global_.offheight = 480
    _lighting(spec, env)
    table, size = env['kind'] == 'table', env['size']
    spec.worldbody.add_geom(name='floor', type=mujoco.mjtGeom.mjGEOM_PLANE,
                            size=[max(2, size[0] / 2) if table else size[0] / 2,
                                  max(2, size[1] / 2) if table else size[1] / 2, .1],
                            rgba=rgba(env['floor_color']), friction=[1, .005, .0001])
    walls = []
    if not table and env['wall_height'] > 0:
        x, y = size[0] / 2 + .05, size[1] / 2 + .05
        corners = [(-x, -y), (x, -y), (x, y), (-x, y)]
        walls = [{'from': a, 'to': b, 'height': env['wall_height'], 'thickness': .1,
                  'color': env['wall_color']} for a, b in zip(corners, corners[1:] + corners[:1])]
    for i, wall in enumerate(walls + env['walls']):
        _wall(spec, i, wall['from'], wall['to'], wall['height'], wall['thickness'], wall['color'])
    if table:
        _table(spec, size)
    for obj in env['objects']:
        _object(spec, obj)
    _overview(spec, size, table)
    if robot is not None:
        spawn = env['spawn']
        robot.attach(spec, Spawn(spawn['x'], spawn['y'], math.radians(spawn['yaw'])), menagerie)
    model = spec.compile()
    data = mujoco.MjData(model)
    if robot is not None:
        robot.bind(model, data)
    mujoco.mj_resetData(model, data)
    if robot is not None:
        robot.reset(model, data)
    mujoco.mj_forward(model, data)
    return World(model, data, env, robot)
