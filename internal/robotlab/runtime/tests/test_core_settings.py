import pytest
from robotlab.core.settings import Settings
from robotlab.protocol import RPCError


def test_settings_persist_and_validate(tmp_path):
    settings = Settings(tmp_path)
    assert settings.get() == {'max_attempts': 20, 'sim_speed': 1.0}
    settings.set({'max_attempts': 3, 'sim_speed': 2})
    assert Settings(tmp_path).get()['max_attempts'] == 3
    for invalid in [{'max_attempts': True}, {'max_attempts': 101}, {'sim_speed': float('nan')}, {'sim_speed': 0}, {'x': 1}]:
        with pytest.raises(RPCError):
            settings.set(invalid)
    assert settings.get()['sim_speed'] == 2
