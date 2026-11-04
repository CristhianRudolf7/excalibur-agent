"""Small deterministic box robot and world modules for core-only tests."""
import sys
from types import ModuleType

import mujoco

from robotlab.robots.base import Robot, ToolSpec, Spawn
from robotlab.robots.testing import build


class FakeRobot(Robot):
    id = 'dog'
    name = {'en': 'Box', 'es': 'Caja'}
    world_kind = 'room'
    default_camera = 'robot/head'

    def attach(self, spec, spawn, menagerie):
        spec.option.timestep = .01
        body = spec.worldbody.add_body(name='robot/base', pos=[spawn.x, spawn.y, .15])
        body.add_joint(name='robot/x', type=mujoco.mjtJoint.mjJNT_SLIDE, axis=[1, 0, 0], damping=1)
        body.add_geom(type=mujoco.mjtGeom.mjGEOM_BOX, size=[.1, .1, .1], mass=1)
        body.add_site(name='robot/grasp', pos=[.2, 0, 0])
        body.add_camera(name=self.default_camera, pos=[0, -2, 1], xyaxes=[1, 0, 0, 0, .4472136, .8944272])

    def bind(self, model, data):
        self.joint = model.joint('robot/x').dofadr[0]

    def reset(self, model, data):
        self.bad = self.unsafe = False
        self.velocity = 0

    def control(self, model, data):
        data.qvel[self.joint] = self.velocity

    def push(self, ctx, dx, duration=1., fail=False, unsafe=False):
        self.velocity = dx / duration
        self.bad, self.unsafe = fail, unsafe
        try:
            ctx.step(duration)
        finally:
            self.velocity = 0
        return {'ok': True, 'moved': dx}

    def tools(self):
        return {'push': ToolSpec('push', 'Move the box', {'type': 'object', 'properties': {
            'dx': {'type': 'number'}, 'duration': {'type': 'number', 'exclusiveMinimum': 0},
            'fail': {'type': 'boolean'}, 'unsafe': {'type': 'boolean'}}, 'required': ['dx'], 'additionalProperties': False}, self.push)}

    def card(self):
        return {'id': self.id, 'name': self.name, 'tools': [{'name': 'push'}], 'cameras': [self.default_camera]}

    def fallen(self, model, data):
        return 'fell' if self.bad else None

    def safety(self, model, data):
        return 'unsafe' if self.unsafe else None

    def state(self, model, data):
        return {'x': float(data.xpos[model.body('robot/base').id, 0]), 'y': 0, 'z': .15, 'yaw_deg': 0}


class FakeWorld:
    def __init__(self, env, robot):
        self.env, self.robot = env, robot or FakeRobot()
        objects = [dict(o, size=[s/2 for s in o['size']], pos=o.get('pos', [0, 0, .1])) for o in env.get('objects', [])]
        self.model, self.data = build(self.robot, objects, Spawn(**env.get('spawn', {})))

    def object_ids(self):
        return [o['id'] for o in self.env.get('objects', [])]

    def object_body(self, id):
        return self.model.body('obj/' + id).id

    def object_info(self, id):
        return next(o for o in self.env['objects'] if o['id'] == id)

    def scene(self):
        return {'version': 1, 'bodies': [{'name': self.model.body(b).name, 'geoms': []} for b in range(self.model.nbody)],
                'cameras': [{'name': self.robot.default_camera, 'fovy': 45}]}

    def poses(self):
        return [v for b in range(self.model.nbody) for v in [*self.data.xpos[b], *self.data.xquat[b]]]

    def camera_pose(self, camera):
        id = self.model.camera(camera).id
        q = [0., 0, 0, 0]
        import numpy as np
        quat = np.array(q)
        mujoco.mju_mat2Quat(quat, self.data.cam_xmat[id])
        return [*self.data.cam_xpos[id], *quat]


def install():
    from robotlab import robots
    robots.ROBOTS = {'dog': 'tests.fake_robot:FakeRobot'}
    builder = ModuleType('robotlab.world.builder')
    builder.build_world = lambda env, robot, menagerie: FakeWorld(env, robot)
    schema = ModuleType('robotlab.world.schema')
    schema.validate = lambda env: [] if isinstance(env, dict) and env.get('kind') in ('room', 'table') else ['invalid kind']
    schema.normalize = lambda env: dict(env)
    sys.modules[builder.__name__] = builder
    sys.modules[schema.__name__] = schema
