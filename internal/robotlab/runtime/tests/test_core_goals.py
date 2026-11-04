from types import SimpleNamespace
import numpy as np
import pytest
from robotlab.core.goals import validate, evaluate


def context():
    poses = {'ball': [1, 0, .25], 'bin': [1, 0, .05]}
    return SimpleNamespace(
        model=None, data=None, held=lambda: None,
        robot=SimpleNamespace(base_xy=lambda *_: [0, 0], fallen=lambda *_: None),
        object_pose=lambda id: (np.array(poses[id]), np.array([1., 0, 0, 0])),
        objects=lambda: [{'id': 'ball', 'shape': 'sphere', 'size': [.3]}, {'id': 'bin', 'shape': 'box', 'size': [1, 1, .1]}])


def test_validation_lists_errors():
    assert len(validate({'type': 'object_near', 'object': 'missing', 'target': {'point': [1]}, 'radius': -1}, ['ball'])) == 3
    assert validate({'type': 'all', 'goals': []})
    assert validate({'type': 'object_on', 'object': 'ball', 'target': {'point': [0, 0]}})
    assert validate({'type': 'robot_near', 'target': {'robot_start': 1}, 'radius': 1})


def test_predicates():
    ctx = context()
    goals = [
        {'type': 'object_near', 'object': 'ball', 'target': {'object': 'bin'}, 'radius': .1},
        {'type': 'robot_near', 'target': {'robot_start': True}, 'radius': .1},
        {'type': 'object_on', 'object': 'ball', 'target': {'object': 'bin'}},
    ]
    assert evaluate({'type': 'all', 'goals': goals}, ctx, [0, 0])['met']
    assert not evaluate({'type': 'object_held', 'object': 'ball'}, ctx, [0, 0])['met']
    ctx.held = lambda: 'ball'
    assert evaluate({'type': 'object_held', 'object': 'ball'}, ctx, [0, 0])['met']
    ctx.robot.fallen = lambda *_: 'fell'
    assert not evaluate(goals[1], ctx, [0, 0])['met']


@pytest.mark.parametrize('marker', [{'compound': True}, {'tags': ['bin']}, {'tags': ['container']}])
def test_object_on_container_metadata_from_environment(marker):
    ctx = context()
    # Runtime object summaries omit tags and compound; the environment owns them.
    ctx.world = SimpleNamespace(env={'objects': [dict(ctx.objects()[1], **marker)]})
    ctx.object_pose = lambda oid: (np.array([1, 0, .16] if oid == 'ball' else [1, 0, .05]),
                                   np.array([1., 0, 0, 0]))
    goal = {'type': 'object_on', 'object': 'ball', 'target': {'object': 'bin'}}
    assert evaluate(goal, ctx, [0, 0])['met']
    ctx.held = lambda: 'ball'
    assert not evaluate(goal, ctx, [0, 0])['met']


@pytest.mark.parametrize(('local_xy', 'bottom', 'met'), [
    ([0, 0], -.01, True),       # Base top .01 minus .02 tolerance.
    ([0, 0], -.011, False),
    ([0, 0], .14, True),        # Wall top .10 plus .04 tolerance.
    ([0, 0], .141, False),
    ([.489, 0], .01, True),
    ([.49, 0], .01, True),
    ([.491, 0], .01, False),    # Center in a wall, inside the outer footprint.
    ([0, .239], .01, True),
    ([0, .24], .01, True),
    ([0, .241], .01, False),
    ([.6, 0], .01, False),
])
def test_container_inner_bounds_are_yaw_aware(local_xy, bottom, met):
    ctx = context()
    ctx.objects = lambda: [
        {'id': 'ball', 'shape': 'sphere', 'size': [.3]},
        {'id': 'bin', 'shape': 'box', 'size': [1, .5, .1], 'compound': True},
    ]
    yaw = np.deg2rad(37)
    c, s = np.cos(yaw), np.sin(yaw)
    x, y = local_xy
    poses = {'ball': [1 + c*x - s*y, s*x + c*y, bottom + .15], 'bin': [1, 0, .05]}
    ctx.object_pose = lambda oid: (np.array(poses[oid]),
                                   np.array([np.cos(yaw / 2), 0, 0, np.sin(yaw / 2)]))
    goal = {'type': 'object_on', 'object': 'ball', 'target': {'object': 'bin'}}
    assert evaluate(goal, ctx, [0, 0])['met'] is met


@pytest.mark.parametrize('grouped', [False, True])
@pytest.mark.parametrize('held', [None, 'ball'])
def test_container_details_describe_inside(grouped, held):
    ctx = context()
    ctx.world = SimpleNamespace(env={'objects': [dict(ctx.objects()[1], compound=True)]})
    ctx.held = lambda: held
    goal = {'type': 'object_on', 'object': 'ball', 'target': {'object': 'bin'}}
    if grouped:
        goal = {'type': 'all', 'goals': [goal]}
    met = held is None
    assert evaluate(goal, ctx, [0, 0]) == {
        'met': met, 'detail': ('goal met' if met else 'goal not met') + ': ball inside bin'}
    ctx.robot.fallen = lambda *_: 'fell'
    assert evaluate(goal, ctx, [0, 0]) == {'met': False, 'detail': 'fell'}


def test_ordinary_target_details_remain_unchanged():
    goal = {'type': 'object_on', 'object': 'ball', 'target': {'object': 'bin'}}
    assert evaluate(goal, context(), [0, 0]) == {'met': True, 'detail': 'goal met'}
