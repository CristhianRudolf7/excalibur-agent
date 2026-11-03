"""Short CPU runs exercise storage, queueing, metrics, policies and isolation."""
import json
import os
from pathlib import Path
import subprocess
import sys
import time

import numpy as np
import pytest

from tests.test_core_fixture import Tiny
from tests.test_core_runtime import child_env
from robotlab import sandbox, sdk
from robotlab.experiments import Experiments
from robotlab.protocol import RPCError
from robotlab.sessions import Sessions
from robotlab.teleop import Mapping


def until(predicate, timeout=90):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        value = predicate()
        if value:
            return value
        time.sleep(0.025)
    pytest.fail('condition did not become true before deadline')


def test_training_metrics_policy_and_persistence(tmp_path, child_env):
    events = []
    store = Experiments(tmp_path, lambda name, data: events.append((name, data)))
    sessions = Sessions(lambda *args: None, tmp_path)
    try:
        value = store.create('Tiny PPO', Tiny.id, [{'label': 'PPO', 'algorithm': 'PPO',
                             'hyperparams': {'n_steps': 256, 'batch_size': 256, 'n_epochs': 1,
                                              'policy_kwargs': {'net_arch': [16, 16]}, 'preview_every': 512}}],
                             total_steps=2048)
        identifier, run_id = value['id'], value['runs'][0]['id']
        assert 'sdk.algorithm(ctx.algorithm' in value['code']
        assert (tmp_path / 'experiments' / identifier / 'experiment.py').exists()
        assert value['sandbox'] in {'bwrap', 'rlimits'}
        assert store.write_code(identifier, 'broken (')['ok'] is False
        assert store.get(identifier)['code'] == value['code']
        store.start(identifier)
        with pytest.raises(RPCError):
            store.write_code(identifier, 'pass')
        result = until(lambda: (value if (value := store.get(identifier))['status'] != 'running' else None), 90)
        assert result['status'] == 'done', (result, (tmp_path / 'experiments' / identifier / 'runs' / run_id / 'log.txt').read_text())
        assert result['runs'][0]['steps'] >= 2048
        assert result['runs'][0]['checkpoints'] == ['best', 'last']
        points = store.metrics(identifier)
        assert any('episode_return' in point for point in points['points'])
        assert any('loss' in point for point in points['points'])
        assert store.metrics(identifier, since=points['next'])['points'] == []
        assert any(name == 'metrics' for name, data in events)
        assert any(name == 'frame' and data.get('preview') and data['session'] == run_id for name, data in events)
        session = sessions.get(sessions.open(Tiny.id)['session'])
        path, algo, code = store.policy(identifier, run_id)
        assert session.load_policy(path, algo, code) == {'mode': 'policy'}
        until(lambda: session.step >= 5)
        assert session.mode == 'policy'
    finally:
        sessions.shutdown()
        store.shutdown()
    reloaded = Experiments(tmp_path, lambda *args: None)
    try:
        assert reloaded.get(identifier)['status'] == 'done'
        assert reloaded.metrics(identifier)['points'] == points['points']
    finally:
        reloaded.shutdown()


def test_queue_stop_and_child_logs(tmp_path, child_env):
    events = []
    store = Experiments(tmp_path, lambda name, data: events.append((name, data)))
    try:
        code = ("import time, os, json\n"
                "from pathlib import Path\n"
                "point = {'run': os.environ['EXCALIBUR_ROBOTLAB_RUN_ID'], 'step': 2, 'wall_s': 0.1, 'episode_return': 4}\n"
                "Path('metrics.jsonl').write_text(json.dumps(point) + '\\n')\n"
                "print('worker alive', flush=True)\n"
                "time.sleep(10)\n")
        value = store.create('Queue', Tiny.id, [{'label': str(i), 'algorithm': 'A2C'} for i in range(3)], code=code)
        store.start(value['id'])
        until(lambda: len(store.active) == 2, 3)
        current = store.get(value['id'])
        assert [run['status'] for run in current['runs']].count('queued') == 1
        handles = [active['process'] for active in store.active.values()]
        until(lambda: any(name == 'log' and 'worker alive' in data['line'] for name, data in events), 12)
        points = until(lambda: store.metrics(value['id'])['points'], 3)
        assert points[0]['step'] == 2 and points[0]['episode_return'] == 4
        stopped = store.stop(value['id'])
        assert stopped['status'] == 'stopped'
        assert all(run['status'] == 'stopped' for run in stopped['runs'])
        assert all(process.poll() is not None for process in handles)
        assert not store.active
    finally:
        store.shutdown()


def test_sandbox_network_and_limits(tmp_path):
    if not sandbox.available(tmp_path):
        pytest.skip('bubblewrap network namespaces unavailable on this host')
    code = tmp_path / 'probe.py'
    code.write_text('import socket\ns = socket.socket()\ns.settimeout(.1)\ntry:\n s.connect(("1.1.1.1", 53))\nexcept OSError:\n print("denied")\nelse:\n raise RuntimeError("network was reachable")\n')
    command = sandbox.bubblewrap_args(tmp_path) + [sys.executable, str(code)]
    result = subprocess.run(command, capture_output=True, timeout=3)
    assert result.returncode == 0 and b'denied' in result.stdout


