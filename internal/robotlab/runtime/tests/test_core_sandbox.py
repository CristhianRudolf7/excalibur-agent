import math
from types import SimpleNamespace

import pytest
from robotlab.core.skillrun import validate, run
from robotlab.robots.base import ActionInterrupted
from robotlab.protocol import RPCError


@pytest.mark.parametrize('code', [
    'import os\ndef run(robot):\n    return 1',
    'def run(robot):\n    return robot.__class__',
    'def run(robot):\n    return open("secret")',
    'def run(robot):\n    return eval("1")',
    'def run(robot):\n    robot.x = 1',
    'from math import __dict__\ndef run(robot):\n    pass',
    'def run(robot):\n    try:\n        robot.push()\n    except:\n        pass',
])
def test_rejects_unsafe_code(code):
    assert validate(code)
    with pytest.raises(RPCError):
        run(code, None)


def test_allowlist_and_nonstepping_loop_budget():
    code = 'import math\ndef run(robot, angle=0):\n    return sum([math.cos(angle) for i in range(3)])'
    assert validate(code) == []
    assert run(code, None, {'angle': math.pi}) == -3
    with pytest.raises(ActionInterrupted, match='budget'):
        run('def run(robot):\n    while True:\n        pass', None)


def test_regression_rebuild_failure_is_a_failed_case():
    from robotlab.core.skillrun import regression
    assert not regression('def run(robot):\n    pass', {})['passed']
