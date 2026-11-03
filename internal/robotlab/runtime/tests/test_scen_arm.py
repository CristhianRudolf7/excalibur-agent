"""Bounded physics, skill and retargeting checks for the arm scenario."""
import mujoco
import numpy as np
import pytest

from robotlab.scenarios.arm import ArmScenario
from robotlab.scenarios.arm.model import BINS, HOME, JOINTS


def exhaust(generator):
    """Capture final status while enforcing a hard frame budget."""
    for _ in range(1500):
        try:
            next(generator)
        except StopIteration as done:
            return done.value
    pytest.fail("skill exceeded frame budget")


@pytest.mark.parametrize("task", ["reach", "pick_place", "sort"])
@pytest.mark.parametrize("count", range(3, 9))
def test_model_builds(task, count):
    s = ArmScenario({"task": task, "objects": count, "randomize": True})
    m = mujoco.MjModel.from_xml_string(s.build_xml(s.params))
    assert m.nu == 7 and m.neq == 1 and not m.nmesh
    assert m.camera("top").id >= 0 and m.camera("side").id >= 0
    assert [m.joint(i).name for i in range(6)] == list(JOINTS)


@pytest.mark.parametrize("task", ["reach", "pick_place", "sort"])
def test_2000_random_steps_stable(task):
    env = ArmScenario({"task": task, "objects": 8}).make_env()
    rng = np.random.default_rng(31)
    for _ in range(2000):
        obs, reward, _, _, info = env.step(rng.uniform(-1, 1, 7))
        assert np.all(np.isfinite(obs)) and np.isfinite(reward)
        assert np.max(np.abs(env.data.qvel)) < 500
        assert np.all(np.isfinite(list(info["terms"].values())))
    assert all(w.number == 0 for w in env.data.warning)


def test_ik_20_random_targets_within_centimetre():
    s = ArmScenario()
    env = s.make_env()
    rng = np.random.default_rng(24)
    for xyz in rng.uniform([.02, -.2, .91], [.30, .2, 1.23], size=(20, 3)):
        exhaust(s.skill(env, "move_to", {"xyz": xyz.tolist(), "yaw": float(rng.uniform(-.4, .4))}))
        assert np.linalg.norm(env.tcp - xyz) < .01
        assert env.data.site_xmat[env.tcp_id].reshape(3, 3)[2, 2] < -.99


@pytest.mark.parametrize("object_id,target", [(1, "A"), (4, "reject"), (5, "B")])
def test_pick_place_end_to_end(object_id, target):
    s = ArmScenario({"task": "sort", "seed": 0})
    env = s.make_env()
    detail = exhaust(s.skill(env, "pick_place", {"object": object_id, "target": target}))
    assert detail == {"in_target": True, "bin": target,
                      "detail": f"Placed part_{object_id} in bin {target} and returned home"}
    assert env.in_bin(f"part_{object_id}", target)
    position = env.data.body(f"part_{object_id}").xpos.copy()
    assert np.all(np.abs(position[:2] - BINS[target][:2]) < [.07, .06])
    assert .78 < position[2] < .85 and env.data.qpos[6] > .03
    if object_id == 1 and target == "A":
        terms = env._reward_terms(s.idle_action(env))
        assert terms["success"] == 1 and terms["lift"] > 0
    np.testing.assert_allclose(env.data.qpos[:6], HOME, atol=.01)


def test_marks_visible_top_camera():
    s = ArmScenario({"task": "sort", "objects": 8})
    env = s.make_env()
    marks = s.marks(env)
    assert len(marks) == len({m["label"] for m in marks}) == 8
    with mujoco.Renderer(env.model, height=480, width=640) as renderer:
        renderer.enable_segmentation_rendering()
        renderer.update_scene(env.data, camera="top")
        seg = renderer.render()
        for mark in marks:
            ids = np.flatnonzero(env.model.geom_bodyid == mark["body"])
            pixels = np.isin(seg[:, :, 0], ids) & (seg[:, :, 1] == mujoco.mjtObj.mjOBJ_GEOM)
            assert np.count_nonzero(pixels) >= 8, mark["name"]


