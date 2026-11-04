import pytest

from tests.fake_robot import FakeRobot, FakeWorld, install


@pytest.fixture(autouse=True)
def fast(monkeypatch):
    monkeypatch.setenv('EXCALIBUR_ROBOTLAB_FAST', '1')


@pytest.fixture
def environment():
    return {'version': 1, 'id': 'test-room', 'name': 'Test room', 'kind': 'room', 'size': [5, 4],
            'objects': [{'id': 'box', 'name': 'Box', 'shape': 'box', 'size': [.2, .2, .2],
                         'pos': [.4, 0, .1], 'dynamic': True}], 'spawn': {'x': 0, 'y': 0, 'yaw': 0}}


@pytest.fixture
def world(environment):
    return FakeWorld(environment, FakeRobot())


@pytest.fixture
def fake_modules(monkeypatch):
    monkeypatch.setenv('EXCALIBUR_ROBOTLAB_MENAGERIE', '/unused')
    import sys
    from robotlab import robots
    monkeypatch.setattr(robots, 'ROBOTS', dict(robots.ROBOTS))
    for name in ('robotlab.world.builder', 'robotlab.world.schema'):
        monkeypatch.setitem(sys.modules, name, None)
    install()
