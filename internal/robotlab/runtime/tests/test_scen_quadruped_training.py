"""Checkpoint selection and resume checks without running an optimizer."""

import json
import time
from types import SimpleNamespace

import numpy as np
import pytest

from robotlab.scenarios.quadruped import train


def test_gait_demonstrations_use_standard_observations_and_actions():
    """Tiny data collection exercises the initializer without fitting a network."""
    observations, actions = train.gait_demonstrations(episodes=2, steps=8)
    assert observations.shape == (16, 48)
    assert actions.shape == (16, 12)
    assert observations.dtype == actions.dtype == np.float32
    assert np.isfinite(observations).all()
    assert np.max(np.abs(actions)) <= 1
    assert np.any(actions != 0)
    assert np.any(observations[1:8, 34:46] != 0)
    with pytest.raises(ValueError, match="positive"):
        train.gait_demonstrations(episodes=0)


def test_evaluation_preserves_best_across_resume(tmp_path, monkeypatch):
    """A resumed, lower-return policy must not replace the previous best."""
    (tmp_path / "metrics.jsonl").write_text(json.dumps({"return": 10.0}) + "\n")
    saved = []
    closed = []

    class Env:
        dt = .02
        data = SimpleNamespace(qpos=np.array([.03]))

        def reset(self, *, seed):
            assert seed == 0
            return np.zeros(48), {}

        def step(self, action):
            return np.zeros(48), 2.0, True, False, {}

        def close(self):
            closed.append(True)

    monkeypatch.setattr(train, "QuadrupedScenario", lambda: SimpleNamespace(make_env=Env))
    callback = train.Evaluation(tmp_path, time.monotonic(), interval=10000)
    callback.model = SimpleNamespace(predict=lambda obs, deterministic: (np.zeros(12), None),
                                     save=lambda path: saved.append(path.name))
    callback.num_timesteps = 18556
    callback._on_training_start()
    assert callback.next_eval == 20000
    assert saved == ["last.zip"]
    record = json.loads((tmp_path / "metrics.jsonl").read_text().splitlines()[-1])
    assert record["steps"] == 18556
    assert record["fell"] is True
    assert record["return"] == 2.0
    assert record["distance"] == pytest.approx(.03)
    assert record["speed"] == pytest.approx(1.5)
    callback.best = 0
    callback.num_timesteps = 20000
    assert callback._on_step()
    assert saved[-2:] == ["best.zip", "last.zip"]
    assert callback.next_eval == 30000
    callback._on_training_end()
    assert len(closed) == 3
