"""Safety, interface and interruption coverage using the shared robot harness."""
import json
import math

import mujoco
import numpy as np
import pytest

from robotlab.robots.base import ActionInterrupted, Spawn
from robotlab.robots.drone import DroneRobot
from robotlab.robots.testing import SimpleContext, build


class TraceContext(SimpleContext):
    def __init__(self, *args):
        super().__init__(*args)
        self.positions = []
        self.speeds = []
        self.rates = []

    def step(self, seconds):
        for _ in range(max(1, round(seconds / 0.01))):
            super().step(min(seconds, 0.01))
            state = self.robot.state(self.model, self.data)
            self.positions.append([state['x'], state['y'], state['z']])
            self.speeds.append(state['speed'])
            self.rates.append(float(np.linalg.norm(self.data.qvel[self.robot._v + 3:self.robot._v + 6])))
            assert self.robot.safety(self.model, self.data) is None


def setup(robot=None, objects=None, spawn=Spawn(), context=SimpleContext):
    robot = robot or DroneRobot()
    model, data = build(robot, objects, spawn)
    return robot, context(robot, model, data)


def act(robot, ctx, name, **args):
    result = robot.tools()[name].fn(ctx, **args)
    assert result['ok'], result
    json.dumps(result, allow_nan=False)
    return result


def test_spawn_cameras_and_card():
    robot, ctx = setup(spawn=Spawn(1.2, -0.8, math.pi / 3))
    state = robot.state(ctx.model, ctx.data)
    assert [state['x'], state['y'], state['z']] == pytest.approx([1.2, -0.8, 0.002])
    assert state['yaw_deg'] == pytest.approx(60)
    assert ctx.model.nu == 4
    for obj in (mujoco.mjtObj.mjOBJ_BODY, mujoco.mjtObj.mjOBJ_ACTUATOR,
                mujoco.mjtObj.mjOBJ_CAMERA, mujoco.mjtObj.mjOBJ_SITE):
        count = {mujoco.mjtObj.mjOBJ_BODY: ctx.model.nbody, mujoco.mjtObj.mjOBJ_ACTUATOR: ctx.model.nu,
                 mujoco.mjtObj.mjOBJ_CAMERA: ctx.model.ncam, mujoco.mjtObj.mjOBJ_SITE: ctx.model.nsite}[obj]
        assert all((mujoco.mj_id2name(ctx.model, obj, i) or '').startswith('robot/')
                   for i in range(1 if obj == mujoco.mjtObj.mjOBJ_BODY else 0, count))
    heading = np.array([math.cos(math.pi / 3), math.sin(math.pi / 3), 0])
    for name, expected in [('front', heading * math.cos(math.radians(10)) + [0, 0, -math.sin(math.radians(10))]),
                           ('down', [0, 0, -1])]:
        sid = ctx.model.camera('robot/' + name).id
        direction = -ctx.data.cam_xmat[sid].reshape(3, 3)[:, 2]
        assert direction == pytest.approx(expected)
        assert 60 <= ctx.model.cam_fovy[sid] <= 70
    assert ctx.model.site('robot/grasp').id >= 0
    card = robot.card()
    json.dumps(card, allow_nan=False)
    assert card['name'] == {'en': 'Drone', 'es': 'Dron'}
    assert all(isinstance(s, str) for s in card['notes'])
    assert {t['name'] for t in card['tools']} == set(robot.tools()) | {'look'}
    for tool in robot.tools().values():
        assert tool.params['type'] == 'object'
        assert not tool.params['additionalProperties']
        assert ' / ' in tool.description
        for prop in tool.params['properties'].values():
            assert prop['minimum'] <= prop['maximum']
            if 'default' in prop:
                assert prop['minimum'] <= prop['default'] <= prop['maximum']


def test_camera_rendering():
    robot, ctx = setup()
    act(robot, ctx, 'takeoff')
    with mujoco.Renderer(ctx.model, height=64, width=80) as renderer:
        for camera in ('robot/front', 'robot/down'):
            renderer.update_scene(ctx.data, camera=camera)
            pixels = renderer.render()
            assert pixels.shape == (64, 80, 3)
            assert np.any(pixels)


