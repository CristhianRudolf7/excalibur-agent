"""Dependency-free Windows runtime checks, also exercising portable branches."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

from robotlab import sandbox


class WindowsRuntimeTests(unittest.TestCase):
    def test_windows_environment_and_namespace_gate(self):
        with tempfile.TemporaryDirectory() as directory:
            with patch.object(sys, 'platform', 'win32'), patch.dict(os.environ, {
                    'Path': 'python-path', 'SystemRoot': 'windows-root',
                    'ComSpec': 'command-shell', 'MUJOCO_GL': 'osmesa',
                    'ROBOTLAB_SENTINEL_SECRET': 'private'}, clear=True):
                env = sandbox.child_environment(Path(directory))
                self.assertEqual(env['Path'], 'python-path')
                self.assertEqual(env['SystemRoot'], 'windows-root')
                self.assertEqual(env['ComSpec'], 'command-shell')
                self.assertNotIn('MUJOCO_GL', env)
                self.assertNotIn('ROBOTLAB_SENTINEL_SECRET', env)
                self.assertFalse(sandbox.available(Path(directory)))

    def test_portable_nonblocking_pipe(self):
        read_fd, write_fd = os.pipe()
        try:
            sandbox.set_pipe_nonblocking(read_fd)
            os.write(write_fd, b'preview')
            self.assertEqual(sandbox.read_pipe(read_fd, 100), b'preview')
        finally:
            os.close(read_fd)
            os.close(write_fd)

    @unittest.skipUnless(sys.platform == 'win32', 'Win32 inherited pipe handles')
    def test_native_preview_pipe_inheritance(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            (path / 'probe.py').write_text(
                'import os\nos.write(int(os.environ["EXCALIBUR_ROBOTLAB_PREVIEW_FD"]), b"preview")\n')
            read_fd, write_fd = os.pipe()
            process = None
            try:
                process = sandbox.launch('probe', [], path, isolated=False, cwd=path,
                                         env={'EXCALIBUR_ROBOTLAB_PREVIEW_FD': str(write_fd)},
                                         pass_fds=(write_fd,), stdout=subprocess.PIPE,
                                         stderr=subprocess.PIPE)
                os.close(write_fd)
                write_fd = None
                stdout, stderr = process.communicate(timeout=10)
                self.assertEqual(process.returncode, 0, stderr.decode())
                self.assertEqual(sandbox.read_pipe(read_fd, 100), b'preview')
                self.assertEqual(sandbox.read_pipe(read_fd, 100), b'')
            finally:
                os.close(read_fd)
                if write_fd is not None:
                    os.close(write_fd)
                if process is not None:
                    sandbox.terminate(process)
                    process.stdout.close()
                    process.stderr.close()

    @unittest.skipUnless(sys.platform == 'win32', 'Win32 job and pipe APIs')
    def test_native_worker_limits_and_pipe(self):
        from robotlab.windows_runtime import PipeSelector
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            (path / 'probe.py').write_text('print("ready", flush=True)\n')
            process = sandbox.launch('probe', [], path, isolated=False,
                                     cwd=path, stdout=subprocess.PIPE,
                                     stderr=subprocess.PIPE)
            try:
                self.assertTrue(PipeSelector(process).select(10))
                stdout, stderr = process.communicate(timeout=10)
                self.assertEqual(process.returncode, 0, stderr.decode())
                self.assertEqual(stdout.strip(), b'ready')
            finally:
                sandbox.terminate(process)
                process.stdout.close()
                process.stderr.close()


if __name__ == '__main__':
    unittest.main()
