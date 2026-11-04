"""Persisted operator settings, snapshotted when a session starts."""
import math
import threading
from pathlib import Path

from ..protocol import RPCError
from .store import read_json, write_json

DEFAULTS = {'max_attempts': 40, 'sim_speed': 1.0}


class Settings:
    def __init__(self, root):
        self.path = Path(root) / 'settings.json'
        self.lock = threading.RLock()
        self.value = dict(DEFAULTS, **read_json(self.path, {}))
        self.set({})

    def get(self):
        with self.lock:
            return dict(self.value)

    def set(self, fields):
        with self.lock:
            if set(fields) - DEFAULTS.keys():
                raise RPCError('invalid_params', 'unknown settings')
            candidate = dict(self.value, **fields)
            n, speed = candidate['max_attempts'], candidate['sim_speed']
            if type(n) is not int or not 1 <= n <= 100:
                raise RPCError('invalid_params', 'max_attempts must be an integer in 1–100')
            if type(speed) not in (int, float) or not math.isfinite(speed) or not 0.25 <= speed <= 4:
                raise RPCError('invalid_params', 'sim_speed must be in 0.25–4')
            write_json(self.path, candidate)
            self.value = candidate
            return dict(candidate)
