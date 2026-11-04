"""Stable goals tested with simulator contacts and deterministic robot sensors."""
import mujoco
import numpy as np
import pytest

from robotlab.core.context import ToolContext
from robotlab.core.goals import evaluate
from robotlab.robots.base import ActionInterrupted
from robotlab.world.builder import build_world
from tests.fake_robot import FakeRobot


def supported_world():
    robot = FakeRobot()
    world = build_world({'id': 'support-test', 'wall_height': 0, 'objects': [
        {'id': 'support', 'shape': 'box', 'size': [.6, .6, .1], 'pos': [1, 0], 'dynamic': False},
        {'id': 'payload', 'shape': 'box', 'size': [.1]*3, 'pos': [1, 0, .15]},
    ]}, robot, '')
    return world


def test_object_on_settles_for_default_second_before_interrupting():
    world = supported_world()
    ctx = ToolContext(world, goal={'type': 'object_on', 'object': 'payload',
                                  'target': {'object': 'support'}})
    assert not ctx.goal_result()['met']
    ctx.step(.9)
    assert not ctx.goal_result()['met']
    with pytest.raises(ActionInterrupted) as exc:
        ctx.step(.3)
    assert exc.value.reason == 'goal'
    assert 1 <= world.data.time < 1.2
    assert ctx.object_speed('payload') < .02


def test_raw_robot_contacts_prevent_object_on():
    world = supported_world()
    ctx = ToolContext(world)
    geom = world.model.geom('obj/payload').id
    robot_geom = next(iter(ctx.contacts.robot_bodies))
    assert not ctx.object_touching_robot('payload')
    contact = mujoco.MjContact()
    contact.geom1, contact.geom2 = geom, robot_geom
    mujoco.mj_addContact(world.model, world.data, contact)
    assert ctx.object_touching_robot('payload')
    goal = {'type': 'object_on', 'object': 'payload', 'target': {'object': 'support'}, 'stable_s': 0}
    assert not evaluate(goal, ctx, [0, 0])['met']


def test_robot_on_counts_distinct_feet_and_resets_after_loss_or_fall():
    world = supported_world()
    ctx = ToolContext(world)
    geom = world.model.geom('obj/support').id
    other = world.model.geom('obj/payload').id
    contacts = {'FL': [geom, geom], 'FR': [geom], 'RL': [geom], 'RR': [other]}
    world.robot.feet_contacts = lambda *_: contacts
    goal = {'type': 'robot_on', 'target': {'object': 'support'}, 'feet': 4}
    def at(t):
        world.data.time = t
        return evaluate(goal, ctx, [0, 0])['met']
    assert not at(0)
    assert not at(2)
    contacts['RR'] = [geom]
    assert not at(2.1)
    assert not at(3)
    assert at(3.1)
    contacts['FL'] = []
    assert not at(3.2)
    contacts['FL'] = [geom]
    assert not at(3.3)
    world.robot.bad = True
    assert not at(4.3)
    world.robot.bad = False
    assert not at(4.4)
    assert at(5.4)
    goal['feet'] = 3
    contacts['FL'] = []
    assert not at(5.5)
    assert at(6.5)
