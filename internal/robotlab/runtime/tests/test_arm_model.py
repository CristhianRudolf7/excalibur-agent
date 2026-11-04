"""Model composition checks independent of the motion controller."""
import mujoco
import numpy as np

from robotlab.assets import menagerie_dir
from robotlab.robots.arm_model import attach_model
from robotlab.robots.base import Spawn


def test_model_attachment_and_cameras():
    spec = mujoco.MjSpec()
    attach_model(spec, Spawn(-0.75, 0, 0), menagerie_dir())
    model = spec.compile()
    data = mujoco.MjData(model)
    mujoco.mj_forward(model, data)
    assert model.nu == 7
    assert model.njnt == 14
    assert np.allclose(data.body('robot/base').xpos, [-0.75, 0, 0.75])
    assert np.allclose(data.cam('robot/overhead').xpos, [0, 0, 1.95])
    assert model.camera('robot/wrist').fovy == 65
    assert model.site('robot/grasp').id >= 0
    assert model.body('robot/gripper/base_mount').parentid == model.body('robot/wrist_3_link').id
    assert all(not model.body(i).name or model.body(i).name.startswith('robot/')
               for i in range(1, model.nbody))
