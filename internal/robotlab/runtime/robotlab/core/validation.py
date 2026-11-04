"""The small JSON-schema vocabulary used by predefined tool arguments."""
import math

from ..protocol import RPCError


def arguments(value, schema, path='args'):
    errors = []
    kind = schema.get('type')
    matches = {'object': isinstance(value, dict), 'array': isinstance(value, list),
               'number': type(value) in (int, float) and math.isfinite(value),
               'integer': type(value) is int, 'boolean': type(value) is bool,
               'string': isinstance(value, str), 'null': value is None}
    if kind and not any(matches.get(k, False) for k in (kind if isinstance(kind, list) else [kind])):
        return [f'{path} must be {kind}']
    if 'enum' in schema and value not in schema['enum']:
        errors.append(f'{path} must be one of {schema["enum"]}')
    if isinstance(value, dict):
        for key in schema.get('required', []):
            if key not in value:
                errors.append(f'{path}.{key} is required')
        props = schema.get('properties', {})
        for key, val in value.items():
            if key in props:
                errors.extend(arguments(val, props[key], path + '.' + key))
            elif schema.get('additionalProperties') is False:
                errors.append(f'{path}.{key} is unknown')
    if isinstance(value, list):
        if len(value) < schema.get('minItems', 0) or len(value) > schema.get('maxItems', float('inf')):
            errors.append(f'{path} has invalid length')
        for i, val in enumerate(value):
            errors.extend(arguments(val, schema.get('items', {}), f'{path}[{i}]'))
    if type(value) in (int, float):
        for key, bad in [('minimum', lambda v: value < v), ('maximum', lambda v: value > v),
                         ('exclusiveMinimum', lambda v: value <= v), ('exclusiveMaximum', lambda v: value >= v)]:
            if key in schema and bad(schema[key]):
                errors.append(f'{path} violates {key} {schema[key]}')
    return errors


def check(value, schema):
    errors = arguments(value, schema)
    if errors:
        raise RPCError('invalid_params', '; '.join(errors))