def test_mapping_results_errors_and_timeout(tmp_path, child_env):
    mapping = Mapping(tmp_path / 'mapping', 'def map_pose(pose, state):\n return {"motor": pose["value"]}\n')
    try:
        mapping.submit({'value': 0.75}, {})
        assert until(mapping.poll, 6)['targets'] == {'motor': 0.75}
    finally:
        mapping.close()
    mapping = Mapping(tmp_path / 'late', 'import time\ndef map_pose(pose, state):\n time.sleep(.2)\n return {}\n')
    try:
        mapping.submit({}, {})
        reply = until(mapping.poll, 6)
        assert '50 ms' in reply['error']
        assert until(lambda: mapping.process.poll() is not None, 1)
        assert mapping.poll() is None
    finally:
        mapping.close()


def test_sdk_discrete_context_and_custom_policy(tmp_path, child_env, monkeypatch):
    store = Experiments(tmp_path, lambda *args: None)
    sessions = Sessions(lambda *args: None, tmp_path)
    try:
        value = store.create('Custom', Tiny.id, [{'label': 'test', 'algorithm': 'DQN'}],
                             total_steps=3, reward_weights={'balance': 2.0})
        monkeypatch.setenv('EXCALIBUR_ROBOTLAB_EXPERIMENT_DIR', str(tmp_path / 'experiments' / value['id']))
        monkeypatch.setenv('EXCALIBUR_ROBOTLAB_RUN_ID', value['runs'][0]['id'])
        ctx = sdk.context()
        env = sdk.make_env()
        try:
            env.reset(seed=0)
            assert env.step(np.zeros(1))[1] == 2.0
            model = sdk.algorithm('DQN', env, buffer_size=20, learning_starts=5)
            assert model.device.type == 'cpu'
            assert model.action_space.n == 3
            for name in ('PPO', 'SAC', 'TD3', 'A2C'):
                candidate = sdk.algorithm(name, env)
                assert type(candidate).__name__ == name
                assert candidate.device.type == 'cpu'
                del candidate
            assert np.array_equal(sdk.discrete_action(2, 1), [1])
        finally:
            env.close()
        sdk.log(step=1, episode_return=3.0)
        path = Path(sdk.save_policy(lambda obs: np.zeros(1)))
        assert path.exists()
        session = sessions.get(sessions.open(Tiny.id)['session'])
        assert session.load_policy(path)['mode'] == 'policy'
        (ctx.run_dir / 'policy.py').write_text('import numpy as np\ndef act(obs):\n return np.zeros(1)\n')
        policy, algorithm, code = store.policy(ctx.experiment, ctx.run)
        assert session.load_policy(policy, algorithm, code)['mode'] == 'policy'
    finally:
        store.shutdown()
        sessions.shutdown()


def test_recovery_and_syntax_errors(tmp_path):
    store = Experiments(tmp_path, lambda *args: None)
    value = store.create('Recovery', Tiny.id, [{'label': 'run', 'algorithm': 'PPO'}])
    store.shutdown()
    path = tmp_path / 'experiments' / value['id'] / 'experiment.json'
    persisted = json.loads(path.read_text())
    persisted['status'] = 'running'
    persisted['runs'][0]['status'] = 'running'
    path.write_text(json.dumps(persisted))
    reloaded = Experiments(tmp_path, lambda *args: None)
    try:
        recovered = reloaded.get(value['id'])
        assert recovered['status'] == 'failed'
        assert recovered['runs'][0]['status'] == 'failed'
        assert 'sidecar stopped' in recovered['error']
        assert reloaded.write_code(value['id'], 'def broken(:')['ok'] is False
    finally:
        reloaded.shutdown()


def test_rlimits_fallback_is_recorded_and_enforced(tmp_path, child_env, monkeypatch):
    monkeypatch.setattr(sandbox, 'available', lambda directory: False)
    store = Experiments(tmp_path, lambda *args: None)
    try:
        code = ("import json, os, resource\nfrom pathlib import Path\n"
                "point = {'run': os.environ['EXCALIBUR_ROBOTLAB_RUN_ID'], 'step': 1, 'wall_s': 0, "
                "'extra': {'address_space': resource.getrlimit(resource.RLIMIT_AS)[0], "
                "'cpu': resource.getrlimit(resource.RLIMIT_CPU)[0], "
                "'nice': os.getpriority(os.PRIO_PROCESS, 0)}}\n"
                "Path('metrics.jsonl').write_text(json.dumps(point) + '\\n')\n")
        value = store.create('Fallback', Tiny.id, [{'label': 'limits', 'algorithm': 'PPO',
                             'hyperparams': {'cpu_seconds': 3}}], code=code)
        assert value['sandbox'] == 'rlimits'
        assert 'isolation are unavailable' in value['warning']
        store.start(value['id'])
        result = until(lambda: (item if (item := store.get(value['id']))['status'] != 'running' else None), 5)
        assert result['status'] == 'done'
        extra = store.metrics(value['id'])['points'][0]['extra']
        assert extra['address_space'] == 4 * 1024**3
        assert extra['cpu'] == 3
        assert extra['nice'] >= 15
    finally:
        store.shutdown()
