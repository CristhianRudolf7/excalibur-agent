"""Lazy registry: unavailable robot plugins do not prevent runtime startup."""
from importlib import import_module
import logging

from ..protocol import RPCError

ROBOTS = {
    'arm': 'robotlab.robots.arm:ArmRobot',
    'dog': 'robotlab.robots.dog:DogRobot',
}


def make(id):
    if id not in ROBOTS:
        raise RPCError('not_found', f'unknown robot: {id}')
    module, name = ROBOTS[id].split(':')
    try:
        return getattr(import_module(module), name)()
    except (ImportError, AttributeError) as exc:
        raise RPCError('unavailable', f'robot {id} is unavailable: {exc}') from exc


def cards():
    result = []
    for id in ROBOTS:
        try:
            result.append(make(id).card())
        except RPCError as exc:
            logging.getLogger(__name__).warning('Skipping robot %s: %s', id, exc)
    return result
