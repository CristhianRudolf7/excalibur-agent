"""Exercise flight worlds through the real JSON-lines sidecar process."""
from __future__ import annotations

import base64
import json
import os
from pathlib import Path
import selectors
import subprocess
import sys
import time


def test_flight_sidecar_manual_snapshot_and_pretrained(tmp_path: Path) -> None:
    """Verify discovery, manual input, rendering, and supported policy playback."""
    process = subprocess.Popen(
        [sys.executable, '-m', 'robotlab.server', '--data', str(tmp_path)],
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        env={**os.environ, 'MUJOCO_GL': 'osmesa', 'OMP_NUM_THREADS': '2',
             'MKL_NUM_THREADS': '2', 'OPENBLAS_NUM_THREADS': '2'},
    )
    selector = selectors.DefaultSelector()
    selector.register(process.stdout, selectors.EVENT_READ)
    pending = bytearray()
    identifier = 0

    def receive(timeout: float = 20) -> dict:
        """Read complete protocol packets with a hard per-response deadline."""
        deadline = time.monotonic() + timeout
        while b'\n' not in pending:
            assert selector.select(max(0, deadline-time.monotonic())), 'sidecar response timed out'
            data = os.read(process.stdout.fileno(), 65536)
            assert data, 'sidecar closed stdout'
            pending.extend(data)
        line, _, tail = pending.partition(b'\n')
        pending[:] = tail
        return json.loads(line)

    def call(method: str, timeout: float = 20, **params: object) -> dict:
        """Ignore asynchronous events while awaiting the matching response ID."""
        nonlocal identifier
        identifier += 1
        process.stdin.write(json.dumps({'id': identifier, 'method': method, 'params': params}).encode() + b'\n')
        process.stdin.flush()
        deadline = time.monotonic() + timeout
        while True:
            assert time.monotonic() < deadline, "sidecar RPC timed out"
            packet = receive(max(.01, deadline - time.monotonic()))
            if packet.get('id') == identifier:
                return packet

    try:
        assert receive()['event'] == 'ready'
        listing = call('scenarios.list')['result']['scenarios']
        info = {item['id']: item for item in listing}
        assert info['drone']['has_pretrained']
        assert not info['rocket']['has_pretrained']
        for scenario in ('drone', 'rocket'):
            opened = call('session.open', scenario=scenario, seed=17)['result']
            session = opened['session']
            if isinstance(session, dict):
                session = session['id']
            assert opened['scene']['scenario'] == scenario
            control = call('session.control', session=session, mode='manual',
                           inputs={'motor1': .2} if scenario == 'drone' else {'thrust': .1, 'gimbal_x': .05})
            assert control['result']['mode'] == 'manual'
            snapshot = call('session.snapshot', session=session, width=160, height=120)['result']
            assert base64.b64decode(snapshot['image_png_b64']).startswith(b'\x89PNG\r\n\x1a\n')
            assert (snapshot['width'], snapshot['height']) == (160, 120)
            policy = call('session.load_policy', timeout=60, session=session, pretrained=True)
            if scenario == 'drone':
                assert policy['result']['mode'] == 'policy'
                deadline = time.monotonic() + 20
                while True:
                    assert time.monotonic() < deadline, 'policy emitted no frames'
                    packet = receive()
                    if packet.get('event') == 'frame' and packet['data'].get('mode') == 'policy':
                        assert packet['data']['session'] == session
                        assert not packet['data']['terminated']
                        break
            else:
                assert policy['error']['code'] == 'not_found'
            assert 'result' in call('session.close', session=session)
    finally:
        selector.close()
        process.stdin.close()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)
        process.stdout.close()
        process.stderr.close()
