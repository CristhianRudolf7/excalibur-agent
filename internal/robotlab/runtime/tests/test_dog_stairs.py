"""Test-only scripted keyframes proving the narrow stairs are physically climbable."""
import json
from pathlib import Path

import numpy as np
import pytest

from robotlab.assets import menagerie_dir
from robotlab.robots.dog import DogRobot
from robotlab.robots.testing import SimpleContext
from robotlab.world.builder import build_world


def stairs(production=False):
    path = Path(__file__).parents[1]/'robotlab/world/defaults/escalera-estrecha.json'
    robot = DogRobot()
    world = build_world(json.loads(path.read_text()), robot, menagerie_dir())
    if production:
        from robotlab.core.context import ToolContext
        ctx = ToolContext(world)
    else:
        ctx = SimpleContext(robot, world.model, world.data)
    return robot, world.model, world.data, ctx


def height(x):
    return 0. if x < 0 else .12 if x < .3 else .24 if x < .6 else .36


def climb_plan(act, *, slower=False):
    """Test-only public-call planner shared with the sidecar integration proof."""
    result = act('wait', seconds=.5)
    feet = {leg: np.array(info['foot_world']) for leg, info in result['state']['legs'].items()}
    calls, keyframes = 1, 0
    # This planner exists only in the test. Each cycle advances one foot at a time,
    # explicitly shifts the COM, lifts, transfers and lowers using public tools.
    for cycle in range(15):
        for leg in ('FL', 'RR', 'FR', 'RL'):
            allfeet = np.array(list(feet.values()))
            center = allfeet.mean(axis=0)
            center[1] = -.065 if leg.endswith('L') else .065
            slope = np.linalg.lstsq(np.column_stack((allfeet[:, 0], np.ones(4))), allfeet[:, 2], rcond=None)[0]
            trunk = dict(x=float(center[0]+.002), y=float(center[1]),
                         z=float(slope[0]*center[0]+slope[1]+.25),
                         pitch=float(-np.degrees(np.arctan(slope[0]))))
            old = feet[leg].copy()
            dest = old.copy()
            dest[0] += .1
            # Avoid landing on the riser edge: reserve 4 cm longitudinal clearance.
            for edge in (0., .3, .6):
                if edge-.04 < dest[0] < edge+.04:
                    dest[0] = edge+.055
            dest[2] = height(dest[0])+.009
            up = old.copy()
            up[2] = max(old[2], dest[2])+.065
            across = dest.copy()
            across[2] = up[2]
            plan = [dict(type='feet', feet={}, trunk=trunk, duration=.8),
                    dict(type='feet', feet={leg: up.tolist()}, duration=.4),
                    dict(type='feet', feet={leg: across.tolist()}, duration=.6),
                    dict(type='feet', feet={leg: dest.tolist()}, duration=.4)]
            if slower:
                # A second timing verifies the same geometric plan is not tied
                # to one exact contact-solver trajectory.
                for key in plan:
                    key['duration'] *= 1.1
            result = act('sequence', keyframes=plan, settle=.2)
            calls += 1
            keyframes += len(plan)
            if not result['ok']:
                print('REJECTED', cycle, leg, trunk, json.dumps(result.get('details')), flush=True)
            assert result['ok'], (cycle, leg, trunk, result.get('details'), result['state'])
            feet[leg] = dest
            assert abs(result['state']['roll_deg']) < 3
            if result.get('goal', {}).get('met'):
                return result, keyframes, calls
        if all(p[0] > .66 for p in feet.values()):
            break
    result = act('wait', seconds=1)
    calls += 1
    return result, keyframes, calls


@pytest.mark.parametrize('production', [False, True], ids=['minimal-context', 'production-context'])
def test_climb_three_steps(production):
    robot, model, data, ctx = stairs(production)

    def act(tool, **args):
        if production:
            ctx.begin_action()
        return robot.tools()[tool].fn(ctx, **args)

    result, keyframes, calls = climb_plan(act, slower=production)
    target = model.geom('obj/descanso').id
    assert all(target in ids for ids in robot.feet_contacts(model, data).values())
    assert robot.fallen(model, data) is None
    assert (keyframes, calls) == (224, 58)
    print('climb keyframes/calls', keyframes, calls, 'sim seconds', round(data.time, 2))


def test_careless_lift_outside_support_triangle_falls():
    from robotlab.robots.base import ActionInterrupted
    robot, model, data, ctx = stairs()
    result = robot.wait(ctx, .5)
    # The front-left foot is about to be lifted. Leaning LEFT puts the COM
    # outside the opposite three-foot triangle, rather than safely to the right.
    result = robot.move_feet(ctx, {}, trunk={'y': .07}, duration=1)
    assert result['ok'], result
    foot = result['state']['legs']['FL']['foot_world']
    with pytest.raises(ActionInterrupted) as exc:
        result = robot.move_feet(ctx, {'FL': [foot[0], foot[1], foot[2]+.10]}, duration=.7)
        assert result['ok'], result
        robot.wait(ctx, 5)
    assert exc.value.reason == 'fell'
    assert robot.fallen(model, data)
