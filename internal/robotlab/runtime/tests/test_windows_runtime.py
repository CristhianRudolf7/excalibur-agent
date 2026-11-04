"""Win32 runtime helpers exercised with fake handles on every platform."""
import ctypes
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
from types import SimpleNamespace
from unittest.mock import MagicMock

import pytest


@pytest.fixture
def runtime(monkeypatch):
    kernel = MagicMock()
    kernel.CreateJobObjectW.return_value = 101
    kernel.GetCurrentProcess.return_value = 102
    kernel.SetInformationJobObject.return_value = 1
    kernel.AssignProcessToJobObject.return_value = 1
    kernel.PeekNamedPipe.return_value = 1
    monkeypatch.setattr(ctypes, 'WinDLL', lambda *a, **k: kernel, raising=False)
    crt = SimpleNamespace(get_osfhandle=lambda fd: fd + 1000,
                          open_osfhandle=lambda handle, flags: handle - 1000)
    monkeypatch.setitem(sys.modules, 'msvcrt', crt)
    path = Path(__file__).parents[1] / 'robotlab/windows_runtime.py'
    spec = importlib.util.spec_from_file_location('runtime_windows_test', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module, kernel


def test_process_limits_and_handle_lifetime(runtime):
    module, kernel = runtime
    module.constrain_current_process(10)
    args = kernel.SetInformationJobObject.call_args.args
    limits = args[2]._obj
    assert limits.basic.process_time == 100_000_000
    assert limits.process_memory == 4 * 1024**3
    assert limits.basic.flags & 0x2000
    assert module._job == 101
    kernel.AssignProcessToJobObject.assert_called_once_with(101, 102)
    kernel.CloseHandle.assert_not_called()


def test_failed_limits_close_handle(runtime, monkeypatch):
    module, kernel = runtime
    kernel.SetInformationJobObject.return_value = 0
    monkeypatch.setattr(ctypes, 'get_last_error', lambda: 5, raising=False)
    monkeypatch.setattr(ctypes, 'WinError', lambda n: OSError(n, 'denied'), raising=False)
    with pytest.raises(OSError):
        module.constrain_current_process(10)
    kernel.CloseHandle.assert_called_once_with(101)
    assert module._job is None


def test_pipe_readiness_data_and_eof(runtime, monkeypatch):
    module, kernel = runtime
    process = SimpleNamespace(stdout=SimpleNamespace(fileno=lambda: 7), poll=lambda: None)
    selector = module.PipeSelector(process)
    assert selector.select(0) == []
    def available(*args):
        args[4]._obj.value = 3
        return 1
    kernel.PeekNamedPipe.side_effect = available
    assert selector.select(0) == [True]
    monkeypatch.setattr(module.os, 'read', lambda fd, size: b'abc'[:size])
    assert module.read_pipe(7, 2) == b'ab'
    kernel.PeekNamedPipe.side_effect = None
    kernel.PeekNamedPipe.return_value = 0
    monkeypatch.setattr(ctypes, 'get_last_error', lambda: 109, raising=False)
    assert selector.select(0) == [True]
    assert module.read_pipe(7, 10) == b''
    selector.close()


def test_launch_passes_only_explicit_handles_and_restores_flags(runtime, monkeypatch):
    module, _ = runtime
    flags = []
    monkeypatch.setattr(os, 'get_handle_inheritable', lambda _: False, raising=False)
    monkeypatch.setattr(os, 'set_handle_inheritable', lambda *a: flags.append(a), raising=False)
    monkeypatch.setattr(subprocess, 'STARTUPINFO', SimpleNamespace, raising=False)
    monkeypatch.setattr(subprocess, 'CREATE_NEW_PROCESS_GROUP', 512, raising=False)
    spawned = []
    monkeypatch.setattr(subprocess, 'Popen', lambda *a, **kw: spawned.append((a, kw)) or 'process')
    env = {'EXCALIBUR_ROBOTLAB_PREVIEW_FD': '7'}
    assert module.launch_process(['probe'], env, pass_fds=(7,)) == 'process'
    assert env['EXCALIBUR_ROBOTLAB_PREVIEW_FD'] == 'handle:1007'
    assert spawned[0][1]['startupinfo'].lpAttributeList == {'handle_list': [1007]}
    assert spawned[0][1]['close_fds'] is True
    assert flags == [(1007, True), (1007, False)]


def test_restore_preview_descriptor(runtime, monkeypatch):
    module, _ = runtime
    monkeypatch.setattr(os, 'O_BINARY', 0, raising=False)
    monkeypatch.setenv('EXCALIBUR_ROBOTLAB_PREVIEW_FD', 'handle:1007')
    module.restore_preview_descriptor()
    assert os.environ['EXCALIBUR_ROBOTLAB_PREVIEW_FD'] == '7'


@pytest.mark.skipif(sys.platform != 'win32', reason='Native Win32 job API')
def test_native_child_resource_limits():
    code = ('from robotlab.windows_runtime import constrain_current_process; '
            'constrain_current_process(10); print("ready", flush=True)')
    process = subprocess.run([sys.executable, '-c', code], capture_output=True, timeout=15)
    assert process.returncode == 0, process.stderr
    assert process.stdout.strip() == b'ready'
