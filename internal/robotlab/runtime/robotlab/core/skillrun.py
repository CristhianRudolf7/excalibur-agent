"""Restricted skill language and tool-only robot proxy.

The allowlist intentionally excludes reflection, arbitrary imports and exception
handlers. An instruction budget also bounds code that never advances physics.
"""
from __future__ import annotations

import ast
import math
import sys

from ..protocol import RPCError
from ..robots.base import ActionInterrupted
from .context import ToolContext
from .validation import check

_NODES = {
    'Module', 'FunctionDef', 'arguments', 'arg', 'Return', 'Expr', 'Call', 'Name', 'Load', 'Store',
    'Constant', 'Dict', 'List', 'Tuple', 'Set', 'BinOp', 'UnaryOp', 'BoolOp', 'Compare', 'If', 'IfExp',
    'For', 'While', 'Break', 'Continue', 'Assign', 'AugAssign', 'Pass', 'keyword', 'Attribute',
    'Subscript', 'Slice', 'ListComp', 'DictComp', 'SetComp', 'GeneratorExp', 'comprehension',
    'Import', 'ImportFrom', 'alias', 'Add', 'Sub', 'Mult', 'Div', 'FloorDiv', 'Mod', 'Pow',
    'USub', 'UAdd', 'Not', 'And', 'Or', 'Eq', 'NotEq', 'Lt', 'LtE', 'Gt', 'GtE', 'In', 'NotIn', 'Is', 'IsNot',
}
_MATH = {name for name in dir(math) if not name.startswith('_')}
_METHODS = {'get', 'items', 'keys', 'values', 'append', 'extend', 'pop', 'count', 'index', 'copy'}
import builtins
_SAFE_NAMES = {'abs', 'all', 'any', 'bool', 'dict', 'enumerate', 'float', 'int', 'len', 'list', 'max', 'min',
               'range', 'reversed', 'round', 'set', 'sorted', 'str', 'sum', 'tuple', 'zip'}
_SAFE = {name: getattr(builtins, name) for name in _SAFE_NAMES}


def validate(code):
    if not isinstance(code, str) or len(code) > 100_000:
        return ['code must be text of at most 100000 characters']
    try:
        tree = ast.parse(code)
    except (SyntaxError, ValueError, RecursionError) as exc:
        return [f'invalid skill syntax: {exc}']
    errors = []
    if not any(isinstance(node, ast.FunctionDef) and node.name == 'run' for node in tree.body):
        errors.append('skill must define run(robot, **args)')
    for node in tree.body:
        if not isinstance(node, (ast.FunctionDef, ast.Import, ast.ImportFrom)):
            errors.append('top level allows only functions and math imports')
    for node in ast.walk(tree):
        if type(node).__name__ not in _NODES:
            errors.append(f'{type(node).__name__} is not allowed')
        if isinstance(node, (ast.Name, ast.arg, ast.FunctionDef)):
            name = node.id if isinstance(node, ast.Name) else node.arg if isinstance(node, ast.arg) else node.name
            if name.startswith('_') or name in ('open', 'exec', 'eval', 'compile', 'getattr', 'setattr', 'globals', 'locals', 'vars', 'type', 'object', 'input', 'help', 'breakpoint'):
                errors.append(f'name {name} is not allowed')
        if isinstance(node, ast.FunctionDef) and (node.decorator_list or node.returns or any(a.annotation for a in node.args.args)):
            errors.append('decorators and annotations are not allowed')
        if isinstance(node, ast.Import):
            if any(a.name != 'math' or a.asname for a in node.names):
                errors.append('only import math is allowed')
        if isinstance(node, ast.ImportFrom):
            if node.module != 'math' or node.level or any(a.name not in _MATH or a.asname for a in node.names):
                errors.append('only named math imports are allowed')
        if isinstance(node, ast.Attribute):
            if node.attr.startswith('_') or isinstance(node.ctx, ast.Store):
                errors.append('private attributes and attribute assignment are not allowed')
            elif isinstance(node.value, ast.Name) and node.value.id == 'robot':
                pass  # Proxy resolves predefined tools and observation methods only.
            elif isinstance(node.value, ast.Name) and node.value.id == 'math' and node.attr in _MATH:
                pass
            elif node.attr not in _METHODS:
                errors.append(f'attribute {node.attr} is not allowed')
    return list(dict.fromkeys(errors))


def run(code, robot, args=None, stop=None):
    errors = validate(code)
    if errors:
        raise RPCError('invalid_params', '; '.join(errors))
    def safe_import(name, globals=None, locals=None, fromlist=(), level=0):
        if name != 'math' or level:
            raise RPCError('invalid_params', 'only math is importable')
        return math
    namespace = {'__builtins__': dict(_SAFE, __import__=safe_import)}
    compiled = compile(code, '<skill>', 'exec')
    budget = 100_000
    previous = sys.gettrace()
    def trace(frame, event, arg):
        nonlocal budget
        # Only authored skill lines consume the instruction budget. Tracing
        # every line in the physics/controller stack adds substantial overhead.
        if frame.f_code.co_filename != '<skill>':
            return None
        budget -= 1
        if stop is not None and stop.is_set():
            raise ActionInterrupted('stopped')
        if budget <= 0:
            raise ActionInterrupted('timeout', 'skill instruction budget exceeded')
        return trace
    sys.settrace(trace)
    try:
        exec(compiled, namespace)
        return namespace['run'](robot, **(args or {}))
    finally:
        sys.settrace(previous)


class RobotProxy:
    def __init__(self, ctx, call, look, note):
        self._ctx, self._call, self._look, self._note = ctx, call, look, note

    def __getattr__(self, name):
        if name.startswith('_') or name not in self._ctx.robot.tools():
            raise RPCError('not_found', f'unknown robot tool: {name}')
        def tool(**args):
            return self._call(name, args)
        return tool

    def look(self, **options):
        return self._look(**options)['visible']

    def state(self):
        return self._ctx.robot.state(self._ctx.model, self._ctx.data)

    def objects(self):
        return self._ctx.objects()

    def log(self, text):
        return self._note(text)


def _replay(code, snapshot):
    """Rebuild an isolated failure world and replay without live side effects."""
    from .. import robots
    from .environments import build
    world = build(snapshot['env'], robots.make(snapshot['robot']))
    ctx = ToolContext(world, robot_start=snapshot.get('robot_start'), speed=4)
    from .snapshots import restore
    restore(ctx, snapshot)
    ctx.begin_action(limit=30)
    def call(name, args):
        tool = world.robot.tools().get(name)
        if tool is None:
            raise RPCError('not_found', f'unknown tool: {name}')
        check(args, tool.params)
        ctx.check()
        return tool.fn(ctx, **args)
    from .perception import look
    proxy = RobotProxy(ctx, call, lambda **kw: look(world, **kw), lambda _: None)
    try:
        run(code, proxy, snapshot.get('last_call', {}).get('args', {}))
        ctx.check()
        return {'passed': True, 'detail': 'no fall or safety stop'}
    except Exception as exc:
        return {'passed': False, 'detail': str(exc)}


def regression(code, snapshot):
    try:
        return _replay(code, snapshot)
    except Exception as exc:
        return {'passed': False, 'detail': str(exc)}
