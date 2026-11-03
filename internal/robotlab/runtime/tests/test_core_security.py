"""Child code receives runtime settings without daemon credentials or host homes."""
import json
import subprocess
import time

import pytest

from robotlab import sandbox
from robotlab.teleop import Mapping


@pytest.mark.parametrize('isolated', [False, True], ids=['rlimits', 'bwrap'])
def test_child_environment_does_not_inherit_secrets(tmp_path, monkeypatch, isolated):
    if isolated and not sandbox.available(tmp_path):
        pytest.skip('bubblewrap namespaces unavailable on this host')
    monkeypatch.setenv('ROBOTLAB_SENTINEL_SECRET', 'must-not-reach-child')
    monkeypatch.setenv('EXCALIBUR_ROBOTLAB_SECRET', 'also-private')
    script = tmp_path / 'probe.py'
    script.write_text('import json, os\nprint(json.dumps(dict(os.environ)))\n')
    process = sandbox.launch('probe', [], tmp_path, isolated=isolated, cwd=tmp_path,
                             stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    stdout, stderr = process.communicate(timeout=10)
    assert process.returncode == 0, stderr.decode()
    environment = json.loads(stdout)
    assert 'ROBOTLAB_SENTINEL_SECRET' not in environment
    assert 'EXCALIBUR_ROBOTLAB_SECRET' not in environment
    assert environment['HOME'] == str(tmp_path)
    assert environment['OMP_NUM_THREADS'] == '2'


@pytest.mark.parametrize('isolated', [False, True], ids=['rlimits', 'bwrap'])
def test_mapping_environment_does_not_inherit_secrets(tmp_path, monkeypatch, isolated):
    if isolated and not sandbox.available(tmp_path):
        pytest.skip('bubblewrap namespaces unavailable on this host')
    monkeypatch.setattr(sandbox, 'available', lambda directory: isolated)
    monkeypatch.setenv('ROBOTLAB_SENTINEL_SECRET', 'must-not-reach-child')
    worker = Mapping(tmp_path, 'import os\ndef map_pose(pose, state):\n'
                     ' return {"leaked": int("ROBOTLAB_SENTINEL_SECRET" in os.environ)}\n')
    try:
        worker.submit({}, {})
        deadline = time.monotonic() + 10
        reply = None
        while reply is None and time.monotonic() < deadline:
            reply = worker.poll()
            time.sleep(.01)
        assert reply == {'targets': {'leaked': 0}}
    finally:
        worker.close()


def test_bubblewrap_hides_homes_and_host_processes(tmp_path):
    from pathlib import Path
    import os
    import tempfile

    directory = tmp_path / 'experiment'
    directory.mkdir()
    if not sandbox.available(directory):
        pytest.skip('bubblewrap namespaces unavailable on this host')
    # Use a real home file as well as a temporary sibling outside the writable mount.
    with tempfile.TemporaryDirectory(prefix='robotlab-probe-', dir=Path.home()) as home:
        private = Path(home) / 'secret.txt'
        private.write_text('private sentinel')
        sibling = tmp_path / 'outside.txt'
        sibling.write_text('outside sentinel')
        script = directory / 'probe.py'
        script.write_text(
            'from pathlib import Path\nimport os\n'
            f'for name in {list(map(str, (private, sibling)))!r}:\n'
            ' try:\n  Path(name).read_text()\n'
            ' except (OSError, PermissionError):\n  pass\n'
            ' else:\n  raise AssertionError("private file exposed")\n'
            f'assert not Path("/proc/{os.getpid()}/environ").exists()\n'
            'pids = [int(p.name) for p in Path("/proc").iterdir() if p.name.isdigit()]\n'
            'assert len(pids) <= 3, pids\n'
            'assert os.getpid() in pids\n'
            'print("isolated")\n')
        process = sandbox.launch('probe', [], directory, isolated=True,
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        stdout, stderr = process.communicate(timeout=10)
        assert process.returncode == 0, stderr.decode()
        assert stdout.strip() == b'isolated'


def test_bubblewrap_mounts_are_narrow(tmp_path):
    from pathlib import Path
    import sys

    args = sandbox.bubblewrap_args(tmp_path)
    for namespace in ('--unshare-pid', '--unshare-net', '--unshare-ipc', '--unshare-uts'):
        assert namespace in args
    for private in ('/home', '/root', '/run/user', '/tmp'):
        if Path(private).exists():
            assert any(args[i:i + 2] == ['--tmpfs', private] for i in range(len(args)))
    mounts = [args[i + 1] for i, arg in enumerate(args) if arg == '--ro-bind']
    assert str(Path(sys.prefix)) in mounts
    assert str(Path(sandbox.__file__).resolve().parents[1]) in mounts
    assert str(Path.home()) not in mounts
