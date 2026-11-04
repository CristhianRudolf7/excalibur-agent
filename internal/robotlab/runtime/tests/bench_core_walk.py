"""Run explicitly to measure ten seconds of recorded/live room locomotion.

Run with the same systemd scope and low priority used for the test suite:
python -m tests.bench_core_walk [--profile]
FAST is always disabled for the reported paced run. The optional profile run
uses FAST only to attribute CPU cost independently of pacing waits.
"""
import argparse
import cProfile
import io
import json
import os
from pathlib import Path
import pstats
import tempfile
import time

from robotlab.protocol import MAX_LINE, Writer
from robotlab.robots.base import ActionInterrupted
from robotlab.server import Runtime


class Sink:
    def __init__(self):
        self.lines = 0
        self.bytes = 0

    def write(self, line):
        size = len(line.encode())
        assert size <= MAX_LINE
        self.lines += 1
        self.bytes += size

    def flush(self):
        pass


def measure(profile=False, robot='dog'):
    sink = Sink()
    writer = Writer(sink)
    with tempfile.TemporaryDirectory(prefix='robotlab-bench-') as root:
        runtime = Runtime(Path(root) / 'data', Path(root) / 'skills', writer.event)
        goal = {'type': 'robot_near', 'target': {'point': [10, 0]}, 'radius': .1}
        sid = runtime.dispatch('session.start', {'robot': robot, 'environment': 'escalera-estrecha',
                               'task': 'Walk for ten seconds', 'goal': goal})['session']['id']
        runtime.dispatch('session.scene', {'session': sid})
        ctx = runtime.sessions.ctx
        origin = ctx.robot.base_xy(ctx.model, ctx.data)
        os.environ['EXCALIBUR_ROBOTLAB_FAST'] = '1' if profile else '0'
        ctx.begin_action()
        profiler = cProfile.Profile() if profile else None
        before_lines = sink.lines
        wall = time.perf_counter()
        cpu = time.process_time()
        if profiler:
            profiler.enable()
        ctx.step(10)
        if profiler:
            profiler.disable()
        elapsed, cpu_elapsed = time.perf_counter() - wall, time.process_time() - cpu
        frames = runtime.dispatch('attempt.frames', {'session': sid, 'attempt': 1, 'count': 400})
        result = {'robot': robot, 'sim_seconds': float(ctx.data.time), 'wall_seconds': elapsed, 'cpu_seconds': cpu_elapsed,
                  'sim_per_wall': float(ctx.data.time) / elapsed, 'recorded_frames': frames['total'],
                  'live_frames': sink.lines - before_lines, 'protocol_bytes': sink.bytes,
                  'distance_m': ctx.robot.base_xy(ctx.model, ctx.data)[0] - origin[0], 'fast': profile}
        print(json.dumps(result), flush=True)
        assert frames['total'] == 200
        assert 0 < result['live_frames'] <= 200
        assert ctx.robot.fallen(ctx.model, ctx.data) is None
        if profiler:
            out = io.StringIO()
            pstats.Stats(profiler, stream=out).strip_dirs().sort_stats('cumtime').print_stats(25)
            print(out.getvalue())
        runtime.close()
        return result


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--profile', action='store_true')
    parser.add_argument('--robot', choices=('dog',), default='dog')
    args = parser.parse_args()
    measure(args.profile, args.robot)
