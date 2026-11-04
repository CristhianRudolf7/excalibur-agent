"""Attempt setup records a replayable pose before any motion is requested."""
import json

import pytest

from tests.test_core_sessions import manager, start


@pytest.mark.parametrize('transition', ['start', 'reset', 'resume', 'restart_resume'])
def test_initial_frame_is_recorded_and_emitted_once(tmp_path, environment, fake_modules, transition):
    events = []
    s = manager(tmp_path, environment)
    s.event = lambda name, data: events.append((name, data))
    sid = start(s)
    previous_path = s.store.session_dir(sid) / 'attempts/1/frames.jsonl'
    if transition != 'start':
        s.act(sid, 'push', {'dx': .2, 'duration': .1})
        previous_frames = previous_path.read_bytes()
        if transition == 'reset':
            s.reset(sid, 'Return to the initial pose', no_skill_reason='Testing the same initial state')
        else:
            if transition == 'resume':
                s.stop(sid, 'agent')
            else:
                # Recover an unclean shutdown, then resume the saved session.
                s = manager(tmp_path, environment)
                s.event = lambda name, data: events.append((name, data))
            s.resume(sid)
        assert previous_path.read_bytes() == previous_frames

    attempt = s.get(sid)['attempt']
    path = s.store.session_dir(sid) / 'attempts' / str(attempt) / 'frames.jsonl'
    recorded = [json.loads(line) for line in path.read_text().splitlines()]
    assert len(recorded) == 1
    initial = recorded[0]
    assert initial == {
        'session': sid, 'attempt': attempt, 'seq': 0, 't': 0.,
        'poses': [round(float(v), 4) for v in s.world.poses()],
        'camera': {'name': s.world.robot.default_camera,
                   'pose': [round(float(v), 4) for v in s.world.camera_pose(s.world.robot.default_camera)]},
        'held': None, 'status': 'running',
    }
    assert s.world.data.time == 0
    scene = s.scene(sid)
    assert len(initial['poses']) == 7 * len(scene['bodies'])
    robot_index = next(i for i, body in enumerate(scene['bodies']) if body['name'] == 'robot/base')
    assert initial['poses'][7 * robot_index + 2] == .15
    assert s.store.load(sid)['attempts'][-1]['frames'] == 1
    assert s.state(sid)['attempt']['sim_time'] == 0
    assert s.frames(sid, attempt) == {'frames': [initial], 'total': 1}
    live = [data for name, data in events if name == 'frame' and data['attempt'] == attempt]
    assert live == [initial]

    # Reading the scene, state, or replay must not create another initial frame.
    assert [json.loads(line) for line in path.read_text().splitlines()] == [initial]
    result = s.act(sid, 'push', {'dx': .1, 'duration': .1})
    assert result['ok'] and result['attempt']['status'] == 'running'
    frames = s.frames(sid, attempt)
    assert s.get(sid)['attempts'][-1]['frames'] == frames['total'] == 3
    assert frames['frames'][0] == initial
    assert [frame['seq'] for frame in frames['frames']] == [0, 1, 2]
    assert [frame['t'] for frame in frames['frames']] == [0., .05, .1]
    assert frames['frames'][-1]['poses'] != initial['poses']
