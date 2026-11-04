import base64
import hashlib
import importlib
import json
from pathlib import Path
import time

import mujoco
import numpy as np
import pytest

from robotlab import assets
from robotlab.world import meshes


@pytest.fixture(autouse=True)
def cache(tmp_path, monkeypatch):
    monkeypatch.setattr(assets, '_data_dir', tmp_path)
    return tmp_path / 'meshes'


def test_clustering_reduces_dense_surface_and_keeps_valid_buffers():
    n = 550
    x, y = np.meshgrid(np.linspace(-1, 1, n), np.linspace(-1, 1, n))
    vertices = np.column_stack([x.ravel(), y.ravel(), (.1 * np.sin(x * 4)).ravel()])
    a = (np.arange(n - 1)[:, None] * n + np.arange(n - 1)).ravel()
    faces = np.concatenate([np.column_stack([a, a + 1, a + n]), np.column_stack([a + 1, a + n + 1, a + n])])
    assert len(faces) > 600_000
    start = time.perf_counter()
    reduced, triangles = meshes.decimate(vertices, faces)
    assert time.perf_counter() - start < 10
    assert 0 < len(triangles) <= 15_000
    assert triangles.max() < len(reduced)
    assert np.all(triangles[:, 0] != triangles[:, 1])
    assert np.max(np.abs(reduced[:, :2])) <= 1
    tiny, tiny_faces = meshes.decimate(vertices, faces, max_faces=1)
    assert len(tiny_faces) <= 1
    assert tiny.shape[1] == 3


def mesh_model():
    spec = mujoco.MjSpec.from_string('''<mujoco><asset><mesh name="tetra"
        vertex="0 0 0  1 0 0  0 1 0  0 0 1" face="0 1 2  0 1 3  0 2 3  1 2 3"/>
        </asset><worldbody><geom type="mesh" mesh="tetra"/></worldbody></mujoco>''')
    return spec.compile()


def test_blob_layout_stable_key_restart_and_repair(cache):
    model = mesh_model()
    key = meshes.register_mesh(model, 0, 'dog')
    assert key == hashlib.sha1(b'dog:tetra').hexdigest()[:16]
    original = meshes.mesh_blob(key)
    wire = json.loads(original)
    positions = np.frombuffer(base64.b64decode(wire['positions']), dtype='<f4').reshape(-1, 3)
    indices = np.frombuffer(base64.b64decode(wire['indices']), dtype='<u4').reshape(-1, 3)
    assert len(positions) == wire['vertices'] == 4
    assert len(indices) == wire['faces'] == 4
    np.testing.assert_allclose(positions, model.mesh_vert)
    assert (cache / 'index.json').is_file()
    importlib.reload(meshes)
    assert meshes.mesh_blob(key) == original
    (cache / f'{key}.bin').write_bytes(b'broken')
    with pytest.raises(ValueError, match='checksum'):
        meshes.mesh_blob(key)
    assert meshes.register_mesh(model, 0, 'dog') == key
    assert meshes.mesh_blob(key) == original
    for bad in ('../bad', '0' * 16, ''):
        with pytest.raises(KeyError):
            meshes.mesh_blob(bad)


