"""Primitive-only UR5e-dimensioned workcell and deterministic part catalogue."""
from __future__ import annotations

import numpy as np

JOINTS = ("shoulder_pan", "shoulder_lift", "elbow", "wrist_1", "wrist_2", "wrist_3")
LIMITS = np.array([[-3.14159, 3.14159], [-3.14159, 0.2], [-3.14159, 3.14159],
                   [-6.28318, 6.28318], [-3.14159, 3.14159], [-6.28318, 6.28318]])
HOME = np.array([0.7, -1.6, 1.8, -1.77079632679, -1.57079632679, 0.0])
BINS = {"A": (0.15, -0.32, 0.76), "B": (0.15, 0.32, 0.76), "reject": (-0.08, 0.35, 0.76)}
COLORS = ((0.85, 0.16, 0.14), (0.12, 0.68, 0.34), (0.12, 0.38, 0.92), (0.96, 0.72, 0.10))
KINDS = ("bolt", "nut", "cube", "ball", "washer")


def validate(params: dict) -> dict:
    """Merge and validate parameters even when called outside the JSON API."""
    p = {"task": "reach", "objects": 5, "seed": 0, "randomize": False, **params}
    if p["task"] not in ("reach", "pick_place", "sort"):
        raise ValueError("task must be reach, pick_place or sort")
    if isinstance(p["objects"], bool) or not isinstance(p["objects"], int) or not 3 <= p["objects"] <= 8:
        raise ValueError("objects must be an integer from 3 through 8")
    if isinstance(p["seed"], bool) or not isinstance(p["seed"], int) or p["seed"] < 0:
        raise ValueError("seed must be a nonnegative integer")
    if not isinstance(p["randomize"], bool):
        raise ValueError("randomize must be boolean")
    if set(p) - {"task", "objects", "seed", "randomize"}:
        raise ValueError("unknown arm parameter")
    return p


def part_specs(count: int) -> list[dict]:
    """Stable mark names and labels; layout alone changes with the reset seed."""
    return [{"name": f"part_{i + 1}", "kind": KINDS[i % 5], "color": i % 4,
             "defect": i % 4 == 3 or i == 2} for i in range(count)]


