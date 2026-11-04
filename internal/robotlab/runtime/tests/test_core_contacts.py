"""Contact evidence uses real constraint forces, including unnamed robot geoms."""
import json

import mujoco
import pytest

from robotlab.core.context import ToolContext
from robotlab.core.environments import Environments
from robotlab.core.session import Sessions
from robotlab.core.settings import Settings
from robotlab.robots.base import ActionInterrupted
from robotlab.world.builder import build_world
from tests.fake_robot import FakeRobot


class ContactRobot(FakeRobot):
    def attach(self, spec, spawn, menagerie):
        super().attach(spec, spawn, menagerie)
        spec.body('robot/base').geoms[0].name = 'robot/chassis'


def contact_world(*, wall=True, robot=None):
    return build_world({
        'version': 1, 'id': 'contact-room', 'name': 'Contact room', 'kind': 'room',
        'size': [3, 2], 'wall_height': 0,
        'walls': [{'from': [.3, -1], 'to': [.3, 1], 'height': 1, 'thickness': .1}] if wall else [],
        'objects': [], 'spawn': {'x': 0, 'y': 0, 'yaw': 0},
    }, robot or ContactRobot(), '/unused')


def test_contacts_keep_recent_pairs_forces_and_action_history():
    world = contact_world()
    ctx = ToolContext(world)
    world.robot.push(ctx, dx=.5)
    contacts = ctx.contacts.recent()
    assert len(contacts) == 1
    wall = contacts[0]
    assert wall['env_geom'] == wall['env_name'] == 'wall/0'
    assert wall['robot_body'] == 'robot/base'
    assert 0 < wall['first_time'] < wall['last_time'] <= world.data.time
    assert wall['max_normal_force'] > 0
    assert ctx.contacts.during_action() == contacts
    # An action reset keeps recent evidence from the preceding tool call.
    ctx.begin_action()
    assert ctx.contacts.recent() == contacts
    assert ctx.contacts.during_action() == []
    world.robot.push(ctx, dx=-.5, duration=2)
    assert ctx.contacts.recent() == []
    assert ctx.contacts.during_action()  # A brief touch at the start still counts.
    assert all(t >= world.data.time - 1.5 - 1e-9 for t, _ in ctx.contacts.history)


def test_unnamed_robot_geoms_are_identified_by_body_namespace():
    world = contact_world(robot=FakeRobot())
    ctx = ToolContext(world)
    world.robot.push(ctx, dx=.5)
    assert ctx.contacts.recent()[0]['robot_body'] == 'robot/base'


def test_floor_and_robot_self_contacts_are_excluded():
    class GroundRobot(ContactRobot):
        def attach(self, spec, spawn, menagerie):
            super().attach(spec, spawn, menagerie)
            spec.body('robot/base').pos[2] = .09
            other = spec.worldbody.add_body(name='robot/other', pos=[0, 0, .09])
            other.add_geom(name='robot/other', type=mujoco.mjtGeom.mjGEOM_BOX,
                           size=[.1, .1, .1])

    world = contact_world(wall=False, robot=GroundRobot())
    ctx = ToolContext(world)
    ctx.step(.1)
    assert world.data.ncon > 0
    assert ctx.contacts.recent() == ctx.contacts.during_action() == []


@pytest.fixture
def sessions(tmp_path, monkeypatch):
    from robotlab import robots

    monkeypatch.setattr(robots, 'make', lambda _: ContactRobot())
    envs = Environments(tmp_path)
    envs.save(contact_world().env)
    sessions = Sessions(tmp_path, Settings(tmp_path), envs)
    sid = sessions.start('dog', 'contact-room', 'Move past the wall',
                         {'type': 'robot_near', 'target': {'point': [1, 0]}, 'radius': .1})['session']['id']
    return sessions, sid


