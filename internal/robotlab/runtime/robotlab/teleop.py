"""Persistent sandboxed pose mapping with bounded asynchronous replies."""
from __future__ import annotations

import ast
import json
import os
from pathlib import Path
import queue
import selectors
import subprocess
import sys
import threading
import time

from . import sandbox
from .protocol import MAX_LINE, read_line


class Mapping:
    """Run user mapping code away from the physics thread; never wait over 50 ms."""
    def __init__(self, directory: Path, code: str):
        ast.parse(code)
        directory.mkdir(parents=True, exist_ok=True)
        path = directory / 'mapping.py'
        path.write_text(code)
        self.process = sandbox.launch('robotlab.teleop', [str(path)], directory,
                                      isolated=sandbox.available(directory),
                                      stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                      stderr=subprocess.DEVNULL)
        self.requests = queue.Queue(maxsize=1)
        self.responses = queue.Queue(maxsize=1)
        self.closed = threading.Event()
        self.thread = threading.Thread(target=self._worker, name='robotlab-pose', daemon=True)
        self.thread.start()

    @staticmethod
    def _reply(process, selector, timeout: float) -> dict:
        """Read a whole bounded reply within one wall-clock deadline."""
        deadline = time.monotonic() + timeout
        data = bytearray()
        while time.monotonic() < deadline:
            if not selector.select(max(0.0, deadline - time.monotonic())):
                break
            raw = os.read(process.stdout.fileno(), 65536)
            if not raw:
                raise RuntimeError("mapping worker exited")
            data.extend(raw)
            if len(data) > MAX_LINE:
                raise ValueError("mapping output exceeds 8 MiB")
            if b"\n" in data:
                value = json.loads(data.split(b"\n", 1)[0])
                if not isinstance(value, dict):
                    raise ValueError("mapping output must be an object")
                return value
        raise TimeoutError("pose mapping exceeded 50 ms; late answer dropped")

    def _worker(self) -> None:
        process = self.process
        selector = selectors.DefaultSelector()
        selector.register(process.stdout, selectors.EVENT_READ)
        try:
            # Startup is not charged to the per-frame mapping budget.
            ready = self._reply(process, selector, 5)
            if ready != {'ready': True}:
                raise RuntimeError(ready.get('error', 'mapping worker failed to start'))
            while not self.closed.is_set():
                try:
                    request = self.requests.get(timeout=0.05)
                except queue.Empty:
                    continue
                process.stdin.write(json.dumps(request).encode() + b'\n')
                process.stdin.flush()
                result = self._reply(process, selector, 0.05)
                if not isinstance(result, dict):
                    raise RuntimeError('mapping worker exited')
                try:
                    self.responses.put_nowait(result)
                except queue.Full:
                    pass
        except Exception as exc:
            try:
                self.responses.put_nowait({'error': str(exc)})
            except queue.Full:
                pass
        finally:
            selector.close()
            sandbox.terminate(process)

    def submit(self, pose: dict, state: dict) -> None:
        """Keep only one pending pose; no stale frame backlog."""
        try:
            self.requests.put_nowait({'pose': pose, 'state': state})
        except queue.Full:
            pass

    def poll(self) -> dict | None:
        """Consume a ready result without delaying the physics loop."""
        try:
            return self.responses.get_nowait()
        except queue.Empty:
            return None

    def close(self) -> None:
        """Stop and join the worker."""
        self.closed.set()
        sandbox.terminate(self.process)
        self.thread.join(timeout=1)
        for stream in (self.process.stdin, self.process.stdout):
            stream.close()


def main() -> None:
    """Mapping subprocess entry point; user stdout cannot corrupt its protocol."""
    import contextlib
    path = Path(sys.argv[1])
    namespace = {}
    output = sys.stdout
    try:
        with contextlib.redirect_stdout(sys.stderr):
            exec(compile(path.read_text(), str(path), 'exec'), namespace)
        mapping = namespace.get('map_pose')
        if not callable(mapping):
            raise ValueError('mapping must define map_pose(pose, state)')
    except Exception as exc:
        output.write(json.dumps({'error': str(exc)[:1000]}) + '\n')
        output.flush()
        return
    output.write('{"ready":true}\n')
    output.flush()
    state = {}
    while True:
        request = read_line(sys.stdin.buffer)
        if request is None:
            break
        try:
            with contextlib.redirect_stdout(sys.stderr):
                state.update(request['state'])
                targets = mapping(request['pose'], state)
            result = {'targets': targets}
            line = json.dumps(result, allow_nan=False)
            if len(line.encode()) > MAX_LINE - 1:
                raise ValueError('mapping output too large')
        except Exception as exc:
            line = json.dumps({'error': str(exc)[:1000]})
        output.write(line + '\n')
        output.flush()


if __name__ == '__main__':
    main()
