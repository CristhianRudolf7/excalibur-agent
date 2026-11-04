"""Validated low-level keyframes; no locomotion behaviours or hidden plans."""
from __future__ import annotations

import math

import numpy as np

from .base import ToolSpec
from . import dog_sensors as sensors


def finite(value, name='number'):
    if isinstance(value, bool):
        raise ValueError(name+' must be a finite number')
    value = float(value)
    if not math.isfinite(value):
        raise ValueError(name+' must be a finite number')
    return value


def bounded(value, low, high, name):
    if value is None:
        raise ValueError(f'{name} is required (seconds) in every keyframe')
    value = finite(value, name)
    if not low <= value <= high:
        raise ValueError(f'{name} must be between {low} and {high}')
    return value


def smooth(u):
    return u*u*u*(10+u*(-15+6*u))


class DogTools:
    def _snapshot(self, data):
        if self.mode == 'feet':
            return self.target.copy(), self.foot_target.copy(), self.trunk_target.copy()
        return self.target.copy(), data.geom_xpos[self.feet].copy(), self.pose(data)

    def _feet_goal(self, feet, trunk, frame, start_feet, start_trunk):
        from .dog import rotation
        if frame not in ('world', 'body'):
            raise ValueError('frame must be world or body')
        if not isinstance(feet, dict) or not isinstance(trunk, dict):
            raise ValueError('feet and trunk must be objects')
        desired = start_trunk.copy()
        fields = ('x', 'y', 'z', 'roll', 'pitch', 'yaw')
        for name, value in trunk.items():
            if name not in fields:
                raise ValueError('Unknown trunk field: '+name)
            i = fields.index(name)
            desired[i] = finite(value, name)*(math.pi/180 if i >= 3 else 1)
        end = start_feet.copy()
        for leg, value in feet.items():
            if leg not in self.legs:
                raise ValueError('Unknown leg: '+leg)
            if not isinstance(value, (list, tuple)) or len(value) != 3:
                raise ValueError('Foot target must have three coordinates')
            p = np.array([finite(v, 'foot coordinate') for v in value])
            if frame == 'body':
                p = desired[:3]+rotation(desired) @ p
            end[self.legs.index(leg)] = p
        return end, desired

    def _joint_feet(self, data, q, trunk, scratch=None):
        """Predict joint-keyframe feet without changing live state or targets."""
        import mujoco
        from .dog import rotation
        if scratch is None:
            scratch = mujoco.MjData(self._model)
            scratch.qpos[:] = data.qpos
        scratch.qpos[self.lq] = q
        quat = np.empty(4)
        mujoco.mju_mat2Quat(quat, rotation(trunk).ravel())
        scratch.qpos[self.rq:self.rq+3] = trunk[:3]
        scratch.qpos[self.rq+3:self.rq+7] = quat
        mujoco.mj_forward(self._model, scratch)
        return scratch.geom_xpos[self.feet].copy(), scratch

    def _landing_preflight(self, data, keyframes):
        """Report resolvable targets even when timing or reachability rejects a call."""
        reports = []
        if not isinstance(keyframes, list):
            return reports
        q, feet, trunk = self._snapshot(data)
        scratch = None
        for k, key in enumerate(keyframes):
            if not isinstance(key, dict):
                continue
            try:
                if key.get('type') == 'feet':
                    end, desired = self._feet_goal(key.get('feet', {}), key.get('trunk', {}),
                                                  key.get('frame', 'world'), feet, trunk)
                    q, _ = self.solve(end, desired)
                    feet, trunk = end, desired
                elif key.get('type') == 'joints':
                    endq = q.copy()
                    for name, value in key['joints'].items():
                        endq[self.joint_names.index(name)] = math.radians(finite(value, name))
                    feet, scratch = self._joint_feet(data, endq, trunk, scratch)
                    q = endq
                else:
                    continue
            except (ValueError, TypeError, OverflowError, KeyError, AttributeError):
                # Malformed coordinates have no world point to raycast. Continue
                # reporting later resolvable keyframes; validation still rejects.
                continue
            reports.append(dict(keyframe=k, legs={
                leg: dict(foot_world=point.round(3).tolist(), **sensors.landing(self._model, data, point))
                for leg, point in zip(self.legs, feet)}))
        return reports

    def _prepare(self, data, keyframes, settle, stop_if):
        settle = bounded(settle, 0, 2, 'settle')
        if not isinstance(keyframes, list) or not 1 <= len(keyframes) <= 16:
            raise ValueError('keyframes must contain 1 to 16 entries')
        if not isinstance(stop_if, dict) or set(stop_if)-{'margin_below', 'contact'}:
            raise ValueError('Invalid stop_if')
        if 'margin_below' in stop_if:
            finite(stop_if['margin_below'], 'margin_below')
        if 'contact' in stop_if and not isinstance(stop_if['contact'], bool):
            raise ValueError('contact must be boolean')
        q, feet, trunk = self._snapshot(data)
        planned, errors, total = [], [], settle
        scratch = None
        for k, key in enumerate(keyframes):
            if not isinstance(key, dict):
                raise ValueError('Each keyframe must be an object')
            duration = bounded(key.get('duration'), .002, 30, 'duration')
            total += duration
            kind = key.get('type')
            allowed = {'type', 'duration', 'joints'} if kind == 'joints' else {'type', 'duration', 'feet', 'frame', 'trunk'}
            if set(key)-allowed:
                raise ValueError('Unknown keyframe fields')
            if kind == 'joints':
                if not isinstance(key.get('joints'), dict):
                    raise ValueError('joints must be an object')
                endq = q.copy()
                for name, value in key['joints'].items():
                    if name not in self.joint_names:
                        raise ValueError('Unknown joint: '+name)
                    i = self.joint_names.index(name)
                    endq[i] = math.radians(finite(value, name))
                    if not self.lrange[i, 0] <= endq[i] <= self.lrange[i, 1]:
                        errors.append(dict(keyframe=k, joint=name, error='Joint limit exceeded'))
                planned.append(dict(type=kind, duration=duration, q0=q.copy(), q1=endq.copy()))
                q = endq
                # Forward kinematics predicts the feet after an explicit joint keyframe.
                feet, scratch = self._joint_feet(data, q, trunk, scratch)
            elif kind == 'feet':
                end, desired = self._feet_goal(key.get('feet', {}), key.get('trunk', {}), key.get('frame', 'world'), feet, trunk)
                plan = dict(type=kind, duration=duration, f0=feet.copy(), f1=end.copy(), p0=trunk.copy(), p1=desired.copy())
                # Validate every control tick of the complete Cartesian path, including
                # omitted legs; an unreachable later keyframe rejects the entire call.
                for u in np.linspace(0, 1, max(2, int(math.ceil(duration/self._model.opt.timestep))+1)):
                    s = smooth(float(u))
                    q, details = self.solve(feet+(end-feet)*s, trunk+(desired-trunk)*s)
                    bad = [dict(keyframe=k, leg=leg, **info) for leg, info in details.items() if not info['reachable']]
                    if bad:
                        errors.extend(bad)
                        break
                planned.append(plan)
                feet, trunk = end, desired
            else:
                raise ValueError('Keyframe type must be joints or feet')
        if total > 30+1e-9:
            raise ValueError('Total duration including settle exceeds 30 s')
        return planned, settle, errors

    def _stop_reason(self, model, data, stop_if, initial_contacts):
        if 'margin_below' in stop_if:
            if sensors.support(self, model, data)[1] < float(stop_if['margin_below']):
                return 'margin_below'
        if stop_if.get('contact') and sensors.external_contacts(self, model, data)[1]-initial_contacts:
            return 'contact'
        return None

    def sequence(self, ctx, keyframes, stop_if=None, settle=.2):
        self._model = ctx.model
        stop_if = {} if stop_if is None else stop_if
        preflight = self._landing_preflight(ctx.data, keyframes)

        def result(**fields):
            return dict(**fields, preflight=preflight, state=self.state(ctx.model, ctx.data))

        try:
            plans, settle, errors = self._prepare(ctx.data, keyframes, settle, stop_if)
            if errors:
                for info in errors:
                    if 'leg' in info:
                        report = next((p for p in preflight if p['keyframe'] == info['keyframe']), None)
                        if report:
                            info.update(report['legs'][info['leg']])
                return result(ok=False, error='Unreachable target or joint limit', details=errors)
        except (ValueError, TypeError, OverflowError) as exc:
            return result(ok=False, error=str(exc), details=[])
        initial_contacts = sensors.external_contacts(self, ctx.model, ctx.data)[1]
        dt = float(ctx.model.opt.timestep)
        for k, plan in enumerate(plans):
            reason = self._stop_reason(ctx.model, ctx.data, stop_if, initial_contacts)
            if reason:
                return result(ok=True, stopped_at=k, reason=reason)
            ticks = max(1, int(math.ceil(plan['duration']/dt)))
            for tick in range(1, ticks+1):
                s = smooth(tick/ticks)
                self.mode = plan['type']
                if self.mode == 'joints':
                    self.target = plan['q0']+(plan['q1']-plan['q0'])*s
                else:
                    self.foot_target = plan['f0']+(plan['f1']-plan['f0'])*s
                    self.trunk_target = plan['p0']+(plan['p1']-plan['p0'])*s
                ctx.step(dt)
                reason = self._stop_reason(ctx.model, ctx.data, stop_if, initial_contacts)
                if reason:
                    return result(ok=True, stopped_at=k, reason=reason)
        for _ in range(int(round(settle/dt))):
            ctx.step(dt)
            reason = self._stop_reason(ctx.model, ctx.data, stop_if, initial_contacts)
            if reason:
                return result(ok=True, stopped_at=len(plans)-1, reason=reason)
        return result(ok=True)

    def move_joints(self, ctx, joints, duration, settle=.2):
        return self.sequence(ctx, [dict(type='joints', joints=joints, duration=duration)], settle=settle)

    def move_feet(self, ctx, feet, duration, frame='world', trunk=None, settle=.2):
        return self.sequence(ctx, [dict(type='feet', feet=feet, frame=frame, trunk={} if trunk is None else trunk, duration=duration)], settle=settle)

    def ik(self, ctx, feet, frame='world', trunk=None):
        try:
            _, start, pose = self._snapshot(ctx.data)
            goal, desired = self._feet_goal(feet, {} if trunk is None else trunk, frame, start, pose)
            _, legs = self.solve(goal, desired)
            for leg, point in zip(self.legs, goal):
                legs[leg].update(sensors.landing(ctx.model, ctx.data, point))
            return dict(ok=True, legs=legs, state=self.state(ctx.model, ctx.data))
        except (ValueError, TypeError, OverflowError) as exc:
            return dict(ok=False, error=str(exc), details=[], state=self.state(ctx.model, ctx.data))

    def wait(self, ctx, seconds):
        try:
            seconds = bounded(seconds, .05, 5, 'seconds')
        except (ValueError, TypeError, OverflowError) as exc:
            return dict(ok=False, error=str(exc), details=[], state=self.state(ctx.model, ctx.data))
        ctx.step(seconds)
        return dict(ok=True, state=self.state(ctx.model, ctx.data))

    def tools(self):
        def obj(properties, required=()):
            return dict(type='object', properties=properties, required=list(required), additionalProperties=False)

        def num(low, high):
            return dict(type='number', minimum=low, maximum=high)

        joints = obj({leg+'_'+part: num(-180, 260) for leg in self.legs for part in self.parts})
        feet = obj({leg: dict(type='array', items=dict(type='number'), minItems=3, maxItems=3) for leg in self.legs})
        trunk = obj({k: dict(type='number') for k in ('x', 'y', 'z', 'roll', 'pitch', 'yaw')})
        cart = dict(feet=feet, frame=dict(type='string', enum=['world', 'body'], default='world'), trunk=trunk)
        timing = dict(duration=num(.002, 30), settle={**num(0, 2), 'default': .2})
        joint_key = obj(dict(type=dict(const='joints'), joints=joints, duration=num(.002, 30)), ['type', 'joints', 'duration'])
        foot_key = obj(dict(type=dict(const='feet'), **cart, duration=num(.002, 30)), ['type', 'feet', 'duration'])
        specs = [
            ('move_joints', 'Interpolar articulaciones en grados / Interpolate joint targets in degrees', obj(dict(joints=joints, **timing), ['joints', 'duration'])),
            ('move_feet', 'Interpolar pies y tronco; pies omitidos fijos en el mundo / Interpolate feet and trunk; omitted feet stay fixed in world', obj(dict(**cart, **timing), ['feet', 'duration'])),
            ('sequence', 'Ejecutar 1–16 fotogramas, máximo 30 s con asentamiento / Execute 1–16 keyframes, maximum 30 s including settle', obj(dict(keyframes=dict(type='array', minItems=1, maxItems=16, items=dict(oneOf=[joint_key, foot_key])), stop_if=obj(dict(margin_below=dict(type='number'), contact=dict(type='boolean'))), settle=timing['settle']), ['keyframes'])),
            ('ik', 'Comprobar alcance sin avanzar tiempo / Check reachability without advancing time', obj(cart, ['feet'])),
            ('wait', 'Mantener objetivos actuales / Hold current targets', obj(dict(seconds=num(.05, 5)), ['seconds']))]
        return {name: ToolSpec(name, description, schema, getattr(self, name)) for name, description, schema in specs}

    def card(self):
        limits = {leg+'_'+part: bounds for leg in self.legs for part, bounds in zip(self.parts, ([-60., 60.], [-90., 200.] if leg.startswith('F') else [-30., 260.], [-156., -48.]))}
        return dict(id=self.id, name=self.name,
                    description='Cuadrúpedo Go2 sin brazo; control de bajo nivel / Go2 quadruped without arm; low-level control',
                    sensors=['Pose y velocidad / Pose and velocity', 'COM, polígono de apoyo y margen firmado / COM, support polygon and signed margin', 'Contactos y fuerza por pie / Per-foot contacts and force', 'Rayos de terreno y bordes / Terrain rays and drop-off edges'],
                    cameras=['robot/head', 'robot/rear'],
                    tools=[dict(name=t.name, description=t.description, params=t.params) for t in self.tools().values()]+[dict(name='look', description='Ver cámara / View camera', params=dict(type='object', properties={'camera': {'type': 'string'}}))],
                    limits=dict(joints_deg=limits, links_m=dict(thigh=.213, calf=.213, hip_offset=.0955, foot_radius=.022),
                                hip_origins_body_m=dict(zip(self.legs, [[.1934, .0465, 0], [.1934, -.0465, 0], [-.1934, .0465, 0], [-.1934, -.0465, 0]])),
                                reference_pose=dict(trunk_z=.27, hip_deg=0., thigh_deg=51.6, calf_deg=-103.1),
                                leg_workspace_m=dict(max_hip_to_foot=.437, sagittal_max=.426, lateral_offset=.0955)),
                    notes=[
                        'Ángulos en grados, distancias en metros, tiempo simulado; z de pie es el centro de la esfera de radio 0.022 m / Angles in degrees, lengths in meters, simulation time; foot z is the center of its 0.022 m radius sphere',
                        'Antes de levantar un pie, desplaza el COM dentro del triángulo de los otros tres pies con margen positivo; mantén tres pies apoyados / Before lifting a foot, shift the COM inside the triangle of the other three feet with positive margin; keep three feet in contact',
                        'Con cuatro pies apoyados, lift por pata indica margin firmado respecto al triángulo de los otros tres, triangle_center (incentro) y shift_needed (desplazamiento mínimo del COM para margen de 0.04 m), en coordenadas mundiales / With four feet in contact, lift per leg reports signed margin for the other three feet, triangle_center (incenter) and shift_needed (minimum COM shift for 0.04 m margin), in world coordinates',
                        'Con tres apoyos, lift del pie aéreo indica aerial=true y support_margin del triángulo actual; los pies apoyados indican margin=null porque levantar otro dejaría solo dos apoyos; con menos de tres, lift={} / With three contacts, lift for the airborne foot reports aerial=true and support_margin of the current triangle; stance feet report margin=null because lifting another would leave only two contacts; with fewer than three, lift={}',
                        'Si el triángulo es demasiado estrecho para 0.04 m, shift_needed apunta al incentro, el mejor margen posible / If the triangle is too narrow for 0.04 m, shift_needed points to the incenter, the best possible margin',
                        'Para desplazar el tronco con pies plantados, usa move_feet con feet={} y trunk; los pies omitidos conservan sus objetivos mundiales / To move the trunk with planted feet, use move_feet with feet={} and trunk; omitted feet keep their world targets',
                        'Mueve solo trunk con move_feet y feet={}, sumando aproximadamente lift.<leg>.shift_needed a x,y del tronco; el COM sigue aproximadamente al tronco. Comprueba lift.<leg>.margin antes de levantar ese pie / Move only trunk with move_feet and feet={}, adding approximately lift.<leg>.shift_needed to trunk x,y; COM moves roughly with the trunk. Check lift.<leg>.margin before lifting that foot',
                        'Sube el pie lo suficiente para superar el borde del escalón antes de avanzar; coloca los pies al menos a 0.05 m de los bordes laterales / Lift the foot high enough to clear the step edge before moving forward; place feet at least 0.05 m from side edges',
                        'Por pata, edge_ahead.dx mide desde el pie hasta el siguiente cambio de altura >0.03 m en la dirección horizontal del tronco (hasta 0.6 m); z_after es la altura tras el borde; ambos son null si no hay borde / Per leg, edge_ahead.dx measures from the foot to the next height change >0.03 m along the horizontal trunk heading (up to 0.6 m); z_after is the height beyond the edge; both are null when no edge is found',
                        'edge_side.left y edge_side.right son distancias desde el pie a caídas laterales >0.03 m, a izquierda y derecha del rumbo del tronco hasta 0.4 m; null indica que no se detectó caída / edge_side.left and edge_side.right are distances from the foot to lateral drops >0.03 m, left and right of the trunk heading up to 0.4 m; null means no drop was detected',
                        'Antes de llevar un pie a un escalón, consulta ik del objetivo: lands_on identifica el objeto o floor, terrain_z es la altura bajo él y clearance=z objetivo-terrain_z; clearance negativo está dentro de la geometría. Para apoyar la esfera, sitúa su centro unos 0.022 m sobre terrain_z / Before swinging a foot onto a step, check ik for the target: lands_on identifies the object or floor, terrain_z is the height beneath it and clearance=target z-terrain_z; negative clearance is inside geometry. To land the sphere, place its center about 0.022 m above terrain_z',
                        'Usa edge_ahead.dx para elegir un objetivo al menos 0.05 m después del borde: avanza desde el pie edge_ahead.dx+0.05 m o más en la dirección horizontal del tronco; confirma lands_on y terrain_z con ik antes de mover / Use edge_ahead.dx to choose a target at least 0.05 m past the edge: advance from the foot by edge_ahead.dx+0.05 m or more along the horizontal trunk heading; confirm lands_on and terrain_z with ik before moving',
                        'move_feet y sequence devuelven preflight por keyframe (índice desde cero), con legs y foot_world, terrain_z, lands_on, clearance por objetivo, incluidos pies omitidos; también al rechazar el plan o detenerlo / move_feet and sequence return preflight per keyframe (zero-based index), with legs and foot_world, terrain_z, lands_on, clearance for each target, including omitted feet; also when rejecting or stopping the plan',
                        'El espacio de trabajo está limitado por dos eslabones de 0.213 m, separación lateral 0.0955 m y límites articulares; usa ik antes de mover / Workspace is limited by two 0.213 m links, 0.0955 m lateral offset and joint limits; use ik before moving',
                        'Planifica fotogramas pequeños; consulta support, margin, terrain_ahead, edges y robot/rear para los pies traseros / Plan small keyframes; read support, margin, terrain_ahead, edges and robot/rear for hind feet',
                        'Principio de la marcha lenta (no es una secuencia): un pie solo alcanza unos 0.15-0.20 m delante de su cadera; para llegar más lejos hay que avanzar el cuerpo. Ciclo típico: desplazar el tronco hacia el triángulo de los otros tres pies, adelantar un pie unos 0.08-0.12 m, apoyarlo, mover el tronco hacia adelante con los pies plantados, y repetir con el siguiente pie (orden habitual RL, FL, RR, FR) / Slow crawl principle (not a sequence): a foot only reaches about 0.15-0.20 m ahead of its hip; to go further the body must advance. Typical cycle: shift the trunk toward the triangle of the other three feet, move one foot about 0.08-0.12 m forward, plant it, move the trunk forward with the feet planted, then repeat with the next foot (usual order RL, FL, RR, FR)',
                        'Para apoyar un pie, apunta a z = terrain_z + 0.004 a 0.008: los pies de pie descansan con el centro a unos 0.004 m sobre el terreno porque el contacto es blando; un objetivo con clearance ≥ 0.02 deja el pie flotando sin contacto / To plant a foot, target z = terrain_z + 0.004 to 0.008: standing feet rest with their center about 0.004 m above the terrain because contacts are soft; a target with clearance ≥ 0.02 leaves the foot hovering without contact',
                        'No hay marcha automática; levantar un pie sin equilibrio o pisar fuera de la escalera puede causar una caída / There is no automatic gait; lifting a foot without balance or stepping outside the stairs can cause a fall'])
