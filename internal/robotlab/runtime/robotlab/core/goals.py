"""Validated goal predicates using simulator truth and horizontal distances."""
from __future__ import annotations

import json
import math

import numpy as np


def _number(x):
    try:
        return type(x) in (int, float) and math.isfinite(x)
    except OverflowError:
        return False


def validate(goal, objects=None, path='goal'):
    errors = []
    known = None if objects is None else {o['id'] if isinstance(o, dict) else o for o in objects}

    def obj(value, field):
        if not isinstance(value, str) or not value:
            errors.append(f'{field} must name an object')
        elif known is not None and value not in known:
            errors.append(f'{field}: unknown object {value}')

    def target(value, field):
        if not isinstance(value, dict) or len(value) != 1:
            errors.append(f'{field} must contain exactly one target')
        elif 'point' in value:
            p = value['point']
            if not isinstance(p, list) or len(p) != 2 or not all(_number(x) for x in p):
                errors.append(f'{field}.point must be [x, y]')
        elif 'object' in value:
            obj(value['object'], field + '.object')
        elif set(value) != {'robot_start'} or value['robot_start'] is not True:
            errors.append(f'{field} has an invalid target')

    if not isinstance(goal, dict):
        return [f'{path} must be an object']
    kind = goal.get('type')
    fields = {'type'}
    if kind == 'all':
        fields.add('goals')
        children = goal.get('goals')
        if not isinstance(children, list) or not children:
            errors.append(f'{path}.goals must be a nonempty list')
        else:
            for i, child in enumerate(children):
                errors.extend(validate(child, objects, f'{path}.goals[{i}]'))
    elif kind in ('object_near', 'robot_near', 'object_held', 'object_on', 'robot_on'):
        if kind not in ('robot_near', 'robot_on'):
            fields.add('object')
            obj(goal.get('object'), path + '.object')
        if kind != 'object_held':
            fields.add('target')
            target(goal.get('target'), path + '.target')
            if kind in ('object_on', 'robot_on') and (not isinstance(goal.get('target'), dict) or 'object' not in goal['target']):
                errors.append(f'{path}.target must name an object for this goal')
        if kind in ('object_near', 'robot_near'):
            fields.add('radius')
            if not _number(goal.get('radius')) or goal['radius'] <= 0:
                errors.append(f'{path}.radius must be positive')
        if kind in ('object_on', 'robot_on'):
            fields.add('stable_s')
            if not _number(goal.get('stable_s', 1.0)) or goal.get('stable_s', 1.0) < 0:
                errors.append(f'{path}.stable_s must be finite and nonnegative')
        if kind == 'robot_on':
            fields.add('feet')
            if type(goal.get('feet', 4)) is not int or not 1 <= goal.get('feet', 4) <= 4:
                errors.append(f'{path}.feet must be an integer in 1–4')
    else:
        errors.append(f'{path}.type is invalid')
    for field in goal.keys() - fields:
        errors.append(f'{path}.{field} is unknown')
    return errors


