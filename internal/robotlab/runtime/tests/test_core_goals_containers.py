"""Container goals against a tabletop world with a fake robot."""
import json
from pathlib import Path

import mujoco
import numpy as np
import pytest

from robotlab.assets import menagerie_dir
from robotlab.core.context import ToolContext
from tests.fake_robot import FakeRobot
from robotlab.robots.base import ActionInterrupted
from robotlab.world import builder


GOAL = {'type': 'object_on', 'object': 'cubo-rojo', 'target': {'object': 'caja'}, 'stable_s': 0}


@pytest.fixture(params=[0, 57], ids=['straight', 'yawed'])
def tabletop(request):
    def build(*, container=True):
        env = {'id': 'container-test', 'kind': 'table', 'objects': [
            {'id': 'cubo-rojo', 'shape': 'box', 'size': [.05]*3, 'pos': [-.3, -.12]},
            {'id': 'caja', 'shape': 'box', 'size': [.22, .2, .12], 'pos': [.33, .12],
             'dynamic': False, 'compound': True, 'tags': ['container']},
        ]}
        target = next(o for o in env['objects'] if o['id'] == 'caja')
        target['yaw'] = request.param
        if not container:
            target.pop('compound')
            target.pop('tags')
        robot = FakeRobot()
        robot.world_kind = 'table'
        world = builder.build_world(env, robot, '')
        return ToolContext(world)
    return build


def place_cube(ctx, position):
    body = ctx.world.object_body('cubo-rojo')
    joint = ctx.model.body_jntadr[body]
    adr, vadr = ctx.model.jnt_qposadr[joint], ctx.model.jnt_dofadr[joint]
    ctx.data.qpos[adr:adr + 7] = [*position, 1, 0, 0, 0]
    ctx.data.qvel[vadr:vadr + 6] = 0
    mujoco.mj_forward(ctx.model, ctx.data)


def test_cube_dropped_inside_bin_meets_goal_and_interrupts(tabletop):
    ctx = tabletop()
    target, _ = ctx.object_pose('caja')
    place_cube(ctx, [*target[:2], .90])
    ctx.step(.5)
    cube, _ = ctx.object_pose('cubo-rojo')
    base = ctx.model.geom('obj/caja')
    base_top = ctx.data.geom_xpos[base.id, 2] + base.size[2]
    assert cube[2] - .025 == pytest.approx(base_top, abs=.002)
    assert cube[2] < target[2] + .06  # Resting below the wall tops.
    assert not {'compound', 'tags'} & ctx.objects()[-1].keys()
    ctx.goal = GOAL
    assert ctx.goal_result() == {'met': True, 'detail': 'goal met: cubo-rojo inside caja'}
    with pytest.raises(ActionInterrupted) as interrupted:
        ctx.step(.002)
    assert interrupted.value.reason == 'goal'


def test_cube_on_table_next_to_bin_is_not_inside(tabletop):
    ctx = tabletop()
    target, _ = ctx.object_pose('caja')
    place_cube(ctx, [target[0] - .3, target[1], .80])
    ctx.step(.5)
    cube, _ = ctx.object_pose('cubo-rojo')
    assert cube[2] - .025 == pytest.approx(.75, abs=.002)
    ctx.goal = GOAL
    assert ctx.goal_result() == {'met': False, 'detail': 'goal not met: cubo-rojo inside caja'}
    ctx.step(.002)


@pytest.mark.parametrize('height', [.80, 1.05], ids=['within-walls', 'above-bin'])
def test_held_cube_cannot_meet_container_goal(tabletop, height):
    ctx = tabletop()
    target, _ = ctx.object_pose('caja')
    place_cube(ctx, [*target[:2], height])
    ctx.hold('cubo-rojo', 'robot/grasp')
    ctx.goal = GOAL
    assert ctx.held() == 'cubo-rojo'
    assert not ctx.goal_result()['met']
    ctx.step(.002)
    assert not ctx.goal_result()['met']
    if height == .80:
        ctx.release()
        assert ctx.goal_result()['met']  # This geometry-only case opts into stable_s=0.


def test_ordinary_box_keeps_top_face_rule(tabletop):
    ctx = tabletop(container=False)
    target, _ = ctx.object_pose('caja')
    ctx.goal = GOAL
    place_cube(ctx, [*target[:2], .785])
    assert not ctx.goal_result()['met']  # Interior height does not count.
    place_cube(ctx, [*target[:2], target[2] + .06 + .025])
    assert ctx.goal_result() == {'met': True, 'detail': 'goal met'}
    ctx.goal = None
    ctx.step(.5)
    cube, _ = ctx.object_pose('cubo-rojo')
    np.testing.assert_allclose(cube[:2], target[:2], atol=.002)
    assert cube[2] - .025 == pytest.approx(target[2] + .06, abs=.002)
    ctx.goal = GOAL
    assert ctx.goal_result()['met']
