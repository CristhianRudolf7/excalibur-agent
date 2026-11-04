"""Solved robot/environment contacts in simulation time, excluding the floor."""
from __future__ import annotations

from collections import deque

import mujoco
import numpy as np


class Contacts:
    window = 1.5

    def __init__(self, world):
        self.model, self.data = world.model, world.data
        self.history = deque()
        self.action = {}
        self.robot_bodies, self.environment = {}, {}
        objects = {oid: world.object_info(oid)['name'] for oid in world.object_ids()}
        for geom in range(self.model.ngeom):
            name = self.model.geom(geom).name
            body = self.model.body(self.model.geom_bodyid[geom]).name
            # Attached models can leave collision geoms unnamed; their body
            # still carries the robot namespace.
            if name.startswith('robot/') or (not name and body.startswith('robot/')):
                self.robot_bodies[geom] = body
            elif name.startswith(('wall/', 'obj/', 'table/')) or name == 'table':
                label = name
                if name.startswith('obj/'):
                    label = objects.get(name.split('/')[1], name)
                self.environment[geom] = (name, label)
        self.force = np.zeros(6)

    def begin_action(self):
        self.action = {}

    def record(self):
        """Read forces before contact indices are refreshed by forward position."""
        stamp = float(self.data.time)
        pairs = {}
        for index in range(self.data.ncon):
            contact = self.data.contact[index]
            a, b = int(contact.geom1), int(contact.geom2)
            if a in self.environment:
                a, b = b, a
            if a not in self.robot_bodies or b not in self.environment or contact.efc_address < 0:
                continue
            env_geom, env_name = self.environment[b]
            key = (env_geom, self.robot_bodies[a])
            mujoco.mj_contactForce(self.model, self.data, index, self.force)
            normal = max(0., float(self.force[0]))
            if not np.isfinite(normal):
                continue
            pairs[key] = (env_name, max(normal, pairs.get(key, ('', 0.))[1]))
        if pairs:
            self.history.append((stamp, pairs))
            self._merge(self.action, stamp, pairs)
        self._prune()

    def _prune(self):
        cutoff = float(self.data.time) - self.window - 1e-9
        while self.history and self.history[0][0] < cutoff:
            self.history.popleft()

    @staticmethod
    def _merge(target, stamp, pairs):
        for (env_geom, robot_body), (env_name, force) in pairs.items():
            key = (env_geom, robot_body)
            if key not in target:
                target[key] = {'env_geom': env_geom, 'env_name': env_name,
                               'robot_body': robot_body, 'first_time': stamp,
                               'last_time': stamp, 'max_normal_force': force}
            else:
                target[key]['last_time'] = stamp
                target[key]['max_normal_force'] = max(target[key]['max_normal_force'], force)

    def recent(self):
        self._prune()
        pairs = {}
        for stamp, contacts in self.history:
            self._merge(pairs, stamp, contacts)
        return list(pairs.values())

    def during_action(self):
        return [dict(contact) for contact in self.action.values()]

    def describe_recent(self):
        contacts = self.recent()
        if not contacts:
            return ''
        # Mention the latest touch, preferring the strongest when several
        # bodies touched at the same step. The result retains the full list.
        contact = max(contacts, key=lambda c: (c['last_time'], c['max_normal_force']))
        age = max(0., float(self.data.time) - contact['last_time'])
        return f'; touched {contact["env_name"]} with {contact["robot_body"]} {age:.1f} s before'
