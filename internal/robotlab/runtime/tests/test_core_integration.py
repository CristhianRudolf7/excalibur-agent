"""Public runtime scenarios using installed models and real rigid-body physics."""
import base64
import io
import math

import pytest

from robotlab.protocol import MAX_LINE, RPCError, Writer, read_line
from robotlab.server import Runtime


class Client:
    def __init__(self, root):
        self.events = []
        self.runtime = Runtime(root / 'data', root / 'skills', self.event)

    @staticmethod
    def wire(value):
        output = io.StringIO()
        Writer(output).send(value)
        encoded = output.getvalue().encode()
        assert len(encoded) <= MAX_LINE
        return read_line(io.BytesIO(encoded))

    def event(self, name, data):
        self.events.append(self.wire({'event': name, 'data': data}))

    def call(self, method, **params):
        request = self.wire({'method': method, 'params': params})
        return self.wire({'result': self.runtime.dispatch(**request)})['result']

    def start(self, robot, environment, goal):
        self.sid = self.call('session.start', robot=robot, environment=environment,
                             task='Verify deterministic training', goal=goal)['session']['id']
        frames = self.call('attempt.frames', session=self.sid, attempt=1)
        assert frames['total'] == 1
        initial = frames['frames'][0]
        assert initial['seq'] == 0 and initial['t'] == 0
        assert [event['data'] for event in self.events if event['event'] == 'frame'] == [initial]
        return self.sid

    def act(self, tool, **args):
        result = self.call('robot.act', session=self.sid, tool=tool, args=args)
        assert result['ok'], (tool, args, result)
        if result['attempt']['status'] == 'running':
            observed = self.call('robot.state', session=self.sid)
            assert observed['state'] == result['state']
            assert observed['goal'] == result['goal']
        return result

    def succeeded(self, attempt=1):
        session = self.call('session.get', session=self.sid)['session']
        assert session['status'] == session['attempts'][-1]['status'] == 'succeeded'
        assert session['outcome']['status'] == 'succeeded'
        frames = self.call('attempt.frames', session=self.sid, attempt=attempt, count=400)
        assert frames['total'] > 0
        assert frames['total'] == session['attempts'][attempt - 1]['frames']
        assert frames['total'] == 1 + math.floor((session['attempts'][attempt - 1]['sim_time'] + 1e-8) * 20)
        scene = self.call('session.scene', session=self.sid)['scene']
        assert all(len(frame['poses']) == 7 * len(scene['bodies']) for frame in frames['frames'])
        assert any(event['event'] == 'frame' for event in self.events)
        return scene


@pytest.fixture
def client(tmp_path):
    client = Client(tmp_path)
    yield client
    client.runtime.close()


def test_wire_session_replay_and_learning(client, environment, fake_modules):
    client.runtime.environments.save(environment)
    sid = client.start('dog', 'test-room', {'type': 'robot_near',
                       'target': {'point': [1, 0]}, 'radius': .1})
    observation = client.call('robot.look', session=sid)
    assert base64.b64decode(observation['png']).startswith(b'\x89PNG')
    failed = client.call('robot.act', session=sid, tool='push', args={'dx': 0, 'fail': True})
    assert failed['attempt']['status'] == 'failed'
    learned = client.call('skills.write', session=sid, name='small-steps', description='Move safely',
                          instructions='Inspect state after each keyframe.', failures=[1])
    assert learned['accepted'] and learned['skill']['failures'] == 1
    assert client.call('attempt.reset', session=sid, reflection='Use small steps')['attempt'] == 2
    client.call('skills.use', session=sid, name='small-steps')
    result = client.act('push', dx=1.2)
    assert result['goal']['met']
    session = client.call('session.get', session=sid)['session']
    assert session['status'] == 'succeeded'
    frames = client.call('attempt.frames', session=sid, attempt=2, count=400)
    scene = client.call('session.scene', session=sid)['scene']
    assert frames['total'] > 0
    assert all(len(f['poses']) == 7 * len(scene['bodies']) for f in frames['frames'])
    assert client.call('skills.read', session=sid, name='small-steps')['skill']['success_rate'] == 1


def assert_look(client, camera):
    observation = client.call('robot.look', session=client.sid, camera=camera)
    assert base64.b64decode(observation['png']).startswith(b'\x89PNG')
    return observation


def test_real_dog_dispatch_learning_stairs_and_recording(client):
    from tests.test_dog_stairs import climb_plan

    sid = client.start('dog', 'escalera-estrecha', {
        'type': 'robot_on', 'target': {'object': 'descanso'}, 'feet': 4, 'stable_s': 1,
    })
    initial = client.call('robot.state', session=sid)
    assert set(initial['state']['legs']) == {'FL', 'FR', 'RL', 'RR'}
    assert_look(client, 'robot/head')
    assert_look(client, 'robot/rear')
    first = client.act('sequence', keyframes=[
        {'type': 'feet', 'feet': {}, 'trunk': {}, 'duration': .2},
    ])
    assert not first['goal']['met']
    with pytest.raises(RPCError, match='write or improve a skill'):
        client.call('attempt.reset', session=sid, reflection='Record the stable reference before climbing.')
    abandoned = client.call('session.get', session=sid)['session']['attempts'][0]
    assert abandoned['status'] == 'abandoned' and abandoned['frames'] > 0
    learned = client.call('skills.write', session=sid, name='stairs-reference',
                          description='Keep the planted feet stable before lifting.',
                          instructions='Shift the COM inside the support triangle before lifting a foot.\n'
                          'Reference keyframe: `{type: feet, feet: {}, trunk: {}, duration: 0.2}`.\n'
                          'Lift, transfer and lower one foot at a time; inspect support and margin.',
                          failures=[1])
    assert learned['accepted'] and learned['skill']['failures'] == 1
    assert client.call('attempt.reset', session=sid,
                       reflection='Use the documented reference and small keyframes.')['attempt'] == 2
    client.call('skills.use', session=sid, name='stairs-reference')
    result, keyframes, calls = climb_plan(client.act, slower=True)
    assert result['goal']['met']
    assert 0 < keyframes <= 224 and calls <= 58
    client.succeeded(attempt=2)
    assert result['attempt']['status'] == 'succeeded'
    assert len(result['state']['support']) == 4
    skill = client.call('skills.read', session=sid, name='stairs-reference')['skill']
    assert skill['uses'] == 1 and skill['success_rate'] == 1
    entries = client.call('session.log', session=sid, attempt=2)['entries']
    actions = [entry for entry in entries if entry['kind'] == 'act']
    assert len(actions) == calls and all('legs' in entry['state'] for entry in actions)


def test_real_arm_dispatch_places_box_on_builtin_shelf(client):
    from tests.test_arm_acceptance import shelf_plan

    sid = client.start('arm', 'estante-pequeno', {
        'type': 'object_on', 'object': 'caja', 'target': {'object': 'estante'}, 'stable_s': 1,
    })
    initial = client.call('robot.state', session=sid)
    assert initial['state']['held'] is None
    assert {obj['id'] for obj in initial['state']['objects']} == {'caja', 'estante'}
    assert_look(client, 'robot/wrist')
    assert_look(client, 'robot/overhead')
    result = shelf_plan(act=client.act)
    client.succeeded()
    assert result['goal']['met'] and result['state']['held'] is None
    entries = client.call('session.log', session=sid)['entries']
    actions = [entry for entry in entries if entry['kind'] == 'act']
    assert [entry['tool'] for entry in actions] == ['sequence', 'sequence', 'wait']
    assert all('gripper' in entry['state'] for entry in actions)