def test_maximum_speed_and_straight_path():
    robot, ctx = setup(context=TraceContext)
    act(robot, ctx, 'takeoff')
    ctx.positions.clear()
    ctx.speeds.clear()
    start = robot._position(ctx.data).copy()
    end = np.array([3.5, 1.2, 2.0])
    result = act(robot, ctx, 'fly_to', x=end[0], y=end[1], z=end[2], speed=999)
    assert result['error_m'] < 0.1
    assert max(ctx.speeds) <= 1.5
    direction = (end - start) / np.linalg.norm(end - start)
    offsets = np.array(ctx.positions) - start
    perpendicular = offsets - np.outer(offsets @ direction, direction)
    assert np.max(np.linalg.norm(perpendicular, axis=1)) < 0.1
    assert np.all(ctx.data.ctrl >= 0)
    assert np.all(ctx.data.ctrl <= 13)


class WallsDrone(DroneRobot):
    def __init__(self, heights):
        super().__init__()
        self.heights = heights

    def attach(self, spec, spawn, menagerie):
        for i, h in enumerate(self.heights):
            spec.worldbody.add_geom(name=f'wall/{i}', type=mujoco.mjtGeom.mjGEOM_BOX,
                                    pos=[8 + i, 0, h / 2], size=[0.05, 3, h / 2])
        super().attach(spec, spawn, menagerie)


@pytest.mark.parametrize('heights, ceiling', [([], 3.0), ([2.5], 2.2), ([2, 2.8], 2.5), ([4], 3.0), ([0.2], 0.0)])
def test_wall_ceiling_and_clamping(heights, ceiling):
    robot, ctx = setup(WallsDrone(heights), context=TraceContext)
    assert robot.ceiling == pytest.approx(ceiling)
    if ceiling < 0.18:
        assert not robot.takeoff(ctx)['ok']
        assert not robot._motors
        for spec in robot.tools().values():
            for p in spec.params['properties'].values():
                assert p['minimum'] <= p['maximum']
        return
    result = act(robot, ctx, 'takeoff', height=999)
    assert result['target'][2] == pytest.approx(ceiling - 0.03)
    assert max(np.array(ctx.positions)[:, 2]) <= ceiling
    act(robot, ctx, 'fly_to', x=0.1, y=0, z=-99)
    assert robot._target[2] == 0.15
    assert robot._position(ctx.data)[2] > 0.1


def test_wall_like_box_stops_before_impact():
    box = dict(id='barrier', shape='box', size=[0.08, 1.5, 1.2], pos=[1.2, 0, 1.2], dynamic=False)
    robot, ctx = setup(objects=[box])
    act(robot, ctx, 'takeoff')
    start = robot._position(ctx.data).copy()
    with pytest.raises(ActionInterrupted, match='route blocked by obj/barrier') as stopped:
        robot.fly_to(ctx, 3, 0, 1)
    assert stopped.value.reason == 'safety'
    assert np.array_equal(start, robot._position(ctx.data))
    assert not robot._motors
    assert not list(robot._contact(ctx.model, ctx.data))
    assert robot.safety(ctx.model, ctx.data) == 'route blocked by obj/barrier'


@pytest.mark.parametrize('shape,size', [('sphere', [0.2]), ('cylinder', [0.2, 0.5])])
def test_other_fixed_shapes_stop_unsafe_routes(shape, size):
    robot, ctx = setup(objects=[dict(id='obstacle', shape=shape, size=size, pos=[1.2, 0, 1], dynamic=False)])
    act(robot, ctx, 'takeoff')
    with pytest.raises(ActionInterrupted) as stopped:
        robot.fly_to(ctx, 2, 0, 1)
    assert stopped.value.reason == 'safety'


