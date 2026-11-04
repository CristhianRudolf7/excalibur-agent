"""Exercise grasp assist against physical objects, with independent free-joint indices."""
import numpy as np
import pytest

from robotlab.robots.humanoid import HumanoidRobot
from robotlab.robots.testing import SimpleContext, build


@pytest.mark.parametrize('side', ['left', 'right'])
def test_grasp_and_release(side):
    # Measure the settled hand, then place a small object on a pedestal there.
    probe = HumanoidRobot()
    pm, pd = build(probe)
    pc = SimpleContext(probe, pm, pd)
    probe.stand(pc)
    sid = pm.site('robot/' + side + '_grasp').id
    p = pd.site_xpos[sid].copy()
    print('grasp site', side, p)
    robot = HumanoidRobot()
    model, data = build(robot, [
        {'id': 'pedestal', 'shape': 'box', 'size': [.02, .02, (p[2] - .012) / 2],
         'pos': [p[0], p[1], (p[2] - .012) / 2], 'dynamic': False},
        {'id': 'ball', 'shape': 'sphere', 'size': [.012], 'pos': p.tolist(), 'mass': .02},
        {'id': 'far', 'shape': 'sphere', 'size': [.02], 'pos': [2, 2, .02]},
    ])
    ctx = SimpleContext(robot, model, data)
    robot.stand(ctx)
    print('ball pose', ctx.object_pose('ball')[0])
    closed = robot.hand(ctx, side, 'close')
    print('closed', closed)
    assert closed['holding'] == 'ball'
    assert ctx.held() == 'ball'
    assert ctx._held[1] == model.site('robot/' + side + '_grasp').id
    # Opening the other hand must leave this grasp intact.
    other = 'right' if side == 'left' else 'left'
    assert robot.hand(ctx, other, 'open')['holding'] == 'ball'
    assert robot.hand(ctx, side, 'open')['holding'] is None
    assert robot.holding_side is None
    assert robot.fallen(model, data) is None


def test_close_ignores_fixed_or_far_objects():
    robot = HumanoidRobot()
    model, data = build(robot, [{'id': 'far', 'shape': 'sphere', 'size': [.02], 'pos': [2, 0, .02]}])
    ctx = SimpleContext(robot, model, data)
    robot.stand(ctx)
    assert robot.hand(ctx, 'both', 'close')['holding'] is None