def evaluate(goal, ctx, robot_start, *, fallen_checked=False):
    """Return simulator truth, including placement inside container targets.

    Ordinary targets use their top face. Containers use the inner footprint
    and the height interval from the base plate to the wall tops.
    """
    environment = getattr(getattr(ctx, 'world', None), 'env', {})
    metadata = {o['id']: o for o in environment.get('objects', [])}

    def object_info(oid):
        if oid in metadata:
            return metadata[oid]
        return next(o for o in ctx.objects() if o['id'] == oid)

    def container(obj):
        return obj.get('compound', False) or bool({'bin', 'container'} & set(obj.get('tags', [])))

    def position(oid):
        return np.asarray(ctx.object_pose(oid)[0])

    def target(t):
        if 'point' in t:
            return np.asarray(t['point'])
        if 'robot_start' in t:
            return np.asarray(robot_start)
        return position(t['object'])[:2]

    fallen = None if fallen_checked else ctx.robot.fallen(ctx.model, ctx.data)
    stamp = float(ctx.data.time)
    if not hasattr(ctx, '_goal_stability') or stamp < getattr(ctx, '_goal_stamp', stamp):
        ctx._goal_stability = {}
    ctx._goal_stamp = stamp

    def stable(g, qualifies):
        key = json.dumps(g, sort_keys=True)
        if not qualifies or fallen:
            ctx._goal_stability.pop(key, None)
            return False
        since = ctx._goal_stability.setdefault(key, stamp)
        return stamp - since + 1e-9 >= g.get('stable_s', 1.0)

    def predicate(g):
        kind = g['type']
        if kind == 'all':
            return all([predicate(child) for child in g['goals']])
        if kind == 'object_held':
            return ctx.held() == g['object']
        if kind in ('object_near', 'robot_near'):
            p = (ctx.robot.base_xy(ctx.model, ctx.data) if kind == 'robot_near'
                 else position(g['object'])[:2])
            return float(np.linalg.norm(np.asarray(p) - target(g['target']))) <= g['radius']
        if kind == 'robot_on':
            support = ctx.world.object_body(g['target']['object'])
            geoms = {i for i in range(ctx.model.ngeom) if ctx.model.geom_bodyid[i] == support}
            feet = ctx.robot.feet_contacts(ctx.model, ctx.data)
            count = sum(bool(geoms.intersection(touched)) for touched in feet.values())
            return stable(g, count >= g.get('feet', 4))
        moving, support = object_info(g['object']), object_info(g['target']['object'])
        a, b = position(moving['id']), position(support['id'])
        def half(o):
            size = o['size']
            if o['shape'] == 'sphere':
                return np.asarray([size[0]] * 3) / 2
            if o['shape'] in ('cylinder', 'capsule'):
                return np.asarray([size[0], size[0], size[1]]) / 2
            return np.asarray(size) / 2
        ah, bh = half(moving), half(support)
        # Compute the moving object's actual lowest face after rotation.
        _, rotation = ctx.object_pose(moving['id'])
        w, x, y, z = rotation
        vertical = np.array([2*(x*z-w*y), 2*(y*z+w*x), 1-2*(x*x+y*y)])
        if moving['shape'] == 'box':
            moving_height = float(np.abs(vertical) @ ah)
        elif moving['shape'] == 'sphere':
            moving_height = ah[2]
        else:
            upright = min(1., abs(float(vertical[2])))
            radius = ah[0]
            moving_height = (radius + (ah[2]-radius)*upright if moving['shape'] == 'capsule'
                             else radius*math.sqrt(1-upright**2) + ah[2]*upright)
        # The target footprint is evaluated in its own yaw frame.
        _, q = ctx.object_pose(support['id'])
        yaw = math.atan2(2 * (q[0]*q[3] + q[1]*q[2]), 1 - 2 * (q[2]**2 + q[3]**2))
        dx, dy = a[:2] - b[:2]
        xy = np.array([math.cos(yaw)*dx + math.sin(yaw)*dy, -math.sin(yaw)*dx + math.cos(yaw)*dy])
        unassisted = (ctx.held() != moving['id'] and not ctx.object_touching_robot(moving['id'])
                      and ctx.object_speed(moving['id']) < .02)
        if container(support):
            # Match the compound bin geometry in world.builder._object.
            thickness = min(.01, *(bh / 2))
            base_top = b[2] - bh[2] + thickness
            bottom = a[2] - moving_height
            epsilon = 1e-9  # Keep inclusive boundaries stable under pose arithmetic.
            placed = (unassisted
                    and bool(np.all(np.abs(xy) <= bh[:2] - thickness + epsilon))
                    and base_top - .02 - epsilon <= bottom <= b[2] + bh[2] + .04 + epsilon)
            return stable(g, placed)
        return stable(g, (unassisted and bool(np.all(np.abs(xy) <= bh[:2]))
                and abs((a[2] - moving_height) - (b[2] + bh[2])) <= 0.01 + 1e-9))

    def container_details(g):
        if g['type'] == 'all':
            return [detail for child in g['goals'] for detail in container_details(child)]
        if g['type'] == 'object_on' and container(object_info(g['target']['object'])):
            return [f"{g['object']} inside {g['target']['object']}"]
        return []

    qualified = predicate(goal)
    met = not fallen and qualified
    detail = 'goal met' if met else (fallen or 'goal not met')
    if not fallen:
        descriptions = container_details(goal)
        if descriptions:
            detail += ': ' + '; '.join(descriptions)
    return {'met': bool(met), 'detail': detail}
