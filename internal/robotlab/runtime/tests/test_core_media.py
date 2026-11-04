import base64
import cv2
import numpy as np
import pytest
from robotlab.core.media import Media
from robotlab.protocol import RPCError


def test_media_image_and_scene_change_video(tmp_path):
    image = tmp_path / 'image.png'
    cv2.imwrite(str(image), np.zeros((1100, 1200, 3), np.uint8))
    video = tmp_path / 'video.mp4'
    out = cv2.VideoWriter(str(video), cv2.VideoWriter_fourcc(*'mp4v'), 10, (80, 60))
    assert out.isOpened()
    for i in range(30):
        frame = np.zeros((60, 80, 3), np.uint8)
        frame[:] = [0, 0, 255] if i < 10 else [0, 255, 0] if i < 20 else [255, 0, 0]
        out.write(frame)
    out.release()
    media = Media(tmp_path / 'data')
    for path, mime in [(image, 'image/png'), (video, 'video/mp4')]:
        result = media.ingest(path, mime, path.name, 'test specification')
        assert 1 <= result['frames'] <= 8
        saved = media.get(result['media_id'])
        assert saved['spec'] == 'test specification'
        decoded = [cv2.imdecode(np.frombuffer(base64.b64decode(f), np.uint8), cv2.IMREAD_COLOR) for f in saved['frames']]
        assert all(max(frame.shape[:2]) <= 1024 for frame in decoded)
        if result['kind'] == 'video':
            assert len(decoded) == 8
            assert len({int(np.argmax(f.mean(axis=(0, 1)))) for f in decoded}) == 3
    with pytest.raises(RPCError):
        media.ingest(image, 'text/plain', 'invalid')
