import base64
import cv2
import numpy as np
import pytest
from robotlab.core.perception import look
from robotlab.protocol import RPCError


def test_rgb_segmentation_depth_without_physics(world):
    before = world.data.time
    result = look(world, width=320, height=240)
    assert world.data.time == before
    rgb = cv2.imdecode(np.frombuffer(base64.b64decode(result['png']), np.uint8), cv2.IMREAD_COLOR)
    assert rgb.shape == (240, 320, 3)
    visible = next(o for o in result['visible'] if o['id'] == 'box')
    assert visible['pixels'] >= 30
    assert visible['bearing_deg'] < 0
    assert np.linalg.norm(np.array(visible['position']) - world.data.xpos[world.object_body('box')]) < .2
    assert visible['distance'] > 1
    with pytest.raises(RPCError, match='camera'):
        look(world, camera='missing')
