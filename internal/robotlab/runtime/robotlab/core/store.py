"""Small atomic JSON store and append-only logs."""
from __future__ import annotations

import json
import os
import re
import tempfile
import threading
import unicodedata
from datetime import datetime, timezone
from pathlib import Path

from ..protocol import RPCError


def now():
    return datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z')


def identifier(value, limit=80):
    if not isinstance(value, str) or not re.fullmatch(r'[a-z0-9-]{1,' + str(limit) + '}', value):
        raise RPCError('invalid_params', 'invalid identifier')
    return value


def slugify(value, limit, fallback):
    """Keep base letters from accented names before reducing to an ASCII slug."""
    text = ''.join(c for c in unicodedata.normalize('NFKD', value)
                   if not unicodedata.combining(c))
    return re.sub('[^a-z0-9]+', '-', text.lower()).strip('-')[:limit].rstrip('-') or fallback


def read_json(path, default=None):
    try:
        return json.loads(Path(path).read_text())
    except FileNotFoundError:
        return default


def write_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix='.' + path.name, dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as out:
            json.dump(value, out, allow_nan=False, ensure_ascii=False)
            out.write('\n')
            out.flush()
            os.fsync(out.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


class Store:
    def __init__(self, root):
        self.root = Path(root)
        self.root.mkdir(parents=True, exist_ok=True)
        self.lock = threading.RLock()

    def session_dir(self, session):
        return self.root / 'sessions' / identifier(session)

    def save(self, session):
        with self.lock:
            write_json(self.session_dir(session['id']) / 'session.json', session)

    def load(self, session):
        with self.lock:
            value = read_json(self.session_dir(session) / 'session.json')
        if value is None:
            raise RPCError('not_found', 'session not found')
        return value

    def sessions(self):
        with self.lock:
            return sorted([read_json(p) for p in (self.root / 'sessions').glob('*/session.json')],
                          key=lambda s: s['created_at'], reverse=True)

    def append(self, path, entry):
        with self.lock:
            path = Path(path)
            path.parent.mkdir(parents=True, exist_ok=True)
            with path.open('a') as out:
                out.write(json.dumps(entry, allow_nan=False, separators=(',', ':')) + '\n')

    def lines(self, path):
        with self.lock:
            try:
                # Ignore an interrupted final append after a process crash.
                result = []
                for line in Path(path).read_text().splitlines():
                    try:
                        result.append(json.loads(line))
                    except ValueError:
                        continue
                return result
            except FileNotFoundError:
                return []
