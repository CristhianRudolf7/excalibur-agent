"""Validated goal predicates using simulator truth and horizontal distances."""
from __future__ import annotations

import math

import numpy as np


def _number(x):
    return type(x) in (int, float) and math.isfinite(x)


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
    elif kind in ('object_near', 'robot_near', 'object_held', 'object_on'):
        if kind != 'robot_near':
            fields.add('object')
            obj(goal.get('object'), path + '.object')
        if kind != 'object_held':
            fields.add('target')
            target(goal.get('target'), path + '.target')
            if kind == 'object_on' and (not isinstance(goal.get('target'), dict) or 'object' not in goal['target']):
                errors.append(f'{path}.target must name an object for object_on')
        if kind in ('object_near', 'robot_near'):
            fields.add('radius')
            if not _number(goal.get('radius')) or goal['radius'] <= 0:
                errors.append(f'{path}.radius must be positive')
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

    def predicate(g):
        kind = g['type']
        if kind == 'all':
            return all(predicate(child) for child in g['goals'])
        if kind == 'object_held':
            return ctx.held() == g['object']
        if kind in ('object_near', 'robot_near'):
            p = (ctx.robot.base_xy(ctx.model, ctx.data) if kind == 'robot_near'
                 else position(g['object'])[:2])
            return float(np.linalg.norm(np.asarray(p) - target(g['target']))) <= g['radius']
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
        # The target footprint is evaluated in its own yaw frame.
        _, q = ctx.object_pose(support['id'])
        yaw = math.atan2(2 * (q[0]*q[3] + q[1]*q[2]), 1 - 2 * (q[2]**2 + q[3]**2))
        dx, dy = a[:2] - b[:2]
        xy = np.array([math.cos(yaw)*dx + math.sin(yaw)*dy, -math.sin(yaw)*dx + math.cos(yaw)*dy])
        if container(support):
            # Match the compound bin geometry in world.builder._object.
            thickness = min(.01, *(bh / 2))
            base_top = b[2] - bh[2] + thickness
            bottom = a[2] - ah[2]
            epsilon = 1e-9  # Keep inclusive boundaries stable under pose arithmetic.
            return (ctx.held() != moving['id']
                    and bool(np.all(np.abs(xy) <= bh[:2] - thickness + epsilon))
                    and base_top - .02 - epsilon <= bottom <= b[2] + bh[2] + .04 + epsilon)
        return (ctx.held() != moving['id'] and bool(np.all(np.abs(xy) <= bh[:2]))
                and abs((a[2] - ah[2]) - (b[2] + bh[2])) <= 0.04)

    def container_details(g):
        if g['type'] == 'all':
            return [detail for child in g['goals'] for detail in container_details(child)]
        if g['type'] == 'object_on' and container(object_info(g['target']['object'])):
            return [f"{g['object']} inside {g['target']['object']}"]
        return []

    fallen = None if fallen_checked else ctx.robot.fallen(ctx.model, ctx.data)
    met = not fallen and predicate(goal)
    detail = 'goal met' if met else (fallen or 'goal not met')
    if not fallen:
        descriptions = container_details(goal)
        if descriptions:
            detail += ': ' + '; '.join(descriptions)
    return {'met': bool(met), 'detail': detail}