def test_crash_on_fast_body_contact_and_latched_failure():
    robot, ctx = setup(objects=[dict(id='barrier', shape='box', size=[0.08, 1.5, 1.2],
                                    pos=[1, 0, 1.2], dynamic=False)])
    ctx.data.qpos[robot._q:robot._q + 3] = [0.68, 0, 1]
    ctx.data.qvel[robot._v] = 1.2
    mujoco.mj_forward(ctx.model, ctx.data)
    assert list(robot._contact(ctx.model, ctx.data))
    assert robot.fallen(ctx.model, ctx.data) == 'crashed'
    ctx.data.qvel[:] = 0
    assert robot.fallen(ctx.model, ctx.data) == 'crashed'
    robot.control(ctx.model, ctx.data)
    assert np.all(ctx.data.ctrl == 0)


def test_tilt_crash_landing_exemption_and_reset():
    robot, ctx = setup()
    angle = math.radians(81)
    ctx.data.qpos[robot._q + 3:robot._q + 7] = [math.cos(angle / 2), math.sin(angle / 2), 0, 0]
    robot._landing = True
    mujoco.mj_forward(ctx.model, ctx.data)
    assert robot.fallen(ctx.model, ctx.data) == 'crashed'
    mujoco.mj_resetData(ctx.model, ctx.data)
    robot.reset(ctx.model, ctx.data)
    mujoco.mj_forward(ctx.model, ctx.data)
    assert robot.fallen(ctx.model, ctx.data) is None
    assert not robot._landing
    act(robot, ctx, 'takeoff')
    act(robot, ctx, 'land')
    assert not robot._landing
    act(robot, ctx, 'takeoff', height=0.6)
    assert robot._position(ctx.data)[2] == pytest.approx(0.6, abs=0.05)


@pytest.mark.parametrize('name,args', [('fly_to', dict(x=0, y=0, z=1)), ('move', dict(forward=1)),
                                      ('turn', dict(angle_deg=90)), ('hover', dict(seconds=1))])
def test_grounded_flight_tools_fail_without_steps(name, args):
    robot, ctx = setup()
    assert not robot.tools()[name].fn(ctx, **args)['ok']
    assert ctx.data.time == 0


@pytest.mark.parametrize('name,args', [('takeoff', dict(height=float('nan'))),
                                      ('fly_to', dict(x=float('inf'), y=0, z=1)),
                                      ('fly_to', dict(x=0, y=0, z=1, speed=True)),
                                      ('move', dict(left='1')), ('turn', dict(angle_deg=None)),
                                      ('hover', dict(seconds=float('-inf')))])
def test_invalid_numbers_do_not_advance_physics(name, args):
    robot, ctx = setup()
    with pytest.raises(ValueError):
        robot.tools()[name].fn(ctx, **args)
    assert ctx.data.time == 0
    assert not robot._motors


@pytest.mark.parametrize('reason', ['goal', 'stopped', 'timeout', 'safety', 'fell'])
@pytest.mark.parametrize('name,args', [('takeoff', {}), ('fly_to', dict(x=0.5, y=0, z=1)),
                                      ('move', dict(forward=0.2)), ('turn', dict(angle_deg=90)),
                                      ('hover', dict(seconds=1)), ('land', {})])
def test_tool_interruption_propagates(reason, name, args):
    class InterruptedContext(SimpleContext):
        def step(self, seconds):
            raise ActionInterrupted(reason, 'test stop')
    robot, ctx = setup(context=InterruptedContext)
    if name != 'takeoff':
        robot._motors = True
        ctx.data.qpos[robot._q + 2] = 1
        mujoco.mj_forward(ctx.model, ctx.data)
    with pytest.raises(ActionInterrupted) as stopped:
        robot.tools()[name].fn(ctx, **args)
    assert stopped.value.reason == reason
    assert ctx.data.time == 0


