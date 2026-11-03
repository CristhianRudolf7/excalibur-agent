"""Robot Lab JSON-lines sidecar: python -m robotlab.server --data DIR."""
from __future__ import annotations

import argparse
from concurrent.futures import ThreadPoolExecutor
import os
from pathlib import Path
import signal
import sys
import threading
import traceback

os.environ.setdefault('MUJOCO_GL', 'osmesa')
os.environ['OMP_NUM_THREADS'] = '2'
os.environ['MKL_NUM_THREADS'] = '2'
os.environ['OPENBLAS_NUM_THREADS'] = '2'
os.environ['LP_NUM_THREADS'] = '2'

from . import __version__
from .experiments import Experiments
from .protocol import RPCError, Writer, read_line
from .sdk import load_scenarios
from .sessions import Sessions


class Server:
    """Dispatch the fixed sidecar surface to owned session and experiment stores."""
    def __init__(self, directory: Path, emit):
        self.sessions = Sessions(emit, directory / 'sessions')
        self.experiments = Experiments(directory, emit)
        self.methods = {
            'ping': lambda: {'ok': True},
            'scenarios.list': lambda: {'scenarios': [cls.info() for cls in load_scenarios().values()]},
            'session.open': self.sessions.open,
            'session.close': self.sessions.close,
            'session.reset': lambda session, **p: self.sessions.get(session).reset(**p),
            'session.set_params': lambda session, **p: self.sessions.get(session).set_params(**p),
            'session.control': lambda session, **p: self.sessions.get(session).control(**p),
            'session.snapshot': lambda session, **p: self.sessions.get(session).snapshot(**p),
            'session.skill': lambda session, skill, args: self.sessions.get(session).skill(skill, args),
            'session.load_policy': self.load_policy,
            'experiment.create': self.experiments.create,
            'experiment.list': self.experiments.list,
            'experiment.get': self.experiments.get,
            'experiment.write_code': self.experiments.write_code,
            'experiment.start': self.experiments.start,
            'experiment.stop': self.experiments.stop,
            'experiment.metrics': self.experiments.metrics,
            'teleop.get_mapping': lambda session: self.sessions.get(session).get_mapping(),
            'teleop.set_mapping': lambda session, code=None: self.sessions.get(session).set_mapping(code),
        }

    def load_policy(self, session: str, experiment=None, run=None, checkpoint='best', pretrained=False) -> dict:
        value = self.sessions.get(session)
        if pretrained:
            return value.load_policy()
        if not experiment or not run:
            raise ValueError('experiment and run are required')
        path, algorithm, code = self.experiments.policy(experiment, run, checkpoint)
        return value.load_policy(path, algorithm, code)

    def call(self, method: str, params: dict) -> dict:
        if not isinstance(method, str) or method not in self.methods:
            raise RPCError('invalid_params', 'unknown method')
        if not isinstance(params, dict):
            raise RPCError('invalid_params', 'params must be an object')
        if os.environ.get('EXCALIBUR_ROBOTLAB', '').strip().lower() in {'0', 'off', 'false', 'no'} and method != 'ping':
            raise RPCError('unavailable', 'Robot Lab is disabled')
        try:
            return self.methods[method](**params)
        except KeyError as exc:
            raise RPCError('invalid_params', str(exc)) from exc
        except (TypeError, ValueError, SyntaxError) as exc:
            raise RPCError('invalid_params', str(exc)) from exc

    def close(self) -> None:
        self.sessions.shutdown()
        self.experiments.shutdown()


class Shutdown(BaseException):
    """Interrupt a blocking stdin read on SIGTERM."""


def main() -> None:
    """Read bounded requests; only skills need concurrent request execution."""
    parser = argparse.ArgumentParser()
    parser.add_argument('--data', type=Path, required=True)
    args = parser.parse_args()
    os.environ.setdefault('MUJOCO_GL', 'osmesa')
    args.data = args.data.resolve()
    args.data.mkdir(parents=True, exist_ok=True)
    writer = Writer(sys.stdout)
    # Keep stdout exclusively owned by the protocol, including scenario prints.
    sys.stdout = sys.stderr
    scenarios = load_scenarios()
    writer.event('ready', {'version': __version__, 'scenarios': len(scenarios)})
    server = Server(args.data, writer.event)
    pool = ThreadPoolExecutor(max_workers=4, thread_name_prefix='robotlab-skill')
    slots = threading.BoundedSemaphore(4)

    def handle(request):
        identifier = request.get('id')
        try:
            result = server.call(request.get('method'), request.get('params', {}))
            writer.send({'id': identifier, 'result': result})
        except RPCError as exc:
            writer.send({'id': identifier, 'error': {'code': exc.code, 'message': str(exc)[:2000]}})
        except Exception as exc:
            traceback.print_exc(file=sys.stderr)
            writer.send({'id': identifier, 'error': {'code': 'internal', 'message': str(exc)[:2000]}})

    def handle_skill(request):
        try:
            handle(request)
        finally:
            slots.release()

    stopping = False
    reading = False

    def stop(signum, frame):
        nonlocal stopping
        stopping = True
        # Interrupt only the blocking read. Mutations finish before cleanup.
        if reading:
            raise Shutdown()

    previous = signal.signal(signal.SIGTERM, stop)
    try:
        while not stopping:
            try:
                reading = True
                request = read_line(sys.stdin.buffer)
            except RPCError as exc:
                writer.send({'id': None, 'error': {'code': exc.code, 'message': str(exc)}})
                continue
            finally:
                reading = False
            if request is None:
                break
            if request.get('method') == 'session.skill':
                if slots.acquire(blocking=False):
                    pool.submit(handle_skill, request)
                else:
                    writer.send({'id': request.get('id'), 'error': {'code': 'conflict', 'message': 'too many pending skills'}})
            else:
                handle(request)
    except (Shutdown, KeyboardInterrupt, BrokenPipeError):
        pass
    finally:
        server.close()
        pool.shutdown(wait=True, cancel_futures=True)
        signal.signal(signal.SIGTERM, previous)


if __name__ == '__main__':
    main()
