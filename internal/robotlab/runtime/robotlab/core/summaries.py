"""Compact factual action outcomes shared by tools and executable skills."""
from __future__ import annotations

import math


def _number(value):
    return isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value)


def _text(value):
    return ' '.join(str(value).split())


def action_summary(name, response, previous_status, held=None):
    result = response['result'] if isinstance(response['result'], dict) else {}
    details = []
    if result.get('error') is not None:
        error = result['error']
        details.append(f'error {error:.2f}' if _number(error) else _text(error))

    pose = next((result[key] for key in ('final_pose', 'pose', 'state')
                 if isinstance(result.get(key), (dict, list, tuple))), response['state'])
    if isinstance(pose, dict):
        coordinates = [pose[key] for key in ('x', 'y', 'z') if key in pose]
    else:
        coordinates = list(pose[:3])
    if len(coordinates) >= 2 and all(_number(v) for v in coordinates):
        details.append('at (' + ','.join(f'{v:.2f}' for v in coordinates) + ')')

    # Prefer target errors and travelled distance before other numeric results.
    preferred = ('error_m', 'target_error_m', 'target_error', 'error_deg',
                 'distance_achieved', 'distance_m', 'distance', 'moved')
    keys = list(preferred) + [key for key in result if key not in preferred]
    for key in keys:
        value = result.get(key)
        if key in ('ok', 'error', 'x', 'y', 'z') or not _number(value):
            continue
        label = key.removesuffix('_m').removesuffix('_deg').replace('_', ' ')
        unit = ' deg' if key.endswith('_deg') else (' m' if key.endswith('_m') or 'distance' in key or key == 'moved' else '')
        details.append(f'{label} {value:.2f}{unit}')
    holding = result.get('held', result.get('holding', held))
    if holding is not None or 'held' in result or 'holding' in result:
        details.append(f'held {_text(holding) if holding is not None else "none"}')

    attempt = response['attempt']
    transition = ''
    if attempt['status'] != previous_status:
        transition = f'; attempt {attempt["status"]}'
        if attempt.get('reason'):
            transition += ': ' + _text(attempt['reason'])
    prefix = f'{_text(name)[:48]} {"ok" if response["ok"] else "failed"}'
    body = prefix + (': ' + ', '.join(details) if details else '')
    # Keep an attempt transition visible even when the result contains long text.
    transition = transition[:120]
    budget = 200 - len(transition)
    if len(body) > budget:
        body = body[:budget - 1].rstrip() + '…'
    return body + transition