def test_runtime_limits_stop_without_core_safety_check():
    robot, ctx = setup()
    ctx.data.qvel[robot._v] = 1.6
    with pytest.raises(ActionInterrupted, match='speed limit'):
        ctx.step(0.02)
    assert not robot._motors
    robot.reset(ctx.model, ctx.data)
    ctx.data.qpos[robot._q + 2] = 3.1
    mujoco.mj_forward(ctx.model, ctx.data)
    with pytest.raises(ActionInterrupted, match='altitude limit'):
        ctx.step(0.02)


def test_runtime_clamps_and_factual_timeout_results(monkeypatch):
    robot, ctx = setup()
    robot._motors = True
    ctx.data.qpos[robot._q + 2] = 1
    mujoco.mj_forward(ctx.model, ctx.data)
    monkeypatch.setattr(ctx, 'run_until', lambda *args, **kwargs: False)
    result = robot.fly_to(ctx, 999, -999, 999, speed=999)
    assert not result['ok']
    assert result['target'] == pytest.approx([30, -30, 2.97])
    assert robot._speed == 1.5
    assert result['error_m'] > 30
    result = robot.move(ctx, forward=-999, left=999, up=-999)
    assert not result['ok']
    assert result['target'] == pytest.approx([-30, 30, 0.15])
    result = robot.fly_to(ctx, 1, 0, 1, speed=-10)
    assert not result['ok']
    assert robot._speed == 0.05
    assert not robot.land(ctx)['ok']
    assert ctx.data.time == 0


def test_negative_turn_full_revolution_and_hover_bounds(monkeypatch):
    robot, ctx = setup(spawn=Spawn(yaw=math.radians(-170)))
    act(robot, ctx, 'takeoff')
    act(robot, ctx, 'turn', angle_deg=-90)
    assert robot.state(ctx.model, ctx.data)['yaw_deg'] == pytest.approx(100, abs=5)
    start = robot._position(ctx.data).copy()
    act(robot, ctx, 'turn', angle_deg=999)
    assert robot.state(ctx.model, ctx.data)['yaw_deg'] == pytest.approx(100, abs=5)
    assert np.linalg.norm(robot._position(ctx.data) - start) < 0.05
    now = ctx.data.time
    act(robot, ctx, 'hover', seconds=-999)
    assert ctx.data.time == now
    durations = []
    monkeypatch.setattr(ctx, 'step', durations.append)
    act(robot, ctx, 'hover', seconds=999)
    assert durations == [60]


def test_contact_angular_speed_and_landing_exemption():
    robot, ctx = setup(objects=[dict(id='barrier', shape='box', size=[0.08, 1.5, 1.2],
                                    pos=[1, 0, 1.2], dynamic=False)])
    ctx.data.qpos[robot._q:robot._q + 3] = [0.68, 0, 1]
    ctx.data.qvel[robot._v + 5] = 12
    mujoco.mj_forward(ctx.model, ctx.data)
    assert list(robot._contact(ctx.model, ctx.data))
    robot._landing = True
    assert robot.fallen(ctx.model, ctx.data) is None
    robot._landing = False
    assert robot.fallen(ctx.model, ctx.data) == 'crashed'


def test_preimpact_velocity_survives_contact_solver():
    robot, ctx = setup()
    ctx.data.qpos[robot._q + 2] = 0.001
    ctx.data.qvel[robot._v + 2] = -1.2
    mujoco.mj_forward(ctx.model, ctx.data)
    with pytest.raises(ActionInterrupted) as stopped:
        ctx.step(0.01)
    assert stopped.value.detail == 'crashed'
    assert robot.fallen(ctx.model, ctx.data) == 'crashed'


def test_land_is_idempotent_on_floor_and_descends_softly():
    robot, ctx = setup(context=TraceContext)
    for _ in range(2):
        act(robot, ctx, 'land')
        assert not robot.state(ctx.model, ctx.data)['landing']
        assert not robot._motors
        assert abs(robot._position(ctx.data)[2]) < 0.04
    act(robot, ctx, 'takeoff')
    ctx.speeds.clear()
    act(robot, ctx, 'land')
    assert max(ctx.speeds) < 0.25
    assert not robot._motors
    assert robot.fallen(ctx.model, ctx.data) is None
