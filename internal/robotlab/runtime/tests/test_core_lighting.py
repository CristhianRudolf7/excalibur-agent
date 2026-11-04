import base64
from io import BytesIO

import mujoco
import numpy as np
import pytest
from PIL import Image

from robotlab.core.perception import look
from robotlab.world.builder import build_world
from robotlab.world.thumbnails import render_overview
from tests.fake_robot import FakeRobot


@pytest.mark.parametrize('size', [[1, 1], [5, 4], [30, 30]])
def test_room_rig_covers_room_without_shadows(size):
    world = build_world({'id': 'lighting', 'size': size, 'wall_height': 2.5}, None, '')
    model = world.model
    np.testing.assert_allclose(model.vis.headlight.ambient, [.4] * 3)
    np.testing.assert_allclose(model.vis.headlight.diffuse, [.55] * 3)
    assert model.vis.headlight.active
    assert model.nlight == 3
    assert not np.any(model.light_castshadow)
    assert np.all(model.light_type == mujoco.mjtLightType.mjLIGHT_DIRECTIONAL)
    assert np.all(np.abs(model.light_pos[:, 0]) <= size[0] / 2)
    assert np.all(np.abs(model.light_pos[:, 1]) <= size[1] / 2)
    assert np.all(model.light_pos[:, 2] > 2.5)
    assert np.all(model.light_dir[:, 2] < 0)


@pytest.mark.parametrize('kind', ['room', 'table'])
def test_camera_and_thumbnail_share_bright_floor_lighting(kind):
    robot = FakeRobot()
    robot.world_kind = kind
    world = build_world({'id': 'lighting', 'kind': kind, 'floor_color': '#e8e4dc'}, robot, '')
    original = world.data.qpos.copy()
    camera = look(world, width=640, height=400, camera='overview')
    rgb = np.asarray(Image.open(BytesIO(base64.b64decode(camera['png']))))
    thumbnail = np.asarray(Image.open(BytesIO(render_overview(world))))
    np.testing.assert_array_equal(rgb, thumbnail)
    with mujoco.Renderer(world.model, height=400, width=640) as renderer:
        renderer.update_scene(world.data, camera='overview')
        renderer.enable_segmentation_rendering()
        segmentation = renderer.render().copy()
    mask = ((segmentation[:, :, 0] == world.model.geom('floor').id)
            & (segmentation[:, :, 1] == int(mujoco.mjtObj.mjOBJ_GEOM)))
    assert mask.sum() > 1000
    # Pale floors stay pale across the visible floor, including shaded areas.
    assert np.all(np.percentile(rgb[mask], 10, axis=0) > np.array([232, 228, 220]) * .7)
    assert np.mean(np.any(rgb[mask] == 255, axis=1)) < .1
    np.testing.assert_array_equal(world.data.qpos, original)
    assert world.data.time == 0
