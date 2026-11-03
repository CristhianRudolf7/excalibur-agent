"""Resource-limited child launch with optional network-isolated bubblewrap."""
from __future__ import annotations

import argparse
import os
from pathlib import Path
import resource
import shutil
import signal
import subprocess
import sys


def bubblewrap_args(directory: Path) -> list[str]:
    """Hide private trees and host processes, then expose only runtime inputs."""
    path = str(directory.resolve())
    command = ['bwrap', '--ro-bind', '/', '/', '--unshare-net', '--unshare-pid',
               '--unshare-ipc', '--unshare-uts', '--die-with-parent', '--new-session',
               '--dev', '/dev', '--proc', '/proc']
    for private in ('/home', '/root', '/run/user', '/tmp'):
        if Path(private).exists():
            command.extend(['--tmpfs', private])
    # The package tree includes bundled scenario assets and pretrained policies.
    # Keep these mounts narrow: exposing their parent would reveal user files.
    runtime = Path(__file__).resolve().parents[1]
    inputs = {Path(sys.prefix), Path(sys.base_prefix), runtime,
              Path(sys.executable).resolve()}
    for source in sorted(inputs, key=lambda entry: (len(entry.parts), str(entry))):
        command.extend(['--ro-bind', str(source), str(source)])
    command.extend(['--bind', path, path, '--chdir', path])
    return command


def available(directory: Path) -> bool:
    """Probe actual namespace support rather than trusting executable presence."""
    if not shutil.which('bwrap'):
        return False
    try:
        return subprocess.run(bubblewrap_args(directory) + ['/bin/true'],
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                              timeout=3, check=False, env=child_environment(directory)).returncode == 0
    except (OSError, subprocess.TimeoutExpired):
        return False


def child_environment(directory: Path, context: dict | None = None) -> dict[str, str]:
    """Pass runtime settings explicitly; never inherit daemon credentials."""
    allowed = {'PATH', 'LANG', 'TZ'}
    result = {key: value for key, value in os.environ.items()
              if key in allowed or key.startswith('LC_')}
    result.update({'HOME': str(directory.resolve()),
                   'PYTHONPATH': str(Path(__file__).resolve().parents[1]),
                   'OMP_NUM_THREADS': '2', 'MKL_NUM_THREADS': '2',
                   'OPENBLAS_NUM_THREADS': '2', 'MUJOCO_GL': 'osmesa',
                   'LP_NUM_THREADS': '2', 'PYTHONDONTWRITEBYTECODE': '1',
                   'PYTHONUNBUFFERED': '1'})
    # This opt-in hook names modules within the exposed runtime source tree.
    modules = os.environ.get('EXCALIBUR_ROBOTLAB_SCENARIO_MODULES')
    if modules:
        result['EXCALIBUR_ROBOTLAB_SCENARIO_MODULES'] = modules
    owned = {'EXCALIBUR_ROBOTLAB_PREVIEW_FD', 'EXCALIBUR_ROBOTLAB_EXPERIMENT_DIR',
             'EXCALIBUR_ROBOTLAB_RUN_ID', 'EXCALIBUR_ROBOTLAB_RUN_STARTED'}
    for key, value in (context or {}).items():
        if key not in owned:
            raise ValueError(f'unsupported child context variable: {key}')
        result[key] = str(value)
    return result


def launch(module: str, args: list[str], directory: Path, *, isolated: bool,
           cpu_seconds: int = 7200, env: dict | None = None, **kwargs) -> subprocess.Popen:
    """Launch in a new process group; the bootstrap sets limits before imports."""
    command = [sys.executable, '-m', 'robotlab.sandbox', '--cpu', str(cpu_seconds),
               '--module', module, *args]
    if isolated:
        command = bubblewrap_args(directory) + command
    child_env = child_environment(directory, env)
    return subprocess.Popen(command, env=child_env, start_new_session=True, **kwargs)


def terminate(process: subprocess.Popen) -> None:
    """Kill and reap an entire child process group, including descendants."""
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    process.wait(timeout=5)


def main() -> None:
    """Apply limits without unsafe preexec hooks in a multithreaded parent."""
    parser = argparse.ArgumentParser()
    parser.add_argument('--cpu', type=int, default=7200)
    parser.add_argument('--module', required=True)
    args, rest = parser.parse_known_args()
    resource.setrlimit(resource.RLIMIT_AS, (4 * 1024**3, 4 * 1024**3))
    cpu = max(1, args.cpu)
    resource.setrlimit(resource.RLIMIT_CPU, (cpu, cpu))
    os.nice(max(0, 15 - os.getpriority(os.PRIO_PROCESS, 0)))
    import runpy
    sys.argv = [args.module, *rest]
    runpy.run_module(args.module, run_name='__main__')


if __name__ == '__main__':
    main()
