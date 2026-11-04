from types import SimpleNamespace
import numpy as np
import pytest
from robotlab.core.goals import validate, evaluate


def context():
    poses = {'ball': [1, 0, .25], 'bin': [1, 0, .05]}
    return SimpleNamespace(
        model=None, data=SimpleNamespace(time=0.), held=lambda: None,
        object_speed=lambda _: 0., object_touching_robot=lambda _: False,
        robot=SimpleNamespace(base_xy=lambda *_: [0, 0], fallen=lambda *_: None),
        object_pose=lambda id: (np.array(poses[id]), np.array([1., 0, 0, 0])),
        objects=lambda: [{'id': 'ball', 'shape': 'sphere', 'size': [.3]}, {'id': 'bin', 'shape': 'box', 'size': [1, 1, .1]}])


def test_validation_lists_errors():
    assert len(validate({'type': 'object_near', 'object': 'missing', 'target': {'point': [1]}, 'radius': -1}, ['ball'])) == 3
    assert validate({'type': 'all', 'goals': []})
    assert validate({'type': 'object_on', 'stable_s': 0, 'object': 'ball', 'target': {'point': [0, 0]}})
    assert validate({'type': 'robot_near', 'target': {'robot_start': 1}, 'radius': 1})


def test_predicates():
    ctx = context()
    goals = [
        {'type': 'object_near', 'object': 'ball', 'target': {'object': 'bin'}, 'radius': .1},
        {'type': 'robot_near', 'target': {'robot_start': True}, 'radius': .1},
        {'type': 'object_on', 'stable_s': 0, 'object': 'ball', 'target': {'object': 'bin'}},
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
    goal = {'type': 'object_on', 'stable_s': 0, 'object': 'ball', 'target': {'object': 'bin'}}
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
    goal = {'type': 'object_on', 'stable_s': 0, 'object': 'ball', 'target': {'object': 'bin'}}
    assert evaluate(goal, ctx, [0, 0])['met'] is met


@pytest.mark.parametrize('grouped', [False, True])
@pytest.mark.parametrize('held', [None, 'ball'])
def test_container_details_describe_inside(grouped, held):
    ctx = context()
    ctx.world = SimpleNamespace(env={'objects': [dict(ctx.objects()[1], compound=True)]})
    ctx.held = lambda: held
    goal = {'type': 'object_on', 'stable_s': 0, 'object': 'ball', 'target': {'object': 'bin'}}
    if grouped:
        goal = {'type': 'all', 'goals': [goal]}
    met = held is None
    assert evaluate(goal, ctx, [0, 0]) == {
        'met': met, 'detail': ('goal met' if met else 'goal not met') + ': ball inside bin'}
    ctx.robot.fallen = lambda *_: 'fell'
    assert evaluate(goal, ctx, [0, 0]) == {'met': False, 'detail': 'fell'}


def test_ordinary_target_details_remain_unchanged():
    goal = {'type': 'object_on', 'stable_s': 0, 'object': 'ball', 'target': {'object': 'bin'}}
    assert evaluate(goal, context(), [0, 0]) == {'met': True, 'detail': 'goal met'}


@pytest.mark.parametrize('kind', ['object_on', 'robot_on'])
@pytest.mark.parametrize('value', [-1, True, '1', None, float('nan'), float('inf')])
def test_stability_validation(kind, value):
    goal = {'type': kind, 'target': {'object': 'bin'}, 'stable_s': value}
    if kind == 'object_on':
        goal['object'] = 'ball'
    assert any('stable_s' in e for e in validate(goal, ['ball', 'bin']))


@pytest.mark.parametrize('feet', [0, 5, True, 1.5, None])
def test_robot_on_feet_validation(feet):
    assert validate({'type': 'robot_on', 'target': {'object': 'bin'}, 'feet': feet}, ['bin'])


def test_robot_on_requires_object_target():
    assert validate({'type': 'robot_on', 'target': {'point': [0, 0]}})
    assert validate({'type': 'robot_on', 'target': {'object': 'missing'}}, ['bin'])
    assert validate({'type': 'robot_on', 'target': {'object': 'bin'}}, ['bin']) == []


def test_object_on_continuous_stability_and_rewind():
    ctx = context()
    goal = {'type': 'object_on', 'object': 'ball', 'target': {'object': 'bin'}}
    def at(t):
        ctx.data.time = t
        return evaluate(goal, ctx, [0, 0])['met']
    assert not at(0)
    assert not at(.999)
    assert at(1)
    # Wall time and repeated reads do not advance the stability timer.
    assert at(1)
    ctx.object_speed = lambda _: .02
    assert not at(1.1)
    ctx.object_speed = lambda _: .019
    assert not at(1.2)
    assert not at(2.199)
    assert at(2.2)
    ctx.object_touching_robot = lambda _: True
    assert not at(3)
    ctx.object_touching_robot = lambda _: False
    assert not at(3.1)
    assert at(4.1)
    ctx.held = lambda: 'ball'
    assert not at(4.2)
    ctx.held = lambda: None
    assert not at(4.3)
    assert at(5.3)
    assert not at(0)  # Simulator reset invalidates previous stability.


def test_object_on_top_tolerance_and_com_footprint():
    ctx = context()
    goal = {'type': 'object_on', 'object': 'ball', 'target': {'object': 'bin'}, 'stable_s': 0}
    def placed(x, z):
        ctx.object_pose = lambda oid: (np.array([x, 0, z] if oid == 'ball' else [1, 0, .05]),
                                      np.array([1., 0, 0, 0]))
        return evaluate(goal, ctx, [0, 0])['met']
    assert placed(1, .26)
    assert not placed(1, .2601)
    assert not placed(1, .2399)
    assert placed(1.49, .25)
    assert not placed(1.501, .25)


def test_all_evaluates_stability_even_when_previous_child_is_false():
    ctx = context()
    goal = {'type': 'all', 'goals': [
        {'type': 'object_held', 'object': 'bin'},
        {'type': 'object_on', 'object': 'ball', 'target': {'object': 'bin'}},
    ]}
    assert not evaluate(goal, ctx, [0, 0])['met']
    ctx.data.time = 1
    ctx.held = lambda: 'bin'
    assert evaluate(goal, ctx, [0, 0])['met']


def test_rotated_box_uses_actual_bottom_face():
    ctx = context()
    ctx.objects = lambda: [
        {'id': 'ball', 'shape': 'box', 'size': [.1, .1, .1]},
        {'id': 'bin', 'shape': 'box', 'size': [1, 1, .1]},
    ]
    angle = np.deg2rad(45) / 2
    ctx.object_pose = lambda oid: (np.array([1, 0, .15] if oid == 'ball' else [1, 0, .05]),
                                   np.array([np.cos(angle), np.sin(angle), 0, 0]) if oid == 'ball'
                                   else np.array([1., 0, 0, 0]))
    goal = {'type': 'object_on', 'object': 'ball', 'target': {'object': 'bin'}, 'stable_s': 0}
    assert not evaluate(goal, ctx, [0, 0])['met']