@pytest.mark.parametrize('kind', ['fell', 'safety', 'crashed'])
def test_failures_explain_wall_touch_and_preserve_evidence(sessions, kind):
    sessions, sid = sessions
    # A successful earlier action brushes the wall. The failing action moves
    # away, so evidence must survive begin_action and report the elapsed time.
    sessions.act(sid, 'push', {'dx': .5})

    def fail():
        sessions.world.robot.push(sessions.ctx, dx=-.3, duration=.4)
        raise ActionInterrupted(kind, 'trunk tilt exceeds 45 degrees')

    result = sessions.execute(sid, fail, {'tool': 'push', 'args': {'dx': -.3}})
    assert not result['ok'] and result['attempt']['status'] == 'failed'
    reason = result['attempt']['reason']
    assert reason.startswith(f'{kind}: trunk tilt exceeds 45 degrees; touched wall/0 with robot/base ')
    assert reason.endswith('0.4 s before')
    assert result['result']['error'] == reason
    contacts = result['result']['contacts']
    assert len(contacts) == 1
    assert contacts[0]['env_geom'] == 'wall/0'
    assert contacts[0]['first_time'] < 1
    assert contacts[0]['last_time'] < sessions.world.data.time
    folder = sessions.store.session_dir(sid) / 'attempts/1/failure'
    state = json.loads((folder / 'state.json').read_text())
    assert state['contacts'] == contacts
    assert state['fell_at'] == state['final']['time'] == sessions.world.data.time
    assert sessions.ctx.contacts.recent() == contacts  # Camera replay leaves evidence intact.


def test_failure_excludes_old_contacts_and_new_attempt_clears_history(sessions):
    sessions, sid = sessions
    sessions.act(sid, 'push', {'dx': .5})
    sessions.act(sid, 'push', {'dx': -.5, 'duration': 2})
    result = sessions.act(sid, 'push', {'dx': 0, 'fail': True})
    assert result['result']['contacts'] == []
    assert 'touched' not in result['attempt']['reason']
    state = json.loads((sessions.store.session_dir(sid) / 'attempts/1/failure/state.json').read_text())
    assert state['contacts'] == []
    sessions.reset(sid, 'Leave more clearance', no_skill_reason='Controlled evidence test')
    assert sessions.ctx.contacts.recent() == sessions.ctx.contacts.during_action() == []


def test_object_contact_uses_display_name():
    world = contact_world(wall=False)
    env = dict(world.env, objects=[{
        'id': 'barrier', 'name': 'Low barrier', 'shape': 'box', 'size': [.1, 1, .3],
        'pos': [.3, 0, .15], 'dynamic': False,
    }])
    world = build_world(env, ContactRobot(), '/unused')
    ctx = ToolContext(world)
    world.robot.push(ctx, dx=.5)
    contact = ctx.contacts.recent()[0]
    assert contact['env_geom'] == 'obj/barrier'
    assert contact['env_name'] == 'Low barrier'
    assert '; touched Low barrier with robot/base ' in ctx.contacts.describe_recent()


def test_successful_actions_report_only_their_own_contacts(sessions):
    sessions, sid = sessions
    untouched = sessions.act(sid, 'push', {'dx': 0})
    assert untouched['ok'] and untouched['result']['contacts'] == []
    brushed = sessions.act(sid, 'push', {'dx': .5})
    assert brushed['ok'] and brushed['attempt']['status'] == 'running'
    contacts = brushed['result']['contacts']
    assert len(contacts) == 1 and contacts[0]['env_geom'] == 'wall/0'
    # Early action contacts remain visible after they leave the rolling window.
    retreated = sessions.act(sid, 'push', {'dx': -.5, 'duration': 2})
    assert retreated['ok'] and retreated['result']['contacts']
    assert sessions.ctx.contacts.recent() == []
    assert retreated['result']['contacts'][0]['last_time'] < sessions.world.data.time - 1.5
    clear = sessions.act(sid, 'push', {'dx': 0})
    assert clear['ok'] and clear['result']['contacts'] == []
    assert brushed['result']['contacts'] == contacts  # Returned evidence owns its data.


def test_tool_unsuccessful_result_keeps_contacts_without_failing_attempt(sessions):
    sessions, sid = sessions

    def blocked():
        sessions.world.robot.push(sessions.ctx, dx=.5)
        return {'ok': False, 'error': 'blocked'}

    result = sessions.execute(sid, blocked, {'tool': 'push', 'args': {'dx': .5}})
    assert not result['ok'] and result['attempt']['status'] == 'running'
    assert result['result']['error'] == 'blocked'
    assert result['result']['contacts'][0]['env_geom'] == 'wall/0'


