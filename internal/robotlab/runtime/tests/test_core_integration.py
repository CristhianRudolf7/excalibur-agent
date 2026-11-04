"""Public runtime scenarios using installed models and real rigid-body physics."""
import base64
import io
import json
import math

import mujoco
import numpy as np
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
        return self.sid

    def act(self, tool, **args):
        result = self.call('robot.act', session=self.sid, tool=tool, args=args)
        assert result['ok'], (tool, args, result)
        return result

    def succeeded(self):
        session = self.call('session.get', session=self.sid)['session']
        assert session['status'] == session['attempts'][-1]['status'] == 'succeeded'
        assert session['outcome']['status'] == 'succeeded'
        frames = self.call('attempt.frames', session=self.sid, attempt=1, count=400)
        assert frames['total'] > 0
        assert frames['total'] == session['attempts'][0]['frames']
        assert frames['total'] == math.floor((session['attempts'][0]['sim_time'] + 1e-8) * 20)
        scene = self.call('session.scene', session=self.sid)['scene']
        assert all(len(frame['poses']) == 7 * len(scene['bodies']) for frame in frames['frames'])
        assert any(event['event'] == 'frame' for event in self.events)
        return scene


@pytest.fixture
def client(tmp_path):
    client = Client(tmp_path)
    yield client
    client.runtime.close()


def test_dog_room_observe_fetch_return_and_all_meshes(client):
    sid = client.start('dog', 'mi-cuarto', {'type': 'object_near', 'object': 'pelota',
                       'target': {'robot_start': True}, 'radius': .6})
    observation = client.call('robot.look', session=sid)
    assert base64.b64decode(observation['png']).startswith(b'\x89PNG')
    ball = next(obj for obj in observation['visible'] if obj['id'] == 'pelota')
    assert ball['pixels'] >= 30
    np.testing.assert_allclose(ball['position'], [1.65, -1.25, .03], atol=.07)
    client.act('walk_to', x=1.3, y=-1.25, speed=.6)
    client.act('crouch', height=.22)
    # Observe again near the object, then lower from an unobstructed approach.
    near = client.call('robot.look', session=sid, camera='robot/arm')
    assert near['state']['arm_stowed']
    client.act('arm_move_to', x=1.65, y=-1.25, z=.18)
    client.act('arm_move_to', x=1.65, y=-1.25, z=.03)
    grasp = client.act('close_gripper')
    assert grasp['result']['held'] == 'pelota'
    client.act('arm_stow')
    result = client.act('walk_to', x=-1.7, y=-1.2, speed=.6)
    assert result['goal']['met']
    scene = client.succeeded()
    keys = {geom['mesh'] for body in scene['bodies'] for geom in body['geoms'] if geom['type'] == 'mesh'}
    assert keys
    for key in keys:
        mesh = client.call('mesh.get', key=key)
        assert mesh['key'] == key and 0 < mesh['faces'] <= 15000
        assert len(base64.b64decode(mesh['positions'])) == mesh['vertices'] * 12
        assert len(base64.b64decode(mesh['indices'])) == mesh['faces'] * 12


def test_arm_table_pick_and_place(client):
    client.start('arm', 'mesa-figuras', {'type': 'object_on', 'object': 'cubo-rojo',
                                      'target': {'object': 'caja'}})
    client.act('move_to', x=-.3, y=-.12, z=.95)
    client.act('move_to', x=-.3, y=-.12, z=.785)
    client.act('close_gripper')
    assert client.runtime.sessions.ctx.held() == 'cubo-rojo'
    client.act('lift', dz=.25)
    client.act('move_to', x=.33, y=.12, z=1.05)
    client.act('move_to', x=.33, y=.12, z=.92)
    result = client.act('open_gripper')
    assert result['goal']['met']
    assert client.runtime.sessions.ctx.held() is None
    client.succeeded()


def test_drone_corridor_takeoff_flight_landing_and_goal(client):
    client.start('drone', 'pasillo', {'type': 'robot_near', 'target': {'object': 'meta'}, 'radius': .3})
    client.act('takeoff', height=.8)
    client.act('fly_to', x=2.1, y=0, z=.8, speed=1.2)
    landed = client.act('land')
    assert landed['result']['stopped'] == 'motors off on contact'
    assert landed['attempt']['status'] == 'running'
    # Horizontal goals freeze physics immediately, so verify landing at a
    # staging waypoint before the final flight enters the goal radius.
    client.act('takeoff', height=.8)
    result = client.act('fly_to', x=3.1, y=0, z=.8)
    assert result['goal']['met']
    client.succeeded()


def write_skill(client, code, failures=None):
    return client.call('skills.write', session=client.sid, name='safe-stance', description='Maintain stance',
                       instructions='Stand and observe before moving.', code=code, failures=failures or [])


def failure_snapshot(client):
    folder = client.runtime.sessions.store.session_dir(client.sid) / 'attempts/1/failure'
    assert {p.name for p in folder.iterdir()} == {'state.json', 'image.png', 'image_fall.png', 'code.py'}
    assert (folder / 'image.png').read_bytes().startswith(b'\x89PNG')
    snapshot = json.loads((folder / 'state.json').read_text())
    assert snapshot['env']['id'] == 'mi-cuarto' and snapshot['robot'] == 'dog'
    return snapshot


