"""Assemble the arm, parallel gripper, pedestal and robot cameras."""
from __future__ import annotations

import math
from pathlib import Path

import mujoco

from .base import Spawn


def attach_model(spec: mujoco.MjSpec, spawn: Spawn, menagerie: str) -> None:
    spec.option.timestep = 0.002
    spec.option.integrator = mujoco.mjtIntegrator.mjINT_IMPLICITFAST
    spec.option.cone = mujoco.mjtCone.mjCONE_ELLIPTIC
    spec.option.impratio = 10
    root = Path(menagerie)
    arm = mujoco.MjSpec.from_file(str(root / 'universal_robots_ur10e/ur10e.xml'))
    gripper = mujoco.MjSpec.from_file(str(root / 'robotiq_2f85/2f85.xml'))
    arm.option.cone = mujoco.mjtCone.mjCONE_ELLIPTIC
    arm.option.impratio = 10
    # The attached gripper changes wrist inertia. Avoid torque-clipped servo
    # oscillation at the fixed 2 ms physics timestep.
    for name in ('wrist_1', 'wrist_2', 'wrist_3'):
        actuator = arm.actuator(name)
        actuator.gainprm[0] = 1200
        actuator.biasprm[1:3] = [-1200, -50]
    # The source keyframe has six coordinates, before the gripper is attached.
    for key in list(arm.keys):
        arm.delete(key)
    arm.attach(gripper, prefix='gripper/', site=arm.site('attachment_site'))
    arm.site('gripper/pinch').name = 'grasp'
    arm.body('gripper/base').add_camera(
        name='wrist', pos=[0.065, 0, 0.065], quat=[0, 1, 0, 0], fovy=65)

    pedestal = spec.worldbody.add_body(name='robot/pedestal', pos=[spawn.x, spawn.y, 0])
    pedestal.add_geom(name='robot/pedestal', type=mujoco.mjtGeom.mjGEOM_CYLINDER,
                      pos=[0, 0, 0.375], size=[0.12, 0.375, 0],
                      rgba=[0.25, 0.28, 0.3, 1])
    pedestal.add_camera(name='robot/overhead', pos=[-spawn.x, -spawn.y, 1.95], fovy=65)
    frame = pedestal.add_frame(pos=[0, 0, 0.75],
                               quat=[math.cos(spawn.yaw / 2), 0, 0, math.sin(spawn.yaw / 2)])
    spec.attach(arm, prefix='robot/', frame=frame)
