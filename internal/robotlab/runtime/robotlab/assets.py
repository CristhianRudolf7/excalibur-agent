"""Pinned model locations and a verified, atomic Menagerie installer.

Model assets stay outside the repository. Set EXCALIBUR_ROBOTLAB_MENAGERIE for
an existing checkout, or install into the directory returned by menagerie_dir.
"""
from __future__ import annotations

import argparse
import ctypes
import fcntl
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import tempfile
from urllib.parse import quote
from urllib.request import urlopen

MENAGERIE_COMMIT = '98a70c569c4c1b6da467a20012f766ee1f8dc521'
MODEL_DIRS = ('universal_robots_ur10e', 'robotiq_2f85', 'unitree_go2')
_data_dir: Path | None = None
_RAW = 'https://raw.githubusercontent.com/google-deepmind/mujoco_menagerie'
_UPSTREAM = 'https://github.com/google-deepmind/mujoco_menagerie'
_MANIFEST = Path(__file__).with_name('menagerie_manifest.json')


def set_data_dir(path: str | Path) -> None:
    global _data_dir
    _data_dir = Path(path)


def data_dir() -> Path:
    """The shared runtime data root (also used by persistent viewer meshes)."""
    if _data_dir is not None:
        return _data_dir
    return Path(os.environ.get('EXCALIBUR_ROBOTLAB_DATA', Path.home() / '.local/share/excalibur/robotlab'))


def menagerie_dir() -> str:
    env = os.environ.get('EXCALIBUR_ROBOTLAB_MENAGERIE')
    if env:
        return env
    if _data_dir is None:
        raise RuntimeError('menagerie models unavailable: set EXCALIBUR_ROBOTLAB_MENAGERIE')
    return str(_data_dir / 'menagerie')


def _manifest() -> dict:
    manifest = json.loads(_MANIFEST.read_text())
    if manifest['version'] != 1 or manifest['commit'] != MENAGERIE_COMMIT:
        raise ValueError('asset manifest version or pinned commit mismatch')
    if set(manifest['models']) != set(MODEL_DIRS):
        raise ValueError('asset manifest must contain exactly the three model folders')
    seen = set()
    for entry in manifest['files']:
        path = PurePosixPath(entry['path'])
        if (len(path.parts) < 2 or path.parts[0] not in MODEL_DIRS or path.is_absolute() or
                '..' in path.parts or str(path) != entry['path'] or entry['path'] in seen):
            raise ValueError(f'asset manifest: invalid or duplicate path {entry["path"]!r}')
        seen.add(entry['path'])
        if (not isinstance(entry['size'], int) or isinstance(entry['size'], bool) or entry['size'] < 0 or
                not isinstance(entry['sha256'], str) or len(entry['sha256']) != 64 or
                any(c not in '0123456789abcdef' for c in entry['sha256'])):
            raise ValueError(f'asset manifest: invalid size or sha256 for {entry["path"]}')
    for model in MODEL_DIRS:
        if f'{model}/LICENSE' not in seen:
            raise ValueError(f'asset manifest: missing {model}/LICENSE')
    return manifest


def _sha256(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()


def _attribution(manifest):
    lines = ['# Robot Lab model assets', '', f'Pinned upstream commit: `{MENAGERIE_COMMIT}`.', '',
             'The original model licenses and notices are retained in each model folder.', '']
    for model in MODEL_DIRS:
        license_name = manifest['models'][model]['license']
        lines.append(f'- `{model}`: upstream [{model}]({_UPSTREAM}/tree/{MENAGERIE_COMMIT}/{model}); '
                     f'license [{license_name}]({model}/LICENSE).')
    return '\n'.join(lines) + '\n'


def _complete(dest, manifest):
    if not dest.is_dir():
        return False
    try:
        if (dest / 'ATTRIBUTION.md').read_text() != _attribution(manifest):
            return False
        for entry in manifest['files']:
            path = dest / entry['path']
            if not path.is_file() or path.stat().st_size != entry['size'] or _sha256(path) != entry['sha256']:
                return False
    except (OSError, UnicodeError):
        return False
    return True


def _download(url):
    with urlopen(url, timeout=60) as response:
        return response.read()


def _install(stage, dest):
    if not dest.exists():
        os.replace(stage, dest)
        return
    # Linux directory exchange keeps readers on either the old complete tree or
    # the new complete tree. The old tree is then removed by TemporaryDirectory.
    library = ctypes.CDLL(None, use_errno=True)
    exchange = library.renameat2
    exchange.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
    exchange.restype = ctypes.c_int
    if exchange(-100, os.fsencode(stage), -100, os.fsencode(dest), 2) != 0:
        code = ctypes.get_errno()
        raise OSError(code, os.strerror(code), str(dest))


def fetch(dest: str | Path, *, fetcher=None) -> Path:
    """Fetch all three folders, verify every byte, and publish one complete tree.

    fetcher(url) -> bytes is injectable for offline tests. A complete install is
    hash-checked and reused without any network calls. Failed fetches leave the
    previous destination untouched.
    """
    dest = Path(dest).expanduser().absolute()
    dest.parent.mkdir(parents=True, exist_ok=True)
    manifest = _manifest()
    fetcher = fetcher or _download
    with (dest.parent / f'.{dest.name}.install.lock').open('a+b') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        if dest.exists() and not dest.is_dir():
            raise ValueError(f'asset destination {dest} must be a directory')
        if _complete(dest, manifest):
            return dest
        with tempfile.TemporaryDirectory(prefix=f'.{dest.name}.stage-', dir=dest.parent) as temp:
            stage = Path(temp) / 'models'
            stage.mkdir()
            for entry in manifest['files']:
                path = stage / entry['path']
                path.parent.mkdir(parents=True, exist_ok=True)
                url = f'{_RAW}/{MENAGERIE_COMMIT}/{quote(entry["path"], safe="/")}'
                content = fetcher(url)
                if len(content) != entry['size']:
                    raise ValueError(f'asset {entry["path"]}: size mismatch (expected {entry["size"]}, got {len(content)})')
                if hashlib.sha256(content).hexdigest() != entry['sha256']:
                    raise ValueError(f'asset {entry["path"]}: sha256 mismatch')
                path.write_bytes(content)
            (stage / 'ATTRIBUTION.md').write_text(_attribution(manifest))
            _install(stage, dest)
        return dest


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description='Install pinned Robot Lab model assets')
    commands = parser.add_subparsers(dest='command', required=True)
    command = commands.add_parser('fetch', help='Download and verify the three model folders')
    command.add_argument('--dest', required=True, type=Path)
    args = parser.parse_args(argv)
    try:
        print(fetch(args.dest))
    except (OSError, ValueError) as exc:
        parser.exit(1, f'asset installation failed: {exc}\n')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
