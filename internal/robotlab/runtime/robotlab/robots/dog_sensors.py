"""Terrain rays and contact-derived support geometry for the quadruped."""
from __future__ import annotations

import math

import mujoco
import numpy as np


def terrain_hit(model, data, x, y):
    """Highest non-robot surface and geom at xy, using filtered downward rays."""
    hit = np.zeros(1, dtype=np.int32)
    distance = mujoco.mj_ray(model, data, np.array([x, y, 20.]), np.array([0., 0., -1.]),
                             np.array([1, 1, 0, 0, 1, 1], dtype=np.uint8), 1, -1, hit)
    return (float(20. - distance), int(hit[0])) if distance >= 0 else (0., -1)


def terrain(model, data, x, y):
    return terrain_hit(model, data, x, y)[0]


def landing(model, data, point):
    """Describe the surface beneath a target foot center without moving physics."""
    height, geom = terrain_hit(model, data, *point[:2])
    name = model.geom(geom).name if geom >= 0 else 'floor'
    if geom >= 0:
        body = model.body(model.geom_bodyid[geom]).name
        if body.startswith('obj/'):
            name = body.removeprefix('obj/')
    return dict(terrain_z=round(height, 3), lands_on=name,
                clearance=round(float(point[2])-height, 3))


def terrain_edge(model, data, origin, direction, reach, drop_only=False):
    """Locate the first >3 cm height discontinuity, refined to submillimeters."""
    origin, direction = np.asarray(origin)[:2], np.asarray(direction)
    previous = terrain(model, data, *origin)
    for distance in np.arange(.01, reach+1e-9, .01):
        height = terrain(model, data, *(origin+direction*distance))
        change = previous-height if drop_only else abs(height-previous)
        # Subtracting ray distances can put an exact 3 cm step a few ulps high.
        if change > .03+1e-9:
            low, high = float(distance-.01), float(distance)
            for _ in range(8):
                mid = (low+high)/2
                z = terrain(model, data, *(origin+direction*mid))
                change = previous-z if drop_only else abs(z-previous)
                if change > .03+1e-9:
                    high = mid
                else:
                    low = mid
            return dict(dx=round(high, 3),
                        z_after=round(terrain(model, data, *(origin+direction*high)), 3))
        previous = height
    return dict(dx=None, z_after=None)


def external_contacts(robot, model, data):
    """Return foot contacts and non-foot (robot geom, environment geom) pairs."""
    feet = {leg: [] for leg in robot.legs}
    other = set()
    foot_names = dict(zip(robot.feet, robot.legs))
    for c in data.contact:
        a, b = int(c.geom1), int(c.geom2)
        if (a in robot.robot_geoms) == (b in robot.robot_geoms):
            continue
        rg, eg = (a, b) if a in robot.robot_geoms else (b, a)
        if rg in foot_names:
            feet[foot_names[rg]].append(eg)
        else:
            other.add((rg, eg))
    return {leg: sorted(set(ids)) for leg, ids in feet.items()}, other


def support_geometry(points, com, yaw):
    """Signed Euclidean distance to the convex support boundary and normal heading."""
    pts = sorted(set(tuple(p[:2]) for p in points))
    c = np.asarray(com[:2])
    if not pts:
        return -1., 0.
    if len(pts) == 1:
        delta = c - pts[0]
        angle = (math.degrees(math.atan2(delta[1], delta[0])-yaw)+180) % 360-180
        return -float(np.linalg.norm(delta)), angle

    def cross(o, a, b):
        return (a[0]-o[0])*(b[1]-o[1])-(a[1]-o[1])*(b[0]-o[0])

    lower, upper = [], []
    for p in pts:
        while len(lower) >= 2 and cross(lower[-2], lower[-1], p) <= 0:
            lower.pop()
        lower.append(p)
    for p in reversed(pts):
        while len(upper) >= 2 and cross(upper[-2], upper[-1], p) <= 0:
            upper.pop()
        upper.append(p)
    hull = np.array(lower[:-1]+upper[:-1])
    best, normal, inside = math.inf, np.zeros(2), len(hull) >= 3
    for a, b in zip(hull, np.roll(hull, -1, axis=0)):
        edge = b-a
        length = float(np.linalg.norm(edge))
        if length < 1e-12:
            continue
        outward = np.array([edge[1], -edge[0]])/length
        if np.dot(c-a, outward) > 1e-10:
            inside = False
        near = a + np.clip(np.dot(c-a, edge)/(length*length), 0, 1)*edge
        distance = float(np.linalg.norm(c-near))
        if distance < best:
            best, normal = distance, outward
    angle = (math.degrees(math.atan2(normal[1], normal[0])-yaw)+180) % 360-180
    return (best if inside else -best), angle


def support(robot, model, data):
    contacts, _ = external_contacts(robot, model, data)
    names = [leg for leg in robot.legs if contacts[leg]]
    points = [data.geom_xpos[robot.feet[robot.legs.index(leg)]] for leg in names]
    yaw = robot.pose(data)[5]
    margin, direction = support_geometry(points, data.subtree_com[robot.base], yaw)
    return names, margin, direction