def test_pose_mapping_smoothing_limits_and_occlusion():
    s = ArmScenario()
    pose = {"landmarks": [[.5, .5, 0., 1.] for _ in range(33)]}
    for idx, point in zip((12, 14, 16), ([.4, .3, 0, 1], [.6, .5, -.1, 1], [.65, .3, -.2, 1])):
        pose["landmarks"][idx] = point
    hand = [[.5 + .01 * i, .5, 0.] for i in range(21)]
    hand[4] = hand[8][:]
    pose["hands"] = [{"handedness": "Right", "landmarks": hand}]
    state = {}
    mapping = s.pose_mapping()
    before = mapping({}, {})
    after = mapping(pose, state)
    assert set(after) == {*JOINTS, "gripper"}
    assert all(np.isfinite(x) and -1 <= x <= 1 for x in after.values())
    assert after["gripper"] < before["gripper"]
    assert abs(after["shoulder_pan"] - before["shoulder_pan"]) < .3
    assert mapping({"landmarks": [[float("nan")]]}, state) == after
    assert mapping({}, state) == after
    pose["arm"] = "left"
    assert np.all(np.isfinite(list(mapping(pose, {}).values())))
    pose["arm"] = "right"
    pose["calibration"] = {"landmarks": pose["landmarks"]}
    assert np.all(np.isfinite(list(mapping(pose, {}).values())))


def test_manual_and_idle_hold_other_joints():
    s = ArmScenario()
    env = s.make_env()
    idle = s.idle_action(env)
    action = s.manual_action(env, {"shoulder_pan": .3, "gripper": -1})
    assert action[0] == pytest.approx(.3) and action[6] == -1
    np.testing.assert_array_equal(action[1:6], idle[1:6])
    assert s.manual_action(env, {"KeyD": 1})[0] > idle[0]
    with pytest.raises(ValueError, match="finite"):
        s.manual_action(env, {"elbow": float("nan")})


def test_reward_signs_and_reweighting():
    env = ArmScenario().make_env()
    action = env.scenario.idle_action(env)
    env.target = env.tcp
    near = env._reward_terms(action)
    env.target = env.tcp + [1, 0, 0]
    far = env._reward_terms(-action)
    assert near["success"] == 1 and near["distance"] == 0
    assert far["distance"] < 0 and far["success"] == 0
    assert far["effort"] <= 0 and far["smooth"] < 0
    env = ArmScenario({"task": "pick_place"}).make_env()
    terms = env._reward_terms(np.zeros(7))
    assert set(terms) == {"reach", "grasp", "lift", "place", "success", "effort"}
    assert terms["reach"] < 0 and terms["place"] < 0
    assert terms["grasp"] == terms["success"] == 0
    env = ArmScenario().make_env({"distance": 0, "success": 0, "effort": 0, "smooth": 0})
    assert env.step(np.zeros(7))[1] == 0


def test_seeded_reset_and_randomisation():
    s = ArmScenario({"task": "sort", "objects": 8, "randomize": True})
    env = s.make_env()
    first = env.reset(seed=7)[0]
    positions = [m["world"] for m in s.marks(env)]
    np.testing.assert_array_equal(first, env.reset(seed=7)[0])
    assert positions == [m["world"] for m in s.marks(env)]
    env.reset(seed=8)
    assert positions != [m["world"] for m in s.marks(env)]


def test_invalid_inputs_and_switch(monkeypatch):
    for p in ({"task": "bad"}, {"objects": 2}, {"objects": True}, {"seed": -1}, {"randomize": "yes"}, {"unknown": 1}):
        with pytest.raises(ValueError):
            ArmScenario(p)
    s = ArmScenario()
    env = s.make_env()
    for xyz in ([10, 0, 1], [0, 0, .2], [float("nan"), 0, 1], None):
        with pytest.raises(ValueError):
            exhaust(s.skill(env, "move_to", {"xyz": xyz}))
    with pytest.raises(ValueError, match="unknown"):
        exhaust(s.skill(env, "invalid", {}))
    for args in ({"object": "missing", "target": "A"}, {"object": 1, "target": "bad"}):
        with pytest.raises(ValueError):
            exhaust(s.skill(env, "pick_place", args))
    monkeypatch.setenv("EXCALIBUR_ROBOTLAB_ARM", "off")
    with pytest.raises(RuntimeError, match="disabled"):
        s.make_env()


