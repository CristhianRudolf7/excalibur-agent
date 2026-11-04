"""Local image/video ingestion with bounded representative PNG frames."""
from __future__ import annotations

import base64
import shutil
import uuid
from pathlib import Path

import cv2
import numpy as np

from ..protocol import RPCError
from .store import identifier, read_json, write_json


def png(frame):
    h, w = frame.shape[:2]
    if max(h, w) > 1024:
        frame = cv2.resize(frame, (max(1, round(w * 1024 / max(h, w))), max(1, round(h * 1024 / max(h, w)))))
    ok, encoded = cv2.imencode('.png', frame)
    if not ok:
        raise RPCError('invalid_params', 'cannot encode media frame')
    return encoded.tobytes()


class Media:
    def __init__(self, root):
        self.root = Path(root) / 'media'

    def ingest(self, path, mime, name, spec=''):
        path = Path(path)
        if not path.is_file():
            raise RPCError('not_found', 'media file not found')
        if not isinstance(mime, str) or not mime.startswith(('image/', 'video/')):
            raise RPCError('invalid_params', 'expected image or video MIME type')
        kind = mime.split('/')[0]
        id = uuid.uuid4().hex
        folder = self.root / id
        folder.mkdir(parents=True)
        try:
            shutil.copyfile(path, folder / 'source')
            if kind == 'image':
                frame = cv2.imread(str(folder / 'source'))
                if frame is None:
                    raise RPCError('invalid_params', 'cannot decode image')
                frames = [frame]
            else:
                frames = self._video(folder / 'source')
            for i, frame in enumerate(frames):
                (folder / f'{i}.png').write_bytes(png(frame))
            write_json(folder / 'meta.json', {'media_id': id, 'kind': kind, 'name': name,
                                            'spec': spec, 'frames': len(frames)})
        except Exception:
            shutil.rmtree(folder)
            raise
        return {'media_id': id, 'kind': kind, 'frames': len(frames)}

    @staticmethod
    def _video(path):
        capture = cv2.VideoCapture(str(path))
        # Keep only thumbnails/histograms during selection; decode selected
        # original frames on a second pass. Memory is independent of duration.
        scenes, previous, count = [], None, 0
        try:
            while True:
                ok, frame = capture.read()
                if not ok:
                    break
                hsv = cv2.cvtColor(cv2.resize(frame, (64, 64)), cv2.COLOR_BGR2HSV)
                hist = cv2.calcHist([hsv], [0, 1], None, [16, 16], [0, 180, 0, 256])
                cv2.normalize(hist, hist)
                if previous is not None:
                    score = cv2.compareHist(previous, hist, cv2.HISTCMP_BHATTACHARYYA)
                    if score > .35:
                        scenes.append((score, count))
                        scenes = sorted(scenes, reverse=True)[:7]
                previous = hist
                count += 1
        finally:
            capture.release()
        if not count:
            raise RPCError('invalid_params', 'cannot decode video')
        selected = {0, count - 1}
        selected.update(i for _, i in sorted(scenes, reverse=True)[:4])
        # Uniform fill uses the largest remaining gap to retain coverage.
        candidates = list(range(count)) if count < 8 else np.linspace(0, count - 1, 33).astype(int).tolist()
        while len(selected) < min(8, count):
            selected.add(max((i for i in candidates if i not in selected),
                             key=lambda i: min(abs(i - j) for j in selected)))
        result = []
        capture = cv2.VideoCapture(str(path))
        try:
            for i in range(count):
                ok, frame = capture.read()
                if not ok:
                    break
                if i in selected:
                    h, w = frame.shape[:2]
                    scale = min(1, 1024 / max(h, w))
                    result.append(cv2.resize(frame, (max(1, round(w*scale)), max(1, round(h*scale)))))
        finally:
            capture.release()
        return result

    def get(self, media_id, max=8):
        folder = self.root / identifier(media_id)
        meta = read_json(folder / 'meta.json')
        if meta is None:
            raise RPCError('not_found', 'media not found')
        if type(max) is not int or not 1 <= max <= 8:
            raise RPCError('invalid_params', 'max must be in 1–8')
        return dict(meta, frames=[base64.b64encode((folder / f'{i}.png').read_bytes()).decode()
                                  for i in range(min(max, meta['frames']))])