def test_real_fall_evidence_regression_and_attempt_reset(client):
    client.start('dog', 'mi-cuarto', {'type': 'object_held', 'object': 'pelota'})
    world = client.runtime.sessions.world
    angle = math.radians(55)
    # Test hook changes only the real root attitude; detection and evidence use
    # the production controller, renderer, world, and skill replay implementation.
    world.data.qpos[world.robot.rq + 3:world.robot.rq + 7] = [math.cos(angle/2), math.sin(angle/2), 0, 0]
    mujoco.mj_forward(world.model, world.data)
    result = client.call('robot.act', session=client.sid, tool='stand', args={})
    assert not result['ok'] and result['attempt']['status'] == 'failed'
    assert 'tilt' in result['attempt']['reason']
    snapshot = failure_snapshot(client)
    assert snapshot['last_call'] == {'tool': 'stand', 'args': {}}
    with pytest.raises(RPCError, match='attempt 1 ended'):
        client.call('robot.act', session=client.sid, tool='stand', args={})
    candidate = write_skill(client, 'def run(robot, **args):\n    robot.stand()\n', [1])
    assert candidate['accepted']  # Equal zero pass counts satisfy the contract.
    assert candidate['regression']['total'] == 1
    assert candidate['regression']['old_passed'] == candidate['regression']['new_passed'] == 0
    assert 'fell' in candidate['regression']['details'][0]['new']['detail']
    archive = client.runtime.skills.folder(client.sid, 'safe-stance') / 'fallos' / (client.sid + '-1')
    assert json.loads((archive / 'state.json').read_text()) == snapshot
    # The isolated replay must not overwrite the frozen live world.
    np.testing.assert_array_equal(world.data.qpos, snapshot['qpos'])
    reset = client.call('attempt.reset', session=client.sid, reflection='Avoid large trunk tilt.')
    assert reset['attempt'] == 2 and reset['session']['attempts'][1]['status'] == 'running'
    assert client.act('stand')['attempt']['n'] == 2


def test_real_skill_regression_accepts_improvement_rejects_breakage(client):
    client.start('dog', 'mi-cuarto', {'type': 'object_held', 'object': 'pelota'})
    write_skill(client, 'def run(robot, **args):\n    return 1 / 0\n')
    failed = client.call('skills.run', session=client.sid, name='safe-stance')
    assert not failed['ok'] and failed['attempt']['status'] == 'failed'
    snapshot = failure_snapshot(client)
    assert snapshot['last_call']['skill'] == 'safe-stance'
    good_code = 'def run(robot, **args):\n    robot.stand()\n'
    improved = write_skill(client, good_code, [1])
    assert improved['accepted']
    assert improved['regression']['old_passed'] == 0
    assert improved['regression']['new_passed'] == improved['regression']['total'] == 1
    rejected = write_skill(client, 'def run(robot, **args):\n    robot.unknown_tool()\n')
    assert not rejected['accepted']
    assert rejected['regression']['old_passed'] == 1 and rejected['regression']['new_passed'] == 0
    assert rejected['skill']['code'] == good_code
    assert rejected['skill']['versions'][-1]['accepted'] is False
    np.testing.assert_array_equal(client.runtime.sessions.world.data.qpos, snapshot['qpos'])
    assert client.call('attempt.reset', session=client.sid, reflection='Use the verified stance skill.')['attempt'] == 2
    assert client.call('skills.run', session=client.sid, name='safe-stance')['ok']


def test_humanoid_shorter_steps_recover_from_prefall_regression(client, monkeypatch):
    from robotlab.core import perception

    rendered = []
    look = perception.look
    def observe(world, **options):
        rendered.append((float(world.data.time), world.data.qpos.copy()))
        return look(world, **options)
    monkeypatch.setattr(perception, 'look', observe)
    client.start('humanoid', 'pasillo', {'type': 'robot_near', 'target': {'point': [3.5, 0]}, 'radius': .1})
    code = 'def run(robot, **args):\n    robot.walk(distance=1, step_length=.35, step_time=.3)\n'
    client.call('skills.write', session=client.sid, name='walking', description='Walk steadily',
                instructions='Use controlled foot placement.', code=code)
    failed = client.call('skills.run', session=client.sid, name='walking')
    assert not failed['ok'] and failed['attempt']['status'] == 'failed'
    folder = client.runtime.sessions.store.session_dir(client.sid) / 'attempts/1/failure'
    snapshot = json.loads((folder / 'state.json').read_text())
    assert snapshot['robot'] == 'humanoid'
    assert snapshot['time'] == pytest.approx(max(0, snapshot['fell_at'] - 1.5), abs=.026)
    assert snapshot['final']['time'] == snapshot['fell_at']
    assert 'target' in snapshot['controller'] and 'feet' in snapshot['controller']
    assert snapshot['qpos'] != snapshot['final']['qpos']
    assert [pose[0] for pose in rendered] == [snapshot['time'], snapshot['fell_at']]
    np.testing.assert_array_equal(rendered[0][1], snapshot['qpos'])
    np.testing.assert_array_equal(rendered[1][1], snapshot['final']['qpos'])
    for image in ('image.png', 'image_fall.png'):
        assert (folder / image).read_bytes().startswith(b'\x89PNG')
    assert (folder / 'image.png').read_bytes() != (folder / 'image_fall.png').read_bytes()
    final_qpos = client.runtime.sessions.world.data.qpos.copy()
    np.testing.assert_array_equal(final_qpos, snapshot['final']['qpos'])
    improved = client.call('skills.write', session=client.sid, name='walking', description='Walk steadily',
                           instructions='Use shorter steps.', failures=[1],
                           code='def run(robot, **args):\n    robot.walk(distance=.4, step_length=.08, step_time=.6)\n')
    print('humanoid pre-fall regression:', snapshot['time'], snapshot['fell_at'], improved['regression'])
    assert improved['accepted']
    assert improved['regression']['old_passed'] == 0
    assert improved['regression']['new_passed'] == improved['regression']['total'] == 1
    np.testing.assert_array_equal(client.runtime.sessions.world.data.qpos, final_qpos)
