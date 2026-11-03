"""Scenario registry. Scenario packages register themselves on import."""

from __future__ import annotations

import importlib
import pkgutil
from typing import Dict, Type

from .scenario import Scenario

_REGISTRY: Dict[str, Type[Scenario]] = {}


def register(cls: Type[Scenario]) -> Type[Scenario]:
    """Class decorator: make a scenario available under ``cls.id``."""
    if not cls.id:
        raise ValueError(f"{cls.__name__} has no id")
    if cls.id in _REGISTRY and _REGISTRY[cls.id] is not cls:
        raise ValueError(f"duplicate scenario id {cls.id!r}")
    _REGISTRY[cls.id] = cls
    return cls


def load_all() -> Dict[str, Type[Scenario]]:
    """Import every package under ``robotlab.scenarios`` and return the registry."""
    from . import scenarios

    for mod in pkgutil.iter_modules(scenarios.__path__):
        importlib.import_module(f"{scenarios.__name__}.{mod.name}")
    return dict(_REGISTRY)


def get(scenario_id: str) -> Type[Scenario]:
    if scenario_id not in _REGISTRY:
        load_all()
    try:
        return _REGISTRY[scenario_id]
    except KeyError:
        raise KeyError(f"unknown scenario {scenario_id!r}") from None