def test_batched_physics_matches_individual_substeps():
    scenario = ArmScenario()
    env, reference = scenario.make_env(), scenario.make_env()
    action = np.linspace(-.5, .5, 7)
    reference.data.ctrl[:] = reference.scale_action(action)
    for _ in range(reference.frame_skip):
        mujoco.mj_step(reference.model, reference.data)
    env.step(action)
    np.testing.assert_allclose(env.data.qpos, reference.data.qpos, atol=1e-12)
    np.testing.assert_allclose(env.data.qvel, reference.data.qvel, atol=1e-12)
    for malformed in ([0], [float("nan")] * 7):
        with pytest.raises(ValueError, match="seven finite"):
            env.step(malformed)


def test_ik_handles_opposite_yaw():
    scenario = ArmScenario()
    env = scenario.make_env()
    xyz = [.15, 0., 1.05]
    exhaust(scenario.skill(env, "move_to", {"xyz": xyz, "yaw": 0.}))
    exhaust(scenario.skill(env, "move_to", {"xyz": xyz, "yaw": np.pi}))
    assert np.linalg.norm(env.tcp - xyz) < .01
    rotation = env.data.site_xmat[env.tcp_id].reshape(3, 3)
    assert rotation[0, 0] < -.99 and rotation[2, 2] < -.99


def test_smallest_layout_has_defect_and_reset_varies_paint():
    scenario = ArmScenario({"task": "sort", "objects": 3, "randomize": True})
    env = scenario.make_env()
    assert any(mark["label"].endswith(":defect") for mark in scenario.marks(env))
    env.reset(seed=23)
    first = env.model.geom_rgba.copy()
    env.reset(seed=23)
    np.testing.assert_array_equal(first, env.model.geom_rgba)
    env.reset(seed=24)
    assert not np.array_equal(first, env.model.geom_rgba)
    assert np.max(env.model.geom_rgba[env.model.geom("part_3_defect").id, :3]) < .1


def test_bilingual_display_metadata():
    info = ArmScenario.info()
    displays = [info["title"], info["description"]]
    displays += [item["label"] for item in info["actuators"]]
    displays += [item["description"] for item in info["reward_terms"]]
    for display in displays:
        assert set(display) == {"en", "es"}
        assert display["en"] and display["es"]
        assert display["en"] != display["es"]


def test_editable_mapping_source_matches_in_process_mapping():
    import ast
    import copy

    scenario = ArmScenario()
    source = scenario.pose_mapping_source()
    imports = [node for node in ast.walk(ast.parse(source)) if isinstance(node, (ast.Import, ast.ImportFrom))]
    assert len(imports) == 1 and imports[0].names[0].name == "numpy"
    namespace = {}
    exec(compile(source, "default_arm_mapping.py", "exec"), namespace)
    exported = namespace["map_pose"]
    in_process = scenario.pose_mapping()
    landmarks = [[.5, .5, 0., 1.] for _ in range(33)]
    for indices in ((12, 14, 16), (11, 13, 15)):
        for idx, point in zip(indices, ([.4, .3, 0, 1], [.6, .5, -.1, 1], [.65, .3, -.2, 1])):
            landmarks[idx] = point
    hand = [[.5 + .01 * i, .5, .001 * i] for i in range(21)]
    hand[4] = hand[8][:]
    poses = [{"landmarks": landmarks, "hands": [{"handedness": "Right", "landmarks": hand}]},
             {"landmarks": landmarks, "arm": "left", "world": [p[:3] for p in landmarks]},
             {"landmarks": landmarks, "calibration": {"landmarks": landmarks}},
             {"landmarks": [[float("nan")]]}, {}]
    for config in ({}, {"gains": [.5] * 6, "offsets": HOME.tolist(), "dead_zone": .01, "smoothing": .6}):
        state, exported_state = copy.deepcopy(config), copy.deepcopy(config)
        for _ in range(3):
            for pose in poses:
                expected = in_process(copy.deepcopy(pose), state)
                actual = exported(copy.deepcopy(pose), exported_state)
                assert actual == expected
                assert exported_state == state


