from io import BytesIO

import numpy as np
from PIL import Image
import pytest

from robotlab.world.builder import build_world
from robotlab.world.thumbnails import render_overview


@pytest.mark.parametrize('kind', ['room', 'table'])
def test_overview_png_size_content_and_physics_unchanged(kind):
    world = build_world({'id': 'preview', 'kind': kind, 'objects': [
        {'id': 'ball', 'shape': 'sphere', 'size': [.15], 'pos': [.2, 0], 'color': '#e34a33'},
    ]}, None, '')
    state = world.data.qpos.copy()
    blob = render_overview(world)
    assert blob.startswith(b'\x89PNG\r\n\x1a\n')
    assert len(blob) > 2000
    image = Image.open(BytesIO(blob))
    assert image.size == (640, 400)
    assert np.asarray(image).std() > 10
    np.testing.assert_array_equal(world.data.qpos, state)
    assert world.data.time == 0
    custom = Image.open(BytesIO(render_overview(world, width=800, height=500)))
    assert custom.size == (800, 500)
    with pytest.raises(ValueError, match='positive integers'):
        render_overview(world, width=0)
