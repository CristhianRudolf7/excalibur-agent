import threading
import time

import mujoco
import numpy as np
import pytest
from robotlab.core.context import ToolContext
from robotlab.robots.base import ActionInterrupted


def test_post_step_observations_match_full_forward(world):
    ctx = ToolContext(world)
    world.robot.push(ctx, dx=.3)
    expected = mujoco.MjData(world.model)
    mujoco.mj_copyData(expected, world.model, world.data)
    mujoco.mj_forward(world.model, expected)
    for field in ('xpos', 'xquat', 'site_xpos', 'cam_xpos', 'cam_xmat', 'cvel'):
        np.testing.assert_allclose(getattr(world.data, field), getattr(expected, field), atol=1e-12)
    assert world.data.ncon == expected.ncon
    np.testing.assert_array_equal(world.data.contact.geom, expected.contact.geom)
    np.testing.assert_allclose(world.data.contact.dist, expected.contact.dist, atol=1e-12)


def test_recording_control_freeze_and_hold(world):
    frames, live = [], []
    ctx = ToolContext(world, record=frames.append, event=lambda *e: live.append((time.monotonic(), e)))
    frozen = world.data.time
    assert world.data.time == frozen
    ctx.hold('box', 'robot/grasp')
    old, _ = ctx.object_pose('box')
    world.robot.push(ctx, dx=.5)
    new, _ = ctx.object_pose('box')
    assert new[0] - old[0] == pytest.approx(.5, abs=.02)
    assert np.linalg.norm(world.data.qvel[:6]) < 1e-8
    assert len(frames) == 20
    assert frames[-1]['t'] == 1
    assert all(b[0] - a[0] >= .05 for a, b in zip(live, live[1:]))
    assert ctx.release() == 'box'
    assert ctx.held() is None


def test_goal_safety_fall_stop_timeout(world):
    ctx = ToolContext(world, goal={'type': 'robot_near', 'target': {'point': [.1, 0]}, 'radius': .03})
    with pytest.raises(ActionInterrupted) as e:
        world.robot.push(ctx, dx=1)
    assert e.value.reason == 'goal'
    ctx.goal = None
    for option, reason in [('fail', 'fell'), ('unsafe', 'safety')]:
        with pytest.raises(ActionInterrupted) as e:
            world.robot.push(ctx, dx=0, **{option: True})
        assert e.value.reason == reason
    world.robot.bad = world.robot.unsafe = False
    ctx.begin_action()
    before = world.data.time
    with pytest.raises(ActionInterrupted) as e:
        ctx.step(61)
    assert e.value.reason == 'timeout'
    assert world.data.time - before == pytest.approx(60)
    ctx.stop.set()
    with pytest.raises(ActionInterrupted, match='stopped'):
        ctx.step(1)


def test_wall_pacing_and_prompt_stop(world, monkeypatch):
    monkeypatch.delenv('EXCALIBUR_ROBOTLAB_FAST')
    ctx = ToolContext(world, speed=2)
    start = time.monotonic()
    ctx.step(.2)
    assert .08 <= time.monotonic() - start < .4
    ctx.begin_action()
    timer = threading.Timer(.05, ctx.stop.set)
    timer.start()
    try:
        start = time.monotonic()
        with pytest.raises(ActionInterrupted, match='stopped'):
            ctx.step(20)
        assert time.monotonic() - start < .5
    finally:
        timer.join()


def test_pacing_reads_live_speed(world, monkeypatch):
    monkeypatch.delenv('EXCALIBUR_ROBOTLAB_FAST')
    speed = [1.]
    ctx = ToolContext(world, speed=lambda: speed[0])
    before = time.monotonic()
    ctx.step(.1)
    normal = time.monotonic() - before
    speed[0] = 4.
    before = time.monotonic()
    ctx.step(.1)
    fast = time.monotonic() - before
    assert normal >= .08
    assert fast < normal * .75


def test_short_controller_steps_keep_cumulative_wall_pacing(world, monkeypatch):
    monkeypatch.delenv('EXCALIBUR_ROBOTLAB_FAST')
    world.model.opt.timestep = .002
    ctx = ToolContext(world)
    started = time.monotonic()
    for _ in range(100):
        ctx.step(.002)
    assert world.data.time == pytest.approx(.2)
    assert .18 <= time.monotonic() - started < .5


def test_failure_history_keeps_two_seconds_and_restores_targets_and_hold(world):
    from robotlab.core import snapshots

    ctx = ToolContext(world)
    ctx.hold('box', 'robot/grasp')
    world.robot.velocity = .15
    ctx.step(3)
    assert len(ctx.history) == 41
    assert ctx.history[0]['time'] == pytest.approx(1.)
    state = ctx.before_failure()
    assert state['time'] == pytest.approx(1.5, abs=.025)
    assert state['held'] == 'box'
    assert state['controller']['velocity'] == .15
    assert state['ctrl'] == world.data.ctrl.tolist()
    # History owns copies, including controller arrays and held transforms.
    original = state['qpos'][:]
    ctx.release()
    world.robot.velocity = 9
    ctx.step(.1)
    assert state['qpos'] == original
    snapshots.restore(ctx, state)
    assert world.data.time == pytest.approx(1.5)
    np.testing.assert_allclose(world.data.qpos, state['qpos'], atol=1e-12)
    np.testing.assert_array_equal(world.data.qvel, state['qvel'])
    assert ctx.held_state() == state['held_state']
    assert world.robot.velocity == .15


def test_early_failure_uses_earliest_available_state(world):
    ctx = ToolContext(world)
    ctx.step(.2)
    assert ctx.before_failure()['time'] == 0


def test_legacy_failure_state_remains_replayable(world):
    from robotlab.core import snapshots

    ctx = ToolContext(world)
    world.robot.push(ctx, dx=.2)
    old = {name: value for name, value in snapshots.capture(ctx).items()
           if name in ('qpos', 'qvel', 'ctrl', 'time', 'held')}
    world.robot.push(ctx, dx=.3)
    snapshots.restore(ctx, old)
    np.testing.assert_array_equal(world.data.qpos, old['qpos'])
    np.testing.assert_array_equal(world.data.qvel, old['qvel'])
    assert world.data.time == old['time']