def test_runtime_preserves_default_pose_smoothing_state(tmp_path):
    import time
    from robotlab.sessions import Session

    session = Session("arm", {}, 0, lambda *args: None, tmp_path)
    pose = {"landmarks": [[.5, .5, 0., 1.] for _ in range(33)]}
    for idx, point in zip((12, 14, 16), ([.4, .3, 0, 1], [.6, .5, -.1, 1], [.65, .3, -.2, 1])):
        pose["landmarks"][idx] = point
    session.control("pose", pose=pose)
    expected_state = {}
    try:
        for _ in range(4):
            expected = session.scenario.pose_mapping()(pose, expected_state)
            action = session._action(time.monotonic())
            np.testing.assert_allclose(action, list(expected.values()), atol=1e-7)
        session.reset(seed=0)
        assert session.pose_state == {}
    finally:
        session.env.close()


def test_editable_mapping_worker_preserves_smoothing_state(tmp_path):
    import time
    from robotlab.teleop import Mapping

    scenario = ArmScenario()
    mapping = Mapping(tmp_path, scenario.pose_mapping_source())
    pose = {"landmarks": [[.5, .5, 0., 1.] for _ in range(33)]}
    for idx, point in zip((12, 14, 16), ([.4, .3, 0, 1], [.6, .5, -.1, 1], [.65, .3, -.2, 1])):
        pose["landmarks"][idx] = point
    state = {}
    try:
        for _ in range(4):
            mapping.submit(pose, {})
            deadline = time.monotonic() + 5
            reply = None
            while reply is None and time.monotonic() < deadline:
                time.sleep(.01)
                reply = mapping.poll()
            assert reply is not None
            assert "error" not in reply, reply
            assert reply["targets"] == scenario.pose_mapping()(pose, state)
    finally:
        mapping.close()

@pytest.mark.parametrize("seed", [0, 7, 23])
def test_work_camera_inspection_marks_and_defects(seed):
    from robotlab.marks import snapshot, badge_position
    scenario = ArmScenario({"task": "sort", "objects": 8})
    env = scenario.make_env()
    env.reset(seed=seed)
    result = snapshot(env, camera="work", width=1024, height=768, marks=True)
    assert [m["id"] for m in result["marks"]] == list(range(1, 9))
    occupied = [tuple(m["bbox"]) for m in result["marks"]]
    for mark in result["marks"]:
        x0, y0, x1, y1 = mark["bbox"]
        assert min(x1-x0, y1-y0) >= 40
        x, y = badge_position(mark["bbox"], occupied, 1024, 768)
        assert all(x+8 < left or x-8 > right or y+8 < top or y-8 > bottom
                   for left, top, right, bottom in occupied)
        occupied.append((x-9, y-9, x+9, y+9))
    assert env.model.geom("target_marker").rgba[3] == 0
    # Defect geometry must contribute enough visible dark pixels for inspection.
    with mujoco.Renderer(env.model, height=768, width=1024) as renderer:
        renderer.update_scene(env.data, camera="work")
        renderer.enable_segmentation_rendering()
        segmentation = renderer.render()
        for part in env.parts:
            if part["defect"]:
                gid = env.model.geom(part["name"] + "_defect").id
                assert np.count_nonzero(segmentation[:, :, 0] == gid) >= 80


def test_mark_ids_survive_invisible_parts():
    from robotlab.marks import snapshot
    scenario = ArmScenario({"task": "sort", "objects": 8})
    env = scenario.make_env()
    body = env.model.body("part_1")
    adr = env.model.jnt_qposadr[body.jntadr[0]]
    env.data.qpos[adr:adr+3] = [10, 10, .75]
    mujoco.mj_forward(env.model, env.data)
    result = snapshot(env, camera="work", width=1024, height=768, marks=True)
    assert [m["id"] for m in result["marks"]] == list(range(2, 9))
    assert [m["name"] for m in result["marks"]] == [f"part_{i}" for i in range(2, 9)]
