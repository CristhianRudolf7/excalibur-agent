"""Numpy vertex-clustering meshes and persistent viewer mesh blobs.

Each .bin contains the UTF-8 JSON wire object from section 4.4. Its position and
index buffers are explicitly little endian. The index survives sidecar restarts.
"""
from __future__ import annotations

import base64
from contextlib import contextmanager
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import tempfile

import numpy as np

from .. import assets

MAX_FACES = 15_000
_KEY = re.compile(r'[0-9a-f]{16}\Z')


def _cache_dir():
    root = assets.data_dir()
    cache = Path(root) / 'meshes'
    cache.mkdir(parents=True, exist_ok=True)
    return cache


def _atomic(path, content):
    fd, temp = tempfile.mkstemp(prefix='.' + path.name + '-', dir=path.parent)
    try:
        with os.fdopen(fd, 'wb') as stream:
            stream.write(content)
        os.replace(temp, path)
    finally:
        if os.path.exists(temp):
            os.unlink(temp)


@contextmanager
def _locked(cache):
    with (cache / '.index.lock').open('a+b') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        try:
            yield
        finally:
            fcntl.flock(lock, fcntl.LOCK_UN)


def _index(cache):
    try:
        value = json.loads((cache / 'index.json').read_text())
        return value if isinstance(value, dict) else {}
    except (FileNotFoundError, json.JSONDecodeError):
        return {}


def _compact(vertices, faces):
    if not len(faces):
        return np.empty((0, 3), dtype='<f4'), np.empty((0, 3), dtype='<u4')
    used, inverse = np.unique(faces, return_inverse=True)
    return vertices[used].astype('<f4'), inverse.reshape(-1, 3).astype('<u4')


def decimate(vertices, faces, max_faces=MAX_FACES):
    """Average vertices in grid cells, remove collapsed/duplicate triangles.

    Sorting integer cell keys is much cheaper than sorting xyz rows. Repeating
    with a coarser grid enforces the face budget without arbitrary face deletion.
    """
    if max_faces < 1:
        raise ValueError('max_faces must be positive')
    vertices = np.asarray(vertices, dtype=np.float64).reshape(-1, 3)
    faces = np.asarray(faces, dtype=np.int64).reshape(-1, 3)
    if len(faces) <= max_faces:
        return _compact(vertices, faces)
    low = vertices.min(axis=0)
    span = np.maximum(vertices.max(axis=0) - low, 1e-12)
    unit = (vertices - low) / span
    resolution = max(1, min(128, int(np.ceil(np.sqrt(max_faces / 2)))))
    while True:
        cells = np.floor(unit * resolution).astype(np.int64)
        base = resolution + 1
        keys = cells[:, 0] + base * (cells[:, 1] + base * cells[:, 2])
        _, inverse, counts = np.unique(keys, return_inverse=True, return_counts=True)
        means = np.column_stack([np.bincount(inverse, weights=vertices[:, axis]) / counts for axis in range(3)])
        clustered = inverse[faces]
        keep = ((clustered[:, 0] != clustered[:, 1]) & (clustered[:, 1] != clustered[:, 2]) &
                (clustered[:, 0] != clustered[:, 2]))
        clustered = clustered[keep]
        if len(clustered):
            canonical = np.sort(clustered, axis=1)
            n = len(means)
            codes = canonical[:, 0] + n * (canonical[:, 1] + n * canonical[:, 2])
            _, unique = np.unique(codes, return_index=True)
            clustered = clustered[np.sort(unique)]
        if len(clustered) <= max_faces:
            return _compact(means, clustered)
        # One cell maps only extrema to distinct vertices; resolution zero
        # collapses everything, which also supports unusually tiny budgets.
        if resolution == 0:
            return _compact(means, clustered)
        factor = min(.75, .9 * (max_faces / len(clustered))**.5)
        resolution = max(0, min(resolution - 1, int(resolution * factor)))


def register_mesh(model, mesh_id: int, robot: str | None) -> str:
    """Register a compiled-model mesh and return its stable section-4.4 key."""
    name = model.mesh(mesh_id).name or f'mesh-{mesh_id}'
    robot = robot or ''
    key = hashlib.sha1(f'{robot}:{name}'.encode()).hexdigest()[:16]
    va, vn = int(model.mesh_vertadr[mesh_id]), int(model.mesh_vertnum[mesh_id])
    fa, fn = int(model.mesh_faceadr[mesh_id]), int(model.mesh_facenum[mesh_id])
    vertices, faces = model.mesh_vert[va:va + vn], model.mesh_face[fa:fa + fn]
    digest = hashlib.sha256(vertices.tobytes() + faces.tobytes()).hexdigest()
    cache = _cache_dir()
    with _locked(cache):
        index = _index(cache)
        entry = index.get(key, {})
        path = cache / f'{key}.bin'
        if entry.get('source') == digest and path.is_file():
            # An interrupted or externally damaged cache is repaired on export.
            content = path.read_bytes()
            if hashlib.sha256(content).hexdigest() == entry.get('blob'):
                return key
        vertices, faces = decimate(vertices, faces)
        blob = {'key': key, 'positions': base64.b64encode(vertices.astype('<f4').tobytes()).decode('ascii'),
                'indices': base64.b64encode(faces.astype('<u4').tobytes()).decode('ascii'),
                'vertices': len(vertices), 'faces': len(faces)}
        content = json.dumps(blob, separators=(',', ':')).encode()
        _atomic(path, content)
        index[key] = {'robot': robot, 'name': name, 'source': digest,
                      'blob': hashlib.sha256(content).hexdigest()}
        _atomic(cache / 'index.json', json.dumps(index, sort_keys=True).encode())
    return key


def mesh_blob(key: str) -> bytes:
    """Read a registered mesh without needing its source model in memory."""
    if not isinstance(key, str) or not _KEY.fullmatch(key):
        raise KeyError(key)
    cache = _cache_dir()
    with _locked(cache):
        entry = _index(cache).get(key)
        if entry is None:
            raise KeyError(key)
        try:
            content = (cache / f'{key}.bin').read_bytes()
        except FileNotFoundError:
            raise KeyError(key) from None
        if hashlib.sha256(content).hexdigest() != entry.get('blob'):
            raise ValueError(f'mesh {key}: cached blob checksum mismatch; export scene to repair')
        return content
