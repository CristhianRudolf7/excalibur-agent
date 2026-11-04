import pytest

from robotlab.core.skills import Skills
from robotlab.core.summaries import action_summary
from tests.test_core_sessions import manager, start


def response(result, ok=True, status='running', reason=''):
    return {'result': result, 'ok': ok, 'state': {'x': 1.38, 'y': -.33},
            'attempt': {'status': status, 'reason': reason}}


def test_success_pose_error_and_held():
    summary = action_summary('walk_to', response({'ok': True, 'error_m': .04, 'held': 'ball'}), 'running')
    assert summary == 'walk_to ok: at (1.38,-0.33), error 0.04 m, held ball'


def test_tool_failure_does_not_report_unchanged_attempt():
    summary = action_summary('walk_to', response({'error': 'blocked by obstacle', 'distance_achieved': 1.2}, ok=False), 'running')
    assert 'walk_to failed: blocked by obstacle' in summary
    assert 'distance achieved 1.20 m' in summary
    assert 'attempt' not in summary


@pytest.mark.parametrize('status, reason', [('failed', 'fell'), ('succeeded', 'goal'), ('stopped', 'user stop')])
def test_attempt_transition(status, reason):
    summary = action_summary('move', response({}, ok=status == 'succeeded', status=status, reason=reason), 'running')
    assert summary.endswith(f'attempt {status}: {reason}')
    assert 'attempt' not in action_summary('move', response({}, status=status), status)


@pytest.mark.parametrize('result', [None, 3, [], {'error': 'long error ' * 100},
                                  {'state': {}, 'distance': float('nan')},
                                  {'final_pose': [2, 3, 4], 'target_error_m': .02},
                                  {'pose': {'x': 2, 'y': 3}, 'holding': None}])
def test_summary_bound_and_varied_results(result):
    summary = action_summary('a' * 100, response(result, ok=False, status='failed', reason='fell'), 'running')
    assert len(summary) <= 200
    assert 'failed' in summary
    assert summary.endswith('attempt failed: fell')


def test_act_logs_result_outcome(tmp_path, environment, fake_modules):
    sessions = manager(tmp_path, environment)
    sid = start(sessions)
    sessions.act(sid, 'push', {'dx': .02})
    summary = sessions.log(sid)['entries'][-1]['summary']
    assert summary.startswith('push ok: at (') and 'moved 0.02 m' in summary
    assert 'attempt' not in summary
    sessions.act(sid, 'push', {'dx': 0, 'fail': True})
    summary = sessions.log(sid)['entries'][-1]['summary']
    assert summary.startswith('push failed:') and 'attempt failed: fell' in summary


def test_skill_logs_returned_failure_and_numbers(tmp_path, environment, fake_modules):
    sessions = manager(tmp_path, environment)
    sid = start(sessions)
    skills = Skills(sessions, tmp_path / 'skills')
    skills.write(sid, 'walk', 'Walk', 'Approach carefully', code=(
        'def run(robot):\n'
        '    return {"ok": False, "error": "blocked by obstacle", "distance": 1.2, "held": "ball"}\n'))
    result = skills.run(sid, 'walk')
    assert not result['ok'] and result['attempt']['status'] == 'running'
    entry = sessions.log(sid)['entries'][-1]
    assert entry['kind'] == 'skill' and entry['ok'] is False
    assert 'walk failed: blocked by obstacle' in entry['summary']
    assert 'distance 1.20 m' in entry['summary'] and 'held ball' in entry['summary']
    assert 'attempt' not in entry['summary']
