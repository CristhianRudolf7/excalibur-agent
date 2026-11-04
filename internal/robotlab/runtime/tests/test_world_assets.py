import hashlib
import json
from pathlib import Path
from urllib.parse import unquote

import pytest

from robotlab import assets


def test_manifest_covers_every_local_file_and_hash():
    root = Path(assets.menagerie_dir())
    manifest = assets._manifest()
    assert manifest['commit'] == assets.MENAGERIE_COMMIT
    expected = {p.relative_to(root).as_posix() for folder in assets.MODEL_DIRS
                for p in (root / folder).rglob('*') if p.is_file()}
    assert {entry['path'] for entry in manifest['files']} == expected
    for entry in manifest['files']:
        path = root / entry['path']
        assert path.stat().st_size == entry['size']
        assert assets._sha256(path) == entry['sha256']
    assert len(manifest['files']) == 164


def test_full_offline_install_verify_and_skip(tmp_path):
    source = Path(assets.menagerie_dir())
    dest = tmp_path / 'models'
    urls = []

    def local_fetcher(url):
        prefix = f'{assets._RAW}/{assets.MENAGERIE_COMMIT}/'
        assert url.startswith(prefix)
        path = unquote(url[len(prefix):])
        assert path.split('/')[0] in assets.MODEL_DIRS
        urls.append(url)
        return (source / path).read_bytes()

    assert assets.fetch(dest, fetcher=local_fetcher) == dest
    assert len(urls) == len(assets._manifest()['files'])
    assert assets._complete(dest, assets._manifest())
    attribution = (dest / 'ATTRIBUTION.md').read_text()
    for model in assets.MODEL_DIRS:
        assert model in attribution
        assert (dest / model / 'LICENSE').read_bytes() == (source / model / 'LICENSE').read_bytes()
    assert 'BSD-2-Clause' in attribution and 'BSD-3-Clause' in attribution and 'Apache-2.0' in attribution

    def no_fetch(url):
        pytest.fail(f'complete install must not fetch: {url}')

    assert assets.fetch(dest, fetcher=no_fetch) == dest


@pytest.fixture
def small_manifest(tmp_path, monkeypatch):
    content = {}
    manifest = {'version': 1, 'commit': assets.MENAGERIE_COMMIT, 'models': {}, 'files': []}
    for folder in assets.MODEL_DIRS:
        manifest['models'][folder] = {'upstream': folder, 'license': 'BSD-3-Clause'}
        for filename in ('LICENSE', 'model.xml'):
            path = f'{folder}/{filename}'
            data = f'{folder} {filename}\n'.encode()
            content[path] = data
            manifest['files'].append({'path': path, 'size': len(data), 'sha256': hashlib.sha256(data).hexdigest()})
    path = tmp_path / 'manifest.json'
    path.write_text(json.dumps(manifest))
    monkeypatch.setattr(assets, '_MANIFEST', path)

    def fake_fetcher(url):
        return content[url.split(f'/{assets.MENAGERIE_COMMIT}/', 1)[1]]

    return manifest, content, fake_fetcher


def test_corrupt_and_partial_installs_are_replaced_atomically(tmp_path, small_manifest):
    manifest, content, fake = small_manifest
    dest = tmp_path / 'models'
    dest.mkdir()
    (dest / 'obsolete').write_text('old install')
    assets.fetch(dest, fetcher=fake)
    assert not (dest / 'obsolete').exists()
    target = dest / manifest['files'][0]['path']
    target.write_bytes(b'x' * target.stat().st_size)
    assert not assets._complete(dest, manifest)
    assets.fetch(dest, fetcher=fake)
    assert target.read_bytes() == content[manifest['files'][0]['path']]
    assert assets._complete(dest, manifest)
    assert not list(tmp_path.glob('.models.stage-*'))


@pytest.mark.parametrize('failure', ['size', 'sha256', 'network'])
def test_failed_install_leaves_previous_tree_untouched(tmp_path, small_manifest, failure):
    manifest, _, fake = small_manifest
    dest = tmp_path / 'models'
    dest.mkdir()
    (dest / 'old').write_text('keep')

    def broken(url):
        if failure == 'network':
            raise OSError('simulated connection failure')
        original = fake(url)
        return b'bad' if failure == 'size' else b'x' * len(original)

    with pytest.raises((ValueError, OSError), match='mismatch|simulated'):
        assets.fetch(dest, fetcher=broken)
    assert (dest / 'old').read_text() == 'keep'
    assert sorted(p.name for p in dest.iterdir()) == ['old']
    assert not list(tmp_path.glob('.models.stage-*'))


def test_cli_offline_success_failure_and_location_api(tmp_path, monkeypatch, small_manifest, capsys):
    _, _, fake = small_manifest
    monkeypatch.setattr(assets, '_download', fake)
    dest = tmp_path / 'models'
    assert assets.main(['fetch', '--dest', str(dest)]) == 0
    assert str(dest) in capsys.readouterr().out
    bad_dest = tmp_path / 'not-a-directory'
    bad_dest.write_text('file')
    with pytest.raises(SystemExit) as exc:
        assets.main(['fetch', '--dest', str(bad_dest)])
    assert exc.value.code == 1
    assert 'must be a directory' in capsys.readouterr().err
    monkeypatch.delenv('EXCALIBUR_ROBOTLAB_MENAGERIE', raising=False)
    monkeypatch.setattr(assets, '_data_dir', None)
    with pytest.raises(RuntimeError, match='unavailable'):
        assets.menagerie_dir()
    assets.set_data_dir(tmp_path)
    assert assets.data_dir() == tmp_path
    assert assets.menagerie_dir() == str(tmp_path / 'menagerie')
    monkeypatch.setenv('EXCALIBUR_ROBOTLAB_MENAGERIE', str(dest))
    assert assets.menagerie_dir() == str(dest)


def test_manifest_rejects_wrong_pin_and_paths(tmp_path, small_manifest):
    manifest, _, _ = small_manifest
    path = assets._MANIFEST
    manifest['commit'] = '0' * 40
    path.write_text(json.dumps(manifest))
    with pytest.raises(ValueError, match='pinned commit mismatch'):
        assets._manifest()
    manifest['commit'] = assets.MENAGERIE_COMMIT
    manifest['files'][0]['path'] = '../outside'
    path.write_text(json.dumps(manifest))
    with pytest.raises(ValueError, match='invalid or duplicate path'):
        assets._manifest()
