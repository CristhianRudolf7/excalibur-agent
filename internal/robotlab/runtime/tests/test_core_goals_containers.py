"""Container goal regressions against the actual tabletop world and arm."""
import json
from pathlib import Path

import mujoco
import numpy as np
import pytest

from robotlab.assets import menagerie_dir
from robotlab.core.context import ToolContext
from robotlab.robots.arm import ArmRobot
from robotlab.robots.base import ActionInterrupted
from robotlab.world import builder


GOAL = {'type': 'object_on', 'object': 'cubo-rojo', 'target': {'object': 'caja'}}


@pytest.fixture(params=[0, 57], ids=['straight', 'yawed'])
def tabletop(request):
    def build(*, container=True):
        path = Path(builder.__file__).with_name('defaults') / 'mesa-figuras.json'
        env = json.loads(path.read_text())
        target = next(o for o in env['objects'] if o['id'] == 'caja')
        target['yaw'] = request.param
        if not container:
            target.pop('compound')
            target.pop('tags')
        world = builder.build_world(env, ArmRobot(), menagerie_dir())
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
        assert ctx.goal_result()['met']


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