def lift_geometry(points, com, clearance=.04):
    """Measure a three-foot stance and project COM onto its clearance inset."""
    triangle = np.asarray(points, dtype=float)[:, :2]
    c = np.asarray(com, dtype=float)[:2]
    opposite = np.linalg.norm(np.roll(triangle, -1, axis=0)-np.roll(triangle, 1, axis=0), axis=1)
    perimeter = float(opposite.sum())
    center = (opposite @ triangle)/perimeter if perimeter > 1e-12 else triangle.mean(axis=0)
    a, b = triangle[1]-triangle[0], triangle[2]-triangle[0]
    radius = abs(float(a[0]*b[1]-a[1]*b[0]))/perimeter if perimeter > 1e-12 else 0.
    margin, _ = support_geometry(triangle, c, 0.)
    # Offsetting all three edges shrinks the triangle about its incenter.
    # If the requested inset is empty, report the best attainable point instead.
    scale = max(0., 1-clearance/radius) if radius > 1e-12 else 0.
    inset = center+(triangle-center)*scale
    if margin >= clearance:
        shift = np.zeros(2)
    else:
        candidates = []
        for start, end in zip(inset, np.roll(inset, -1, axis=0)):
            edge = end-start
            length2 = float(edge @ edge)
            t = np.clip((c-start) @ edge/length2, 0., 1.) if length2 > 1e-24 else 0.
            candidates.append(start+t*edge)
        nearest = min(candidates, key=lambda point: float(np.linalg.norm(point-c)))
        shift = nearest-c
    return dict(margin=round(margin, 3), triangle_center=center.round(3).tolist(),
                shift_needed=shift.round(3).tolist())


def observation(robot, model, data):
    pose = robot.pose(data)
    rotation = data.xmat[robot.base].reshape(3, 3)
    foot_contacts, pairs = external_contacts(robot, model, data)
    names, margin, direction = support(robot, model, data)
    lift = {}
    if len(names) == 4:
        for i, leg in enumerate(robot.legs):
            others = [data.geom_xpos[foot] for j, foot in enumerate(robot.feet) if j != i]
            lift[leg] = lift_geometry(others, data.subtree_com[robot.base])
    elif len(names) == 3:
        lift = {leg: dict(margin=None) if leg in names else
                dict(aerial=True, support_margin=round(margin, 3)) for leg in robot.legs}
    forces = dict.fromkeys(robot.legs, 0.)
    foot_names = dict(zip(robot.feet, robot.legs))
    for i, c in enumerate(data.contact):
        for rg, eg in ((int(c.geom1), int(c.geom2)), (int(c.geom2), int(c.geom1))):
            if rg in foot_names and eg not in robot.robot_geoms:
                wrench = np.zeros(6)
                mujoco.mj_contactForce(model, data, i, wrench)
                forces[foot_names[rg]] += max(0., float(wrench[0]))
    legs = {}
    forward = np.array([math.cos(pose[5]), math.sin(pose[5])])
    left = np.array([-forward[1], forward[0]])
    for i, leg in enumerate(robot.legs):
        p = data.geom_xpos[robot.feet[i]]
        legs[leg] = dict(joints_deg=dict(zip(robot.parts, np.rad2deg(data.qpos[robot.lq[i*3:i*3+3]]).round(1).tolist())),
                         target_deg=dict(zip(robot.parts, np.rad2deg(robot.target[i*3:i*3+3]).round(1).tolist())),
                         foot_world=p.round(3).tolist(),
                         foot_body=(rotation.T @ (p-pose[:3])).round(3).tolist(),
                         contact=bool(foot_contacts[leg]), force_n=round(forces[leg], 1),
                         terrain_z=round(terrain(model, data, *p[:2]), 3),
                         edge_ahead=terrain_edge(model, data, p, forward, .6),
                         edge_side={side: terrain_edge(model, data, p, sign*left, .4, True)['dx']
                                    for side, sign in (('left', 1), ('right', -1))})
    heading = np.array([[math.cos(pose[5]), -math.sin(pose[5])],
                        [math.sin(pose[5]), math.cos(pose[5])]])
    ahead, edges = [], []
    for dx in (.1, .2, .3, .4, .5, .6):
        for dy in (-.15, 0., .15):
            p = pose[:2] + heading @ [dx, dy]
            ahead.append(dict(dx=dx, dy=dy, z=round(terrain(model, data, *p), 3)))
    # Scan outward from the heading centerline, report the first drop on each side
    # at each forward sample (including the current trunk station).
    for dx in (0., .1, .2, .3, .4, .5, .6):
        center = pose[:2]+heading @ [dx, 0.]
        for side, sign in (('left', 1), ('right', -1)):
            previous = terrain(model, data, *center)
            for distance in np.arange(.01, .401, .01):
                p = center+heading @ [0., sign*distance]
                height = terrain(model, data, *p)
                if previous-height > .03:
                    edges.append(dict(dx=dx, dy=round(sign*float(distance), 3), side=side,
                                      z_drop=round(previous-height, 3)))
                    break
                previous = height
    contacts = [dict(link=model.body(model.geom_bodyid[rg]).name.removeprefix('robot/'),
                     geom=rg, other=model.geom(eg).name or model.body(model.geom_bodyid[eg]).name,
                     other_geom=eg) for rg, eg in sorted(pairs)]
    return dict(x=round(float(pose[0]), 3), y=round(float(pose[1]), 3), z=round(float(pose[2]), 3),
                roll_deg=round(math.degrees(pose[3]), 1), pitch_deg=round(math.degrees(pose[4]), 1),
                yaw_deg=round(math.degrees(pose[5]), 1), vel=data.qvel[robot.rv:robot.rv+3].round(3).tolist(),
                ang_vel_deg=np.rad2deg(data.qvel[robot.rv+3:robot.rv+6]).round(1).tolist(),
                com=data.subtree_com[robot.base].round(3).tolist(), support=names,
                margin=round(margin, 3), margin_dir_deg=round(direction, 1), lift=lift, legs=legs,
                terrain_ahead=ahead, edges=edges, contacts=contacts)
