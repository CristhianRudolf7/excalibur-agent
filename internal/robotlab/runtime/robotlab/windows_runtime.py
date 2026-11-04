"""Win32 resource limits and pipe readiness using only the standard library."""
from __future__ import annotations

import ctypes
from ctypes import wintypes
import msvcrt
import os
import subprocess
import threading
import time

kernel = ctypes.WinDLL('kernel32', use_last_error=True)
kernel.CreateJobObjectW.argtypes = [ctypes.c_void_p, wintypes.LPCWSTR]
kernel.CreateJobObjectW.restype = wintypes.HANDLE
kernel.GetCurrentProcess.restype = wintypes.HANDLE
kernel.SetInformationJobObject.argtypes = [wintypes.HANDLE, ctypes.c_int, ctypes.c_void_p, wintypes.DWORD]
kernel.AssignProcessToJobObject.argtypes = [wintypes.HANDLE, wintypes.HANDLE]
kernel.CloseHandle.argtypes = [wintypes.HANDLE]
kernel.PeekNamedPipe.argtypes = [wintypes.HANDLE, ctypes.c_void_p, wintypes.DWORD,
                                ctypes.c_void_p, ctypes.POINTER(wintypes.DWORD), ctypes.c_void_p]


class BasicLimits(ctypes.Structure):
    _fields_ = [('process_time', ctypes.c_int64), ('job_time', ctypes.c_int64),
                ('flags', wintypes.DWORD), ('minimum', ctypes.c_size_t),
                ('maximum', ctypes.c_size_t), ('active', wintypes.DWORD),
                ('affinity', ctypes.c_size_t), ('priority', wintypes.DWORD),
                ('scheduling', wintypes.DWORD)]


class ExtendedLimits(ctypes.Structure):
    _fields_ = [('basic', BasicLimits), ('io', ctypes.c_uint64 * 6),
                ('process_memory', ctypes.c_size_t), ('job_memory', ctypes.c_size_t),
                ('peak_process', ctypes.c_size_t), ('peak_job', ctypes.c_size_t)]


# This non-inheritable handle stays open for the worker's lifetime. Windows
# closes it on exit (including TerminateProcess), killing all descendants.
_job = None


def constrain_current_process(cpu_seconds: int) -> None:
    global _job
    handle = kernel.CreateJobObjectW(None, None)
    if not handle:
        raise ctypes.WinError(ctypes.get_last_error())
    limits = ExtendedLimits()
    # KILL_ON_JOB_CLOSE | PROCESS_MEMORY | PROCESS_TIME | PRIORITY_CLASS
    limits.basic.flags = 0x2000 | 0x100 | 0x2 | 0x20
    limits.basic.process_time = cpu_seconds * 10_000_000
    limits.basic.priority = 0x4000  # BELOW_NORMAL_PRIORITY_CLASS
    limits.process_memory = 4 * 1024**3
    if not kernel.SetInformationJobObject(handle, 9, ctypes.byref(limits), ctypes.sizeof(limits)):
        error = ctypes.WinError(ctypes.get_last_error())
        kernel.CloseHandle(handle)
        raise error
    if not kernel.AssignProcessToJobObject(handle, kernel.GetCurrentProcess()):
        error = ctypes.WinError(ctypes.get_last_error())
        kernel.CloseHandle(handle)
        raise error
    _job = handle


class PipeSelector:
    """Poll anonymous pipes without select(), which only accepts sockets on Windows."""
    def __init__(self, process):
        self.process = process
        self.handle = msvcrt.get_osfhandle(process.stdout.fileno())

    def select(self, timeout: float):
        deadline = time.monotonic() + timeout
        while True:
            available = wintypes.DWORD()
            if not kernel.PeekNamedPipe(self.handle, None, 0, None, ctypes.byref(available), None):
                error = ctypes.get_last_error()
                if error in (109, 232):  # Broken/closing pipe: let the reader observe EOF.
                    return [True]
                raise ctypes.WinError(error)
            if available.value or self.process.poll() is not None:
                return [True]
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                return []
            time.sleep(min(0.005, remaining))

    def close(self):
        pass


_launch_lock = threading.Lock()


def launch_process(command, env, **kwargs):
    """Pass only explicit pipe handles; CRT descriptor numbers are process-local."""
    descriptors = kwargs.pop('pass_fds', ())
    handles = [msvcrt.get_osfhandle(fd) for fd in descriptors]
    info = subprocess.STARTUPINFO()
    if handles:
        info.lpAttributeList = {'handle_list': handles}
    descriptor = env.get('EXCALIBUR_ROBOTLAB_PREVIEW_FD')
    if descriptor is not None and int(descriptor) in descriptors:
        env['EXCALIBUR_ROBOTLAB_PREVIEW_FD'] = 'handle:' + str(msvcrt.get_osfhandle(int(descriptor)))
    with _launch_lock:
        previous = [os.get_handle_inheritable(handle) for handle in handles]
        try:
            for handle in handles:
                os.set_handle_inheritable(handle, True)
            return subprocess.Popen(command, env=env, startupinfo=info,
                                    creationflags=subprocess.CREATE_NEW_PROCESS_GROUP,
                                    close_fds=True, **kwargs)
        finally:
            for handle, inherited in zip(handles, previous):
                os.set_handle_inheritable(handle, inherited)


def restore_preview_descriptor():
    value = os.environ.get('EXCALIBUR_ROBOTLAB_PREVIEW_FD', '')
    if value.startswith('handle:'):
        descriptor = msvcrt.open_osfhandle(int(value[7:]), os.O_WRONLY | os.O_BINARY)
        os.environ['EXCALIBUR_ROBOTLAB_PREVIEW_FD'] = str(descriptor)


def read_pipe(descriptor: int, size: int) -> bytes:
    available = wintypes.DWORD()
    handle = msvcrt.get_osfhandle(descriptor)
    if not kernel.PeekNamedPipe(handle, None, 0, None, ctypes.byref(available), None):
        error = ctypes.get_last_error()
        if error in (109, 232):
            return b''
        raise ctypes.WinError(error)
    if not available.value:
        return b''
    return os.read(descriptor, min(size, available.value))
