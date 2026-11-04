import sys
from types import ModuleType

import pytest
from robotlab import robots
from robotlab.protocol import RPCError


def test_lazy_registry(monkeypatch, caplog):
    module = ModuleType('test_plugin')
    class Robot:
        def card(self):
            return {'id': 'test'}
    module.Robot = Robot
    monkeypatch.setitem(sys.modules, 'test_plugin', module)
    monkeypatch.setattr(robots, 'ROBOTS', {'test': 'test_plugin:Robot', 'missing': 'missing_robot:Robot'})
    assert robots.make('test').card() == {'id': 'test'}
    assert robots.cards() == [{'id': 'test'}]
    assert 'Skipping robot missing' in caplog.text
    with pytest.raises(RPCError, match='unknown robot'):
        robots.make('unknown')