def build_xml(params: dict) -> str:
    """Generate standard DH frames: Rz(q), Tz(d), Tx(a), Rx(alpha)."""
    p = validate(params)
    rng = np.random.default_rng(p["seed"])
    light = float(rng.uniform(.7, 1.0)) if p["randomize"] else .85
    xml = [f'''<mujoco model="Robot Lab arm"><compiler angle="radian"/>
    <option timestep="0.002" integrator="implicitfast" cone="elliptic"/>
    <visual><global offwidth="640" offheight="480"/><headlight diffuse=".5 .5 .5"/></visual>
    <default><joint damping="3" armature=".08"/>
    <geom friction="1.5 .01 .001" solref=".008 1" solimp=".95 .99 .001"/>
    <default class="arm"><geom contype="0" conaffinity="0" rgba=".78 .82 .87 1" density="1600"/></default>
    </default><worldbody><light pos="0 -1 3" diffuse="{light} {light} {light}"/>
    <light pos="-1 1 2" diffuse=".4 .45 .5"/>
    <geom name="floor" type="plane" size="3 3 .1" rgba=".13 .17 .23 1"/>
    <geom name="table" type="box" pos="0 0 .70" size=".85 .65 .04" rgba=".35 .40 .47 1"/>
    <geom type="box" pos="-.65 -.48 .34" size=".035 .035 .34" rgba=".22 .27 .33 1"/>
    <geom type="box" pos=".65 .48 .34" size=".035 .035 .34" rgba=".22 .27 .33 1"/>
    <camera name="work" pos=".17 -.08 1.6" xyaxes="1 0 0 0 .996 .087" fovy="32"/>
    <camera name="top" pos=".05 0 2.5" quat="1 0 0 0" fovy="48"/>
    <camera name="side" pos="1.6 -1.8 1.65" xyaxes=".747 .664 0 -.24 .27 .933" fovy="48"/>
    <body name="target" mocap="true" pos=".2 0 1.05"><geom name="target_marker" type="sphere" size=".018" rgba=".25 1 .6 {".45" if p["task"] == "reach" else "0"}" contype="0" conaffinity="0"/></body>
    <body name="base" pos="-.4 0 .74" euler="0 0 3.141592653589793" childclass="arm" gravcomp="1">
    <geom type="cylinder" pos="0 0 .04" size=".085 .04" rgba=".15 .35 .65 1"/>''']
    # Standard UR5e DH dimensions in metres. Each moving body precedes its fixed DH frame.
    a = [0, -.425, -.3922, 0, 0, 0]
    d = [.1625, 0, 0, .1333, .0997, .0996]
    alpha = [np.pi / 2, 0, 0, np.pi / 2, -np.pi / 2, 0]
    for i, name in enumerate(JOINTS):
        lo, hi = LIMITS[i]
        xml.append(f'<body name="{name}_body" gravcomp="1"><joint name="{name}" axis="0 0 1" range="{lo} {hi}"/>')
        xml.append('<geom type="cylinder" size=".046 .045" rgba=".18 .43 .74 1"/>')
        end = (a[i], 0, d[i])
        if abs(a[i]) + abs(d[i]) > .05:
            xml.append(f'<geom type="capsule" fromto="0 0 0 {end[0]} 0 {end[2]}" size=".034"/>')
        xml.append(f'<body name="dh_{i}" pos="{a[i]} 0 {d[i]}" euler="{alpha[i]} 0 0" gravcomp="1">')
    xml.append('''<body name="gripper" gravcomp="1"><geom type="box" pos="0 0 .025" size=".065 .034 .025" rgba=".20 .25 .31 1"/>
    <site name="tcp" pos="0 0 .115" size=".006" rgba=".2 .9 .8 1"/>
    <body name="left_finger" pos=".009 0 .09" gravcomp="1">
      <joint name="gripper" type="slide" axis="1 0 0" range="0 .045" damping="8" armature=".01"/>
      <geom name="left_pad" type="box" size=".009 .028 .035" rgba=".12 .16 .20 1" contype="1" conaffinity="1" friction="3 .02 .002" density="900"/>
    </body><body name="right_finger" pos="-.009 0 .09" gravcomp="1">
      <joint name="finger_right" type="slide" axis="-1 0 0" range="0 .045" damping="8" armature=".01"/>
      <geom name="right_pad" type="box" size=".009 .028 .035" rgba=".12 .16 .20 1" contype="1" conaffinity="1" friction="3 .02 .002" density="900"/>
    </body></body>''')
    xml.extend(['</body></body>'] * 6)
    xml.append('</body>')
    for index, (name, pos) in enumerate(BINS.items()):
        x, y, z = pos
        xml.append(f'<camera name="bin_{name}" pos="{x} {y - .035} 1.35" xyaxes="1 0 0 0 .998 .06" fovy="25"/>')
        color = ((.12, .45, .9), (.15, .68, .42), (.8, .24, .25))[index]
        rgba = ' '.join(map(str, (*color, 1)))
        xml.append(f'<body name="bin_{name}" pos="{x} {y} {z}"><geom name="bin_{name}_floor" type="box" size=".085 .075 .02" rgba="{rgba}"/>')
        for dx, dy, sx, sy in ((-.09, 0, .005, .08), (.09, 0, .005, .08), (0, -.08, .09, .005), (0, .08, .09, .005)):
            xml.append(f'<geom type="box" pos="{dx} {dy} .025" size="{sx} {sy} .045" rgba="{rgba}"/>')
        # Raised primitive strokes label A, B and reject (X), visible without textures.
        strokes = {"A": [(-.025, -.025, 0, .025), (0, .025, .025, -.025), (-.015, -.005, .015, -.005)],
                   "B": [(-.02, .005, -.02, .055), (-.02, .055, .02, .04), (-.02, .03, .02, .04), (-.02, .03, .02, .015), (-.02, .005, .02, .015)],
                   "reject": [(-.02, .005, .02, .055), (.02, .005, -.02, .055)]}[name]
        for x1, y1, x2, y2 in strokes:
            xml.append(f'<geom type="capsule" fromto="{x1} {y1} .043 {x2} {y2} .043" size=".003" rgba=".95 .96 1 1" contype="0" conaffinity="0"/>')
        xml.append('</body>')
    if p["task"] != "reach":
        for i, spec in enumerate(part_specs(p["objects"])):
            color = np.array(COLORS[spec["color"]])
            if p["randomize"]:
                color = np.clip(color * rng.uniform(.85, 1.15, 3), .05, 1)
            rgba = ' '.join(map(str, (*color, 1)))
            xml.append(f'<body name="{spec["name"]}" pos="{.03 + (i % 4) * .105} {-.08 + (i // 4) * .16} .77"><freejoint/><geom name="{spec["name"]}_main" ')
            shape = {"bolt": 'type="cylinder" size=".013 .023"', "nut": 'type="cylinder" size=".023 .014"',
                     "cube": 'type="box" size=".02 .02 .02"', "ball": 'type="sphere" size=".021"',
                     "washer": 'type="cylinder" size=".025 .009"'}[spec["kind"]]
            xml.append(f'{shape} rgba="{rgba}" mass=".035"/>')
            if spec["kind"] == "bolt":
                xml.append(f'<geom type="cylinder" pos="0 0 .02" size=".022 .006" rgba="{rgba}" mass=".008"/>')
            if spec["kind"] in ("nut", "washer"):
                xml.append('<geom type="cylinder" pos="0 0 .014" size=".009 .001" rgba=".08 .09 .11 1" contype="0" conaffinity="0" mass=".0001"/>')
            if spec["defect"]:
                xml.append(f'<geom name="{spec["name"]}_defect" type="box" pos=".004 0 .022" euler="0 0 .45" size=".004 .021 .003" rgba=".04 .04 .05 1" mass="0" contype="0" conaffinity="0"/>')
            xml.append('</body>')
    xml.append('</worldbody><equality><joint joint1="finger_right" joint2="gripper" solref=".004 1"/></equality><actuator>')
    for name in JOINTS:
        i = JOINTS.index(name)
        xml.append(f'<position name="{name}" joint="{name}" kp="900" kv="75" ctrlrange="{LIMITS[i, 0]} {LIMITS[i, 1]}" forcerange="-160 160"/>')
    xml.append('<position name="gripper" joint="gripper" kp="2200" kv="25" ctrlrange="0 .045" forcerange="-65 65"/></actuator></mujoco>')
    return ''.join(xml)