def test_rolling_window_recomputes_first_time_and_maximum_force(monkeypatch):
    world = contact_world()
    ctx = ToolContext(world)

    def measured_force(model, data, index, force):
        force[:] = [50 if data.time < .6 else 2, 1000, 0, 0, 0, 0]

    monkeypatch.setattr(mujoco, 'mj_contactForce', measured_force)
    world.robot.push(ctx, dx=.5)
    assert ctx.contacts.recent()[0]['max_normal_force'] == 50
    world.robot.push(ctx, dx=.2, duration=2)
    recent = ctx.contacts.recent()[0]
    assert recent['first_time'] == pytest.approx(world.data.time - 1.5,
                                               abs=world.model.opt.timestep + 1e-9)
    assert recent['max_normal_force'] == 2
    assert ctx.contacts.during_action()[0]['max_normal_force'] == 50
    assert ctx.contacts.during_action()[0]['first_time'] < recent['first_time']


def test_table_top_contacts_are_included():
    class TableRobot(ContactRobot):
        world_kind = 'table'

        def attach(self, spec, spawn, menagerie):
            super().attach(spec, spawn, menagerie)
            spec.body('robot/base').pos[2] = .8

    world = build_world({
        'version': 1, 'id': 'contact-table', 'name': 'Contact table', 'kind': 'table',
        'size': [1.2, .8], 'objects': [], 'spawn': {'x': 0, 'y': 0, 'yaw': 0},
    }, TableRobot(), '/unused')
    ctx = ToolContext(world)
    ctx.step(.1)
    contacts = ctx.contacts.recent()
    assert len(contacts) == 1
    assert contacts[0]['env_geom'] == 'table/top'
    assert contacts[0]['robot_body'] == 'robot/base'
    assert contacts[0]['max_normal_force'] > 0


def test_real_humanoid_wall_brush_explains_fall(tmp_path):
    # No obstacles: a narrow room puts the left hand near the side wall.
    # The production walking controller and collision solver produce the fall.
    envs = Environments(tmp_path)
    envs.save({
        'version': 1, 'id': 'narrow-room', 'name': 'Narrow room', 'kind': 'room',
        'size': [3, 1], 'wall_height': 1.5, 'objects': [],
        'spawn': {'x': 0, 'y': .25, 'yaw': 0},
    })
    sessions = Sessions(tmp_path, Settings(tmp_path), envs)
    sid = sessions.start('humanoid', 'narrow-room', 'Walk along the room',
                         {'type': 'robot_near', 'target': {'point': [1.4, 0]}, 'radius': .05})['session']['id']
    result = sessions.act(sid, 'walk', {'distance': 1, 'step_length': .12, 'step_time': .6})
    assert not result['ok'] and result['attempt']['status'] == 'failed'
    assert 'tilt' in result['attempt']['reason']
    assert 'touched wall/2 with robot/left_' in result['attempt']['reason']
    contacts = result['result']['contacts']
    wrist = next(c for c in contacts if c['robot_body'] == 'robot/left_wrist_yaw_link')
    assert wrist['env_geom'] == 'wall/2'
    assert wrist['max_normal_force'] > 0
    assert 0 <= sessions.world.data.time - wrist['last_time'] <= 1.5
    assert all(c['env_geom'] == 'wall/2' for c in contacts)
    assert all(c['first_time'] >= sessions.world.data.time - 1.5 - 1e-9 for c in contacts)
    folder = sessions.store.session_dir(sid) / 'attempts/1/failure'
    state = json.loads((folder / 'state.json').read_text())
    assert state['contacts'] == contacts
    assert state['env']['objects'] == []
    assert state['last_call']['tool'] == 'walk'
    assert (folder / 'image.png').read_bytes().startswith(b'\x89PNG')
    assert sessions.ctx.contacts.recent() == contacts
