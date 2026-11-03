"""Bounded JSON-lines transport shared by the sidecar and mapping workers."""
from __future__ import annotations

import json
import threading
from typing import Any, BinaryIO, TextIO

MAX_LINE = 8 * 1024 * 1024
ERROR_CODES = frozenset({'invalid_params', 'not_found', 'conflict', 'unavailable', 'internal'})


class RPCError(Exception):
    """A public protocol error."""
    def __init__(self, code: str, message: str):
        super().__init__(message)
        self.code = code


def read_line(stream: BinaryIO) -> dict | None:
    """Read one bounded object, draining an oversized line before rejecting it."""
    raw = stream.readline(MAX_LINE + 1)
    if not raw:
        return None
    if len(raw) > MAX_LINE:
        while raw and not raw.endswith(b'\n'):
            raw = stream.readline(MAX_LINE + 1)
        raise RPCError('invalid_params', 'line exceeds 8 MiB')
    try:
        def reject_constant(value: str) -> None:
            raise ValueError(f'non-finite JSON number: {value}')
        value = json.loads(raw, parse_constant=reject_constant)
    except (ValueError, UnicodeError) as exc:
        raise RPCError('invalid_params', 'invalid JSON') from exc
    if not isinstance(value, dict):
        raise RPCError('invalid_params', 'expected a JSON object')
    return value


class Writer:
    """Serialize responses and events without interleaving concurrent writes."""
    def __init__(self, stream: TextIO):
        self.stream = stream
        self.lock = threading.Lock()

    def send(self, value: dict) -> None:
        line = json.dumps(value, separators=(',', ':'), allow_nan=False)
        if len(line.encode()) + 1 > MAX_LINE:
            raise RPCError('invalid_params', 'output exceeds 8 MiB')
        with self.lock:
            self.stream.write(line + '\n')
            self.stream.flush()

    def event(self, name: str, data: Any) -> None:
        self.send({'event': name, 'data': data})
