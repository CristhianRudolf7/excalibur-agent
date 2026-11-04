"""Real humanoid locomotion through the JSON-lines server process."""
import json
import os
import queue
import subprocess
import threading

import pytest


class Server:
    def __init__(self, root):
        self.data = root / 'data'
        self.process = subprocess.Popen(
            [os.sys.executable, '-m', 'robotlab.server', '--data', str(self.data), '--skills', str(root / 'skills')],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        self.messages = queue.Queue()
        self.events = []
        self.next_id = 0
        self.reader = threading.Thread(target=self._read, daemon=True)
        self.reader.start()
        assert self.messages.get(timeout=20)['event'] == 'ready'

    def _read(self):
        for line in self.process.stdout:
            self.messages.put(json.loads(line))

    def call(self, method, **params):
        self.next_id += 1
        self.process.stdin.write(json.dumps({'id': self.next_id, 'method': method, 'params': params}) + '\n')
        self.process.stdin.flush()
        while True:
            message = self.messages.get(timeout=90)
            if 'event' in message:
                self.events.append(message)
                continue
            assert message['id'] == self.next_id
            assert 'error' not in message, message
            return message['result']

    def close(self):
        self.process.stdin.close()
        try:
            self.process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            self.process.kill()
            self.process.wait(timeout=5)
        self.reader.join(timeout=2)
        diagnostics = self.process.stderr.read()
        self.process.stdout.close()
        self.process.stderr.close()
        assert self.process.returncode == 0, diagnostics


@pytest.fixture
def server(tmp_path):
    server = Server(tmp_path)
    try:
        yield server
    finally:
        server.close()


def test_humanoid_corridor_walk_to_and_aggressive_failure(server):
    def start():
        return server.call('session.start', robot='humanoid', environment='pasillo', task='Walk 1.5 m ahead',
                           goal={'type': 'robot_near', 'target': {'point': [-1.7, 0]}, 'radius': .3})['session']['id']

    sid = start()
    result = server.call('robot.act', session=sid, tool='walk_to', args={'x': -1.7, 'y': 0})
    print('humanoid corridor walk_to:', json.dumps(result))
    assert result['attempt']['status'] in ('succeeded', 'failed')
    assert result['goal']['met'] == (result['attempt']['status'] == 'succeeded')
    assert result['attempt']['reason']
    assert server.call('attempt.frames', session=sid, attempt=1, count=400)['total'] > 0
    if result['attempt']['status'] == 'failed':
        assert (server.data / 'sessions' / sid / 'attempts/1/failure/state.json').is_file()
    else:
        assert server.call('session.get', session=sid)['session']['status'] == 'succeeded'
    assert any(event['event'] == 'frame' for event in server.events)
    if server.call('session.get', session=sid)['session']['status'] == 'live':
        server.call('session.stop', session=sid, by='agent')

    sid = start()
    aggressive = server.call('robot.act', session=sid, tool='walk_to',
                             args={'x': -1.7, 'y': 0, 'step_length': .35, 'step_time': .3})
    print('humanoid aggressive walk_to:', json.dumps(aggressive))
    assert not aggressive['ok'] and aggressive['attempt']['status'] == 'failed'
    assert 'tilt' in aggressive['attempt']['reason'] or 'pelvis' in aggressive['attempt']['reason']
    failure = server.data / 'sessions' / sid / 'attempts/1/failure'
    assert {path.name for path in failure.iterdir()} == {'state.json', 'code.py', 'image.png', 'image_fall.png'}
    snapshot = json.loads((failure / 'state.json').read_text())
    assert snapshot['last_call']['tool'] == 'walk_to'
    assert server.call('session.image', session=sid, path='attempts/1/failure/image.png')['png']
    assert server.call('status')['live_session'] == sid
