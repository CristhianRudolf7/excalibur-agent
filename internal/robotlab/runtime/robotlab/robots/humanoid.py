"""Parameterized classical walking and manipulation for a free humanoid."""
from __future__ import annotations

import math
from pathlib import Path

import mujoco
import numpy as np

from .base import Robot, Spawn, ToolSpec, json_ok, json_fail
from .humanoid_balance import LegIK, attitude, rotation, smooth


class HumanoidRobot(Robot):
    id = 'humanoid'
    name = {'en': 'Humanoid', 'es': 'Humanoide'}
    world_kind = 'room'
    default_camera = 'robot/head'

    def attach(self, spec, spawn: Spawn, menagerie: str):
        self.spawn = spawn
        child = mujoco.MjSpec.from_file(str(Path(menagerie) / 'unitree_g1/g1_with_hands.xml'))
        child.body('torso_link').add_camera(name='head', pos=[0.08, 0, 0.40],
                                          xyaxes=[0, -1, 0, 0.259, 0, 0.966], fovy=65)
        for side in ('left', 'right'):
            child.body(side + '_wrist_yaw_link').add_site(name=side + '_grasp', pos=[0.15, 0, 0], size=[0.008] * 3)
        frame = spec.worldbody.add_frame(pos=[spawn.x, spawn.y, 0],
                                        quat=[math.cos(spawn.yaw / 2), 0, 0, math.sin(spawn.yaw / 2)])
        spec.option.integrator = mujoco.mjtIntegrator.mjINT_IMPLICITFAST
        spec.attach(child, prefix='robot/', frame=frame)
        spec.option.timestep = 0.004

    def bind(self, model, data):
        self.model = model
        self.pelvis = model.body('robot/pelvis').id
        self.torso = model.body('robot/torso_link').id
        self.root_joint = model.joint('robot/floating_base_joint').id
        self.root = model.jnt_qposadr[self.root_joint]
        self.root_v = model.jnt_dofadr[self.root_joint]
        self.acts = np.array([a for a in range(model.nu) if model.actuator(a).name.startswith('robot/')])
        self.joints = model.actuator_trnid[self.acts, 0]
        self.qa = model.jnt_qposadr[self.joints]
        self.va = model.jnt_dofadr[self.joints]
        self.names = {model.joint(j).name.removeprefix('robot/'): i for i, j in enumerate(self.joints)}
        self.sites = [model.site('robot/' + s + '_foot').id for s in ('left', 'right')]
        self.key = mujoco.mj_name2id(model, mujoco.mjtObj.mjOBJ_KEY, 'robot/stand')
        self.initial = model.key_qpos[self.key].copy() if self.key >= 0 else model.qpos0.copy()
        if self.key >= 0:
            # Attached keyframes retain the source free-joint pose, without the frame transform.
            self.initial[self.root:self.root + 3] = rotation(self.spawn.yaw) @ self.initial[self.root:self.root + 3]
            self.initial[self.root:self.root + 2] += [self.spawn.x, self.spawn.y]
            quat = self.initial[self.root + 3:self.root + 7].copy()
            mujoco.mju_mulQuat(self.initial[self.root + 3:self.root + 7],
                               np.array([math.cos(self.spawn.yaw / 2), 0, 0, math.sin(self.spawn.yaw / 2)]), quat)
        self.initial[self.qa] = np.clip(self.initial[self.qa], model.jnt_range[self.joints, 0], model.jnt_range[self.joints, 1])
        if self.key < 0:
            pose = mujoco.MjData(model)
            pose.qpos[:] = self.initial
            mujoco.mj_forward(model, pose)
            foot_bodies = {int(model.site_bodyid[site]) for site in self.sites}
            soles = [pose.geom_xpos[g, 2] - model.geom_size[g, 0] for g in range(model.ngeom)
                     if model.geom_bodyid[g] in foot_bodies and model.geom_type[g] == mujoco.mjtGeom.mjGEOM_SPHERE
                     and model.geom_contype[g]]
            self.initial[self.root + 2] -= min(soles)
        self.ik = LegIK(model, self.initial, self.root, self.sites, self.joints[:12])
        self.jp = np.zeros((3, model.nv))
        self.jr = np.zeros((3, model.nv))
        self.kp = model.actuator_gainprm[self.acts, 0]
        self.mass = float(model.body_subtreemass[self.pelvis])
        self.waist_pitch = int(self.qa[self.names['waist_pitch_joint']])
        self.root_kp = np.array([40., 40., 100.])
        self.root_kd = np.array([12., 12., 20.])
        self.leg_kp = np.array([120., 120., 80., 180., 40., 40.] * 2)
        self.leg_kd = np.array([8., 8., 6., 10., 3., 3.] * 2)

    def reset(self, model, data):
        # Only robot coordinates are reset: object free joints may precede this one.
        data.qpos[self.root:self.root + 7] = self.initial[self.root:self.root + 7]
        data.qpos[self.qa] = self.initial[self.qa]
        data.qvel[self.root_v:self.root_v + 6] = 0
        data.qvel[self.va] = 0
        mujoco.mj_forward(model, data)
        self.target = data.qpos.copy()
        for offset in (0, 6):
            self.target[self.qa[offset:offset + 6]] = [-0.15, 0, 0, 0.3, -0.15, 0]
        self.feet = data.site_xpos[self.sites].copy()
        self.feet[:, 2] = 0.035
        self.foot_yaws = [self.spawn.yaw, self.spawn.yaw]
        self.yaw = self.spawn.yaw
        self.height = float(data.qpos[self.root + 2]) - 0.045
        self.body_goal = data.qpos[self.root:self.root + 3].copy()
        self.body_goal[2] = self.height
        self.com_offset = (data.subtree_com[self.pelvis] - data.xpos[self.pelvis]).copy()
        self.weights = [0.5, 0.5]
        self.swing_foot = None
        self.filtered_height = float(data.qpos[self.root + 2])
        self.depth = 0.0
        self.lean = 0.0
        self.next_control = -1.0
        self.max_tilt = 0.0
        self.hand_states = {'left': 'open', 'right': 'open'}
        self.holding_side = None
        self.holding = None
        data.ctrl[self.acts] = self.target[self.qa]

    def control(self, model, data):
        _, _, yaw = attitude(data.xmat[self.pelvis])
        roll, trunk_pitch, _ = attitude(data.xmat[self.torso])
        pitch = trunk_pitch - self.target[self.waist_pitch]
        tilt = math.acos(max(-1.0, min(1.0, data.xmat[self.torso, 8])))
        self.max_tilt = max(self.max_tilt, math.degrees(tilt))
        desired = self.body_goal.copy()
        if data.time + 1e-9 >= self.next_control:
            self.next_control = data.time + 0.01
            self.filtered_height += np.clip(self.height - self.depth - self.filtered_height, -0.001, 0.001)
            desired[2] = self.filtered_height
            crouch = max(0.0, self.height - self.filtered_height)
            desired[:2] -= (rotation(self.yaw) @ np.array([.4 * crouch, 0, 0]))[:2]
            self.target[self.waist_pitch] = 1.2 * crouch
            self.target = self.ik.solve(desired, self.yaw, self.feet, self.foot_yaws, self.target)
            if self.swing_foot is not None:
                self.target = self.ik.solve(data.qpos[self.root:self.root + 3], yaw, self.feet, self.foot_yaws, self.target,
                                            foot_index=self.swing_foot, base_quat=data.qpos[self.root + 3:self.root + 7])
        desired[2] = self.filtered_height
        # Hip retreat keeps deep crouches inside the ankle range; the waist leans forward.
        crouch = max(0.0, self.height - self.filtered_height)
        desired[:2] = self.body_goal[:2] - (rotation(self.yaw) @ np.array([.4 * crouch, 0, 0]))[:2]
        vel = data.qvel[self.root_v:self.root_v + 3]
        pos = data.qpos[self.root:self.root + 3]
        force = self.mass * (self.root_kp * (desired - pos) - self.root_kd * vel)
        force[2] += self.mass * 9.81
        force[2] = np.clip(force[2], 0, self.mass * 18)
        planar = float(np.linalg.norm(force[:2]))
        if planar > .55 * force[2]:
            force[:2] *= .55 * force[2] / max(planar, 1e-9)
        yaw_error = math.atan2(math.sin(self.yaw - yaw), math.cos(self.yaw - yaw))
        r = data.xmat[self.pelvis].reshape(3, 3)
        moment = r @ (data.qfrc_bias[self.root_v + 3:self.root_v + 6] +
                      120 * np.array([-roll, self.lean - pitch, yaw_error]) - 18 * data.qvel[self.root_v + 3:self.root_v + 6])
        weights = self.weights
        if self.swing_foot is None:
            p0, p1 = data.site_xpos[self.sites]
            line = p1[:2] - p0[:2]
            com = data.subtree_com[self.pelvis]
            zmp = com[:2] - com[2] * force[:2] / max(1.0, force[2])
            fraction = float(np.clip((zmp - p0[:2]) @ line / max(0.001, line @ line), 0, 1))
            weights = [1 - fraction, fraction]
        arms = self.target[self.qa].copy()
        torque = data.qfrc_bias[self.va].copy()
        leftover = moment.copy()
        for side, site in enumerate(self.sites):
            leftover -= np.cross(data.site_xpos[site] - pos, force * weights[side])
        for side, site in enumerate(self.sites):
            w = weights[side]
            f = force * w
            mt = leftover * w
            # A finite sole can only generate a bounded ground moment.
            local = rotation(-self.foot_yaws[side]) @ mt
            local[0] = np.clip(local[0], -0.025 * f[2], 0.025 * f[2])
            local[1] = np.clip(local[1], -0.10 * f[2], 0.045 * f[2])
            local[2] = np.clip(local[2], -0.02 * f[2], 0.02 * f[2])
            mujoco.mj_jacSite(model, data, self.jp, self.jr, site)
            torque -= self.jp[:, self.va].T @ f + self.jr[:, self.va].T @ (rotation(self.foot_yaws[side]) @ local)
        # Translate a bounded inverse-dynamics torque into the model's position targets.
        # Compensation preserves our chosen leg damping with the implicit actuator servo.
        torque[:12] += self.leg_kp * (self.target[self.qa[:12]] - data.qpos[self.qa[:12]]) - self.leg_kd * data.qvel[self.va[:12]]
        arms[:12] = data.qpos[self.qa[:12]] + (torque[:12] - model.actuator_biasprm[self.acts[:12], 2] * data.qvel[self.va[:12]]) / self.kp[:12]
        arms[12:] += data.qfrc_bias[self.va[12:]] / self.kp[12:]
        data.ctrl[self.acts] = np.clip(arms, model.actuator_ctrlrange[self.acts, 0], model.actuator_ctrlrange[self.acts, 1])

    @staticmethod
    def _number(value, minimum, maximum):
        if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value):
            raise ValueError('expected a finite number')
        return max(minimum, min(maximum, float(value)))

    @staticmethod
    def _sides(side):
        if side not in ('left', 'right', 'both'):
            raise ValueError('side must be left, right or both')
        return ('left', 'right') if side == 'both' else (side,)

    def stand(self, ctx, duration=1.0):
        duration = self._number(duration, 0.1, 10)
        recovery = self.depth / 0.07 + 0.5 if self.depth else 0
        self.depth = 0.0
        self.lean = 0.0
        self.swing_foot = None
        self.weights = [0.5, 0.5]
        self.body_goal[:2] = self.feet.mean(axis=0)[:2] - self.com_offset[:2]
        ctx.step(max(duration, recovery))
        return json_ok(state=self.state(ctx.model, ctx.data))

    def _phase(self, ctx, duration, update):
        start = float(ctx.data.time)
        while ctx.data.time - start < duration - 1e-9:
            t = min(1.0, (ctx.data.time - start + 0.02) / duration)
            update(t)
            ctx.step(min(0.02, duration - (ctx.data.time - start)))

    def _footstep(self, ctx, foot, target, foot_yaw, step_time=0.6, step_height=0.05, com_shift=0.6):
        stance = 1 - foot
        begin = self.body_goal.copy()
        center = self.feet.mean(axis=0)
        support = self.feet[stance].copy()
        support[:2] = center[:2] + (support[:2] - center[:2]) * (com_shift / 0.6)
        support[:2] -= self.com_offset[:2]
        support[0:2] += (rotation(self.yaw) @ np.array([0.015, 0, 0]))[:2]
        old_weights = self.weights.copy()

        def transfer(t):
            u = smooth(t)
            self.body_goal[:2] = begin[:2] * (1 - u) + support[:2] * u
            self.weights = [old_weights[i] * (1 - u) + float(i == stance) * u for i in (0, 1)]

        self._phase(ctx, max(0.45, 2.0 * step_time), transfer)
        self.swing_foot = foot
        first = self.feet[foot].copy()
        old_yaw = self.foot_yaws[foot]
        def swing(t):
            u = smooth(t)
            self.feet[foot] = first * (1 - u) + target * u
            self.feet[foot, 2] += step_height * math.sin(math.pi * t) ** 2
            self.foot_yaws[foot] = old_yaw * (1 - u) + foot_yaw * u
            self.yaw = sum(self.foot_yaws) / 2
        self._phase(ctx, step_time, swing)
        self.feet[foot] = target.copy()
        ctx.step(0.15)
        self.swing_foot = None
        self.weights = [0.5, 0.5]

    def walk(self, ctx, distance, step_length=0.12, step_time=0.6, step_height=0.05, com_shift=0.6, torso_lean_deg=2):
        start = ctx.data.xpos[self.pelvis].copy()
        distance = self._number(distance, -3, 3)
        step_length = self._number(step_length, 0.02, 0.35)
        step_time = self._number(step_time, 0.3, 2.0)
        step_height = self._number(step_height, .005, .12)
        com_shift = self._number(com_shift, 0, 1.2)
        self.depth = 0
        self.lean = math.radians(self._number(torso_lean_deg, -10, 15))
        direction = rotation(self.yaw) @ np.array([1.0 if distance >= 0 else -1.0, 0, 0])
        progress = [0., 0.]
        steps = 0
        self.max_tilt = 0
        deadline = ctx.data.time + 54
        while min(progress) < abs(distance) - 1e-6:
            if ctx.data.time + 3 * step_time + .15 > deadline:
                break
            foot = steps % 2
            dest = min(abs(distance), max(progress) + step_length)
            target = self.feet[foot].copy() + direction * (dest - progress[foot])
            self._footstep(ctx, foot, target, self.yaw, step_time, step_height, com_shift)
            progress[foot] = dest
            steps += 1
        self.stand(ctx, 1.0)
        achieved = float((ctx.data.xpos[self.pelvis] - start) @ direction)
        fields = dict(steps_done=steps, distance_achieved=achieved, error_m=abs(distance) - achieved,
                      max_tilt_deg=self.max_tilt, state=self.state(ctx.model, ctx.data))
        if min(progress) < abs(distance) - 1e-6:
            return json_fail('simulation time budget reached', **fields)
        if abs(abs(distance) - achieved) > .15:
            return json_fail('distance outside tolerance', **fields)
        return json_ok(**fields)

    def turn(self, ctx, angle_deg, step_time=0.6):
        angle = math.radians(self._number(angle_deg, -180, 180))
        step_time = self._number(step_time, .3, 2)
        self.lean = 0
        self.depth = 0
        start_yaw = attitude(ctx.data.xmat[self.pelvis])[2]
        center = self.feet.mean(axis=0)
        original = self.feet.copy()
        original_yaws = self.foot_yaws.copy()
        n = math.ceil(abs(angle) / math.radians(15))
        steps = 0
        deadline = ctx.data.time + 54
        for k in range(1, n + 1):
            delta = angle * k / n
            for foot in (0, 1):
                if ctx.data.time + 3 * step_time + .15 > deadline:
                    self.stand(ctx)
                    return json_fail('simulation time budget reached', steps_done=steps, state=self.state(ctx.model, ctx.data))
                target = center + rotation(delta) @ (original[foot] - center)
                self._footstep(ctx, foot, target, original_yaws[foot] + delta, step_time)
                steps += 1
        self.stand(ctx, 1)
        end_yaw = attitude(ctx.data.xmat[self.pelvis])[2]
        achieved = math.degrees(math.atan2(math.sin(end_yaw - start_yaw), math.cos(end_yaw - start_yaw)))
        error = math.degrees(math.atan2(math.sin(angle - math.radians(achieved)), math.cos(angle - math.radians(achieved))))
        fields = dict(angle_achieved_deg=achieved, error_deg=error, steps_done=steps,
                      state=self.state(ctx.model, ctx.data))
        return json_ok(**fields) if abs(error) <= 15 else json_fail('turn outside tolerance', **fields)

    def step(self, ctx, foot, dx, dy):
        if foot not in ('left', 'right'):
            raise ValueError('foot must be left or right')
        dx = self._number(dx, -.25, .25)
        dy = self._number(dy, -.15, .15)
        index = 0 if foot == 'left' else 1
        r = rotation(self.yaw)
        target = self.feet[index] + r @ np.array([dx, dy, 0])
        if abs(dy) > 1e-6 and abs(dx) < 1e-6:
            self.max_tilt = 0
            # Bend the knees before widening the stance: an extended trailing
            # leg otherwise runs out of reach during the second COM transfer.
            self.squat(ctx, .04)
            self._shuffle(ctx, dy, .8)
            self.stand(ctx, 1)
            error = float(np.linalg.norm(ctx.data.site_xpos[self.sites[index]] - target))
            fields = dict(foot=foot, foot_position=ctx.data.site_xpos[self.sites[index]].tolist(),
                          error_m=error, max_tilt_deg=self.max_tilt, state=self.state(ctx.model, ctx.data))
            return json_ok(**fields) if error <= .05 else json_fail('foot placement outside tolerance', **fields)
        separation = (r.T @ (target - self.feet[1 - index]))[1] * (1 if index == 0 else -1)
        if not .10 <= separation <= .40:
            return json_fail('foot separation must stay between 0.10 and 0.40 m')
        self.lean = 0
        self.depth = 0
        self._footstep(ctx, index, target, self.yaw)
        self.stand(ctx, 1)
        error = float(np.linalg.norm(ctx.data.site_xpos[self.sites[index]] - target))
        fields = dict(foot=foot, foot_position=ctx.data.site_xpos[self.sites[index]].tolist(),
                      error_m=error, state=self.state(ctx.model, ctx.data))
        return json_ok(**fields) if error <= .05 else json_fail('foot placement outside tolerance', **fields)

    def _shuffle(self, ctx, distance, step_time):
        """Lead outwards, transfer onto the landed foot, then close the stance."""
        lead = 0 if distance > 0 else 1
        delta = rotation(self.yaw) @ np.array([0, distance, 0])
        targets = self.feet + delta
        for index in (lead, 1 - lead):
            self._footstep(ctx, index, targets[index], self.yaw, step_time)

    def side_step(self, ctx, distance, step_width=.10, step_time=.8):
        distance = self._number(distance, -1, 1)
        step_width = self._number(step_width, .02, .15)
        step_time = self._number(step_time, .8, 2)
        start = ctx.data.xpos[self.pelvis].copy()
        left = rotation(self.yaw) @ np.array([0., 1., 0.])
        deadline = ctx.data.time + 54
        progress = 0.
        shuffles = 0
        self.max_tilt = 0
        if abs(distance) > 1e-6:
            self.squat(ctx, .04)
            while progress < abs(distance) - 1e-6:
                # Reserve time for both feet so a partial action ends with a
                # normal stance, then settle before the core's 60 s limit.
                if ctx.data.time + 2 * (3 * step_time + .16) > deadline:
                    break
                width = min(step_width, abs(distance) - progress)
                self._shuffle(ctx, math.copysign(width, distance), step_time)
                progress += width
                shuffles += 1
            self.stand(ctx)
        achieved = float((ctx.data.xpos[self.pelvis] - start) @ left)
        error = distance - achieved
        fields = dict(distance_achieved=achieved, error_m=error, max_tilt_deg=self.max_tilt,
                      steps_done=2 * shuffles, shuffles_done=shuffles,
                      step_width=step_width, step_time=step_time, state=self.state(ctx.model, ctx.data))
        if progress < abs(distance) - 1e-6:
            return json_fail('simulation time budget reached', **fields)
        return json_ok(**fields) if abs(error) <= .05 else json_fail('distance outside tolerance', **fields)

    def walk_to(self, ctx, x, y, **params):
        allowed = {'step_length': (.02, .35), 'step_time': (.3, 2), 'step_height': (.005, .12),
                   'com_shift': (0, 1.2), 'torso_lean_deg': (-10, 15)}
        if params.keys() - allowed.keys():
            raise ValueError('unknown walking parameter')
        params = {key: self._number(value, *allowed[key]) for key, value in params.items()}
        x, y = self._number(x, -15, 15), self._number(y, -15, 15)
        delta = np.array([x, y]) - ctx.data.xpos[self.pelvis, :2]
        distance = float(np.linalg.norm(delta))
        if distance > 3:
            return json_fail('target is more than 3 m away', error_m=distance, state=self.state(ctx.model, ctx.data))
        if distance < .03:
            return json_ok(distance_achieved=0., steps_done=0, error_m=distance, state=self.state(ctx.model, ctx.data))
        bearing = math.atan2(delta[1], delta[0])
        yaw = attitude(ctx.data.xmat[self.pelvis])[2]
        angle = math.degrees(math.atan2(math.sin(bearing - yaw), math.cos(bearing - yaw)))
        if abs(angle) > 2:
            turned = self.turn(ctx, angle, step_time=params.get('step_time', .6))
            if not turned['ok']:
                return json_fail('could not face target', turn=turned, state=self.state(ctx.model, ctx.data))
        result = self.walk(ctx, distance, **params)
        error = float(np.linalg.norm(ctx.data.xpos[self.pelvis, :2] - [x, y]))
        result['error_m'] = error
        result['target'] = [x, y]
        if error > .25:
            result.update(ok=False, error='target outside 0.25 m tolerance')
        return result

    def squat(self, ctx, depth):
        depth = self._number(depth, 0, .25)
        self.lean = 0
        self.swing_foot = None
        self.weights = [.5, .5]
        self.body_goal[:2] = self.feet.mean(axis=0)[:2] - self.com_offset[:2]
        old = self.depth
        self.depth = depth
        ctx.step(abs(depth - old) / .07 + 1)
        error = self.height - depth - ctx.data.xpos[self.pelvis, 2]
        fields = dict(depth_achieved=self.height - float(ctx.data.xpos[self.pelvis, 2]), error_m=float(error),
                      state=self.state(ctx.model, ctx.data))
        return json_ok(**fields) if abs(error) < .04 else json_fail('squat outside tolerance', **fields)

    def _arm_target(self, ctx, sides, values):
        qa, starts, ends = [], [], []
        joint_names = ('shoulder_pitch', 'shoulder_roll', 'shoulder_yaw', 'elbow', 'wrist_roll', 'wrist_pitch', 'wrist_yaw')
        for side in sides:
            for j, value in zip(joint_names, values[side]):
                i = self.names[side + '_' + j + '_joint']
                q = self.qa[i]
                qa.append(q)
                starts.append(self.target[q])
                ends.append(float(np.clip(value, *ctx.model.jnt_range[self.joints[i]])))
        starts, ends = np.array(starts), np.array(ends)
        duration = max(.8, float(np.max(np.abs(ends - starts))) * 1.5 / 1.2)
        self._phase(ctx, duration, lambda t: self.target.__setitem__(qa, starts + smooth(t) * (ends - starts)))
        ctx.step(.4)
        return float(np.max(np.abs(ctx.data.qpos[qa] - ends)))

    def move_arm(self, ctx, side, pose=None, joints_deg=None):
        sides = self._sides(side)
        if (pose is None) == (joints_deg is None):
            raise ValueError('provide exactly one of pose or joints_deg')
        if pose is not None and pose not in ('down', 'forward', 'up', 'wave'):
            raise ValueError('unknown arm pose')
        if joints_deg is not None:
            if not isinstance(joints_deg, (tuple, list)) or len(joints_deg) != 7:
                raise ValueError('joints_deg must contain seven arm angles')
            angles = [math.radians(self._number(v, -180, 180)) for v in joints_deg]
            values = {s: angles for s in sides}
        else:
            poses = {'down': [.2, .2, 0, 1.28, 0, 0, 0],
                     'forward': [-1.3, .2, 0, 1.28, 0, 0, 0],
                     'up': [-2.5, .7, 0, .7, 0, 0, 0],
                     'wave': [-1.8, .7, 0, 1.2, 0, 0, 0]}
            values = {s: poses[pose].copy() for s in sides}
            for s in sides:
                if s == 'right':
                    values[s][1] *= -1
        error = self._arm_target(ctx, sides, values)
        if pose == 'wave':
            for sign in (1, -1, 1, 0):
                for s in sides:
                    values[s][4] = sign * .55
                error = self._arm_target(ctx, sides, values)
        fields = dict(side=side, pose=pose or 'joints', error_deg=math.degrees(error), state=self.state(ctx.model, ctx.data))
        return json_ok(**fields) if error < math.radians(20) else json_fail('arm outside tolerance', **fields)

    def _try_grasp(self, ctx, sides, objects):
        if ctx.held() is not None:
            return
        for side in sides:
            sid = ctx.model.site('robot/' + side + '_grasp').id
            for obj in objects:
                pos, _ = ctx.object_pose(obj['id'])
                rel = ctx.data.site_xmat[sid].reshape(3, 3).T @ (pos - ctx.data.site_xpos[sid])
                if np.linalg.norm(rel) <= .04 and abs(rel[1]) <= .035 and abs(rel[2]) <= .035:
                    ctx.hold(obj['id'], 'robot/' + side + '_grasp')
                    self.holding_side = side
                    self.holding = obj['id']
                    return

    def hand(self, ctx, side, state):
        sides = self._sides(side)
        if state not in ('open', 'close'):
            raise ValueError('hand state must be open or close')
        if state == 'open' and self.holding_side in sides:
            ctx.release()
            self.holding_side = None
            self.holding = None
        qa, starts, ends = [], [], []
        for hand_side in sides:
            sign = -1 if hand_side == 'left' else 1
            for joint in ('thumb_0', 'thumb_1', 'thumb_2', 'middle_0', 'middle_1', 'index_0', 'index_1'):
                i = self.names[hand_side + '_hand_' + joint + '_joint']
                if state == 'open':
                    angle = -sign * 1.04 if joint == 'thumb_1' else 0
                else:
                    angle = {'thumb_0': .4, 'thumb_1': -sign * .3, 'thumb_2': -sign * 1.0}.get(joint, sign * 1.1)
                qa.append(self.qa[i])
                starts.append(self.target[self.qa[i]])
                ends.append(float(np.clip(angle, *ctx.model.jnt_range[self.joints[i]])))
            self.hand_states[hand_side] = state
        starts, ends = np.array(starts), np.array(ends)
        objects = sorted((o for o in ctx.objects() if o.get('dynamic', False)), key=lambda item: item['id'])
        def close_or_open(t):
            self.target[qa] = starts + smooth(t) * (ends - starts)
            # Test during closure so contact does not knock a small object out first.
            if state == 'close' and t > 0.02:
                self._try_grasp(ctx, sides, objects)
        self._phase(ctx, 1.2, close_or_open)
        if state == 'close':
            self._try_grasp(ctx, sides, objects)
        self.holding = ctx.held()
        return json_ok(side=side, hand=state, holding=self.holding, state=self.state(ctx.model, ctx.data))

    def tools(self):
        def number(lo, hi, default=None):
            out = {'type': 'number', 'minimum': lo, 'maximum': hi}
            if default is not None:
                out['default'] = default
            return out

        def schema(properties, required=()):
            return {'type': 'object', 'properties': properties, 'required': list(required), 'additionalProperties': False}

        sides = {'type': 'string', 'enum': ['left', 'right', 'both']}
        walk_params = {'step_length': number(.02, .35, .12), 'step_time': number(.3, 2, .6),
                       'step_height': number(.005, .12, .05), 'com_shift': number(0, 1.2, .6),
                       'torso_lean_deg': number(-10, 15, 2)}
        arm = schema({'side': sides, 'pose': {'type': 'string', 'enum': ['down', 'forward', 'up', 'wave']},
                      'joints_deg': {'type': 'array', 'minItems': 7, 'maxItems': 7, 'items': number(-180, 180)}}, ['side'])
        arm['oneOf'] = [{'required': ['pose'], 'not': {'required': ['joints_deg']}},
                        {'required': ['joints_deg'], 'not': {'required': ['pose']}}]
        definitions = [
            ('stand', 'Balance standing / Mantener el equilibrio de pie', schema({'duration': number(.1, 10, 1)}), self.stand),
            ('walk', 'Walk a signed distance / Caminar una distancia con signo',
             schema({'distance': number(-3, 3), **walk_params}, ['distance']), self.walk),
            ('turn', 'Turn by stepping in place / Girar con pasos en el sitio',
             schema({'angle_deg': number(-180, 180), 'step_time': number(.3, 2, .6)}, ['angle_deg']), self.turn),
            ('step', 'Place a foot; pure lateral moves shuffle both feet / Colocar un pie; los movimientos laterales desplazan ambos pies',
             schema({'foot': {'type': 'string', 'enum': ['left', 'right']}, 'dx': number(-.25, .25), 'dy': number(-.15, .15)},
                    ['foot', 'dx', 'dy']), self.step),
            ('side_step', 'Shuffle sideways keeping heading; positive is left / Desplazarse lateralmente sin girar; positivo hacia la izquierda',
             schema({'distance': number(-1, 1), 'step_width': number(.02, .15, .10),
                     'step_time': number(.8, 2, .8)}, ['distance']), self.side_step),
            ('walk_to', 'Face and walk to world coordinates / Orientarse y caminar a coordenadas del mundo',
             schema({'x': number(-15, 15), 'y': number(-15, 15), **walk_params}, ['x', 'y']), self.walk_to),
            ('squat', 'Lower the pelvis / Bajar la pelvis', schema({'depth': number(0, .25)}, ['depth']), self.squat),
            ('move_arm', 'Move seven arm joints / Mover siete articulaciones del brazo', arm, self.move_arm),
            ('hand', 'Open or close a hand / Abrir o cerrar una mano',
             schema({'side': sides, 'state': {'type': 'string', 'enum': ['open', 'close']}}, ['side', 'state']), self.hand),
        ]
        return {name: ToolSpec(name, description, params, fn) for name, description, params, fn in definitions}

    def card(self):
        return {
            'id': self.id, 'name': self.name,
            'description': 'Walk, turn, squat, position arms and grasp small objects / Caminar, girar, agacharse, mover brazos y sujetar objetos pequeños',
            'sensors': ['Trunk attitude / Actitud del tronco', 'Joint positions / Posiciones articulares',
                        'Foot contacts / Contactos de los pies', 'Center of mass / Centro de masa'],
            'cameras': [self.default_camera],
            'tools': [{'name': tool.name, 'description': tool.description, 'params': tool.params} for tool in self.tools().values()] +
                     [{'name': 'look', 'description': 'Camera view / Vista de cámara',
                       'params': {'type': 'object', 'properties': {'camera': {'type': 'string', 'enum': [self.default_camera]}}}}],
            'limits': {'minimum_pelvis_height_m': .45, 'maximum_trunk_tilt_deg': 45,
                       'walk_distance_m': [-3, 3], 'step_length_m': [.02, .35], 'step_time_s': [.3, 2],
                       'step_height_m': [.005, .12], 'squat_depth_m': [0, .25], 'arm_target_speed_rad_s': 1.2,
                       'foot_separation_m': [.10, .40], 'grasp_tolerance_m': .04,
                       'side_step_distance_m': [-1, 1], 'side_step_width_m': [.02, .15], 'side_step_time_s': [.8, 2],
                       'maximum_verified_lateral_step_m': .15, 'recommended_walk_step_length_m': .12,
                       'shoulder_arm_half_width_m': .28, 'recommended_wall_clearance_m': .45,
                       'joint_limits': 'Model position and force limits / Límites articulares y de fuerza del modelo'},
            'notes': [
                'Walk, turn and side_step stop after a 54 s simulation budget and settle before returning a partial result; '
                'the action limit is 60 s. / Caminar, girar y side_step se detienen tras 54 s de simulación y estabilizan '
                'el cuerpo antes de devolver un resultado parcial; el límite de la acción es 60 s.',
                'Default gait: step_length=0.12, step_time=0.6, step_height=0.05, com_shift=0.6, torso_lean_deg=2. '
                'Flat floor only; collisions and obstacles can cause falls. / Marcha predeterminada: step_length=0.12, '
                'step_time=0.6, step_height=0.05, com_shift=0.6, torso_lean_deg=2. Solo suelo plano; los obstáculos pueden causar caídas.',
                'step_time is the swing duration; each step also takes 2*step_time to transfer weight and 0.15 s to land. '
                'com_shift scales the lateral transfer: 0.6 targets the stance foot; smaller values leave COM between the feet. / '
                'step_time es la duración del balanceo; cada paso añade 2*step_time para transferir el peso y 0.15 s para apoyar. '
                'com_shift escala la transferencia lateral: 0.6 apunta al pie de apoyo; valores menores dejan el centro de masa entre los pies.',
                'Pure lateral step (dx=0) shuffles both feet, with the outside foot leading, regardless of foot. '
                'Verified through dy=+/-0.15 m per shuffle on flat ground; knees bend 0.04 m for reach. '
                'side_step keeps heading, distance is signed (+left), step_width clamps to 0.02..0.15 m and step_time to 0.8..2 s. / '
                'El paso lateral puro (dx=0) desplaza ambos pies, empezando por el exterior, independientemente de foot. '
                'Verificado hasta dy=+/-0.15 m por desplazamiento en suelo plano; las rodillas bajan 0.04 m para mantener el alcance. '
                'side_step mantiene el rumbo, distance lleva signo (+izquierda), step_width se limita a 0.02..0.15 m y step_time a 0.8..2 s.',
                'Measured shoulder/arm half-width with default arms: 0.275 m from pelvis center (allow 0.28 m); '
                'arms can brush a parallel wall when the pelvis is closer than 0.28 m. Keep the planned pelvis path at least '
                '0.45 m from walls to allow about 0.12 m of lateral COM sway and margin; raised or extended arms need more space. / '
                'Semiancho medido de hombros/brazos en la postura predeterminada: 0.275 m desde el centro de la pelvis '
                '(reservar 0.28 m); los brazos pueden rozar una pared paralela si la pelvis pasa a menos de 0.28 m. '
                'Mantener la trayectoria prevista de la pelvis al menos a 0.45 m de las paredes para permitir unos 0.12 m '
                'de oscilación lateral del centro de masa y un margen; los brazos elevados o extendidos requieren más espacio.',
                'Use the default 0.12 m walking step for distance. Very small steps (step_length<0.08) are slow, '
                'about 0.03 m/s at step_length=0.06 and default timing; long walks reach the 60 s action limit '
                '(the controller returns partial progress before then). / Usar el paso predeterminado de 0.12 m para recorrer distancia. '
                'Los pasos muy pequeños (step_length<0.08) son lentos, unos 0.03 m/s con step_length=0.06 y el tiempo predeterminado; '
                'las caminatas largas alcanzan el límite de acción de 60 s (el controlador devuelve el avance parcial antes).',
                'Arm joints_deg order: shoulder pitch, roll, yaw, elbow, wrist roll, pitch, yaw. Values are clamped to joint limits. / '
                'Orden de joints_deg: hombro pitch, roll, yaw, codo, muñeca roll, pitch, yaw. Se aplican los límites articulares.',
                'Grasp assist is a simulator convenience: close with a dynamic object center within 4 cm of robot/left_grasp or '
                'robot/right_grasp and between the fingers. Open releases the object held by that hand. / '
                'La asistencia de agarre es una ayuda del simulador: cerrar con el centro de un objeto dinámico a menos de 4 cm de '
                'robot/left_grasp o robot/right_grasp y entre los dedos. Abrir libera el objeto de esa mano.',
                'Measured on a flat floor: step_length=0.35 with step_time=0.3 falls; do not use these for a safe first walk. / '
                'Medido en suelo plano: step_length=0.35 con step_time=0.3 causa una caída; evitar estos valores en la primera marcha.',
                'Walking parameter ranges are experimental limits, not safe combinations: longer steps need more transfer time; '
                'high step_height increases swing acceleration, com_shift far from 0.6 misplaces COM, and extreme torso_lean_deg shifts balance. '
                'Prefer the default step_length=0.12, step_time=0.6, step_height=0.05, com_shift=0.6, torso_lean_deg=2. / '
                'Los rangos de marcha son límites experimentales, no combinaciones seguras: los pasos largos necesitan más tiempo de transferencia; '
                'un step_height alto aumenta la aceleración del pie, com_shift lejos de 0.6 desplaza mal el centro de masa y torso_lean_deg extremo altera el equilibrio. '
                'Preferir los valores predeterminados step_length=0.12, step_time=0.6, step_height=0.05, com_shift=0.6, torso_lean_deg=2.',
                'Use stand to rise after squat. A fall ends the attempt; reset before the next action. / '
                'Usar stand para levantarse tras squat. Una caída termina el intento; reiniciar antes de otra acción.',
            ],
        }

    def fallen(self, model, data):
        if not np.isfinite(data.qpos[self.root:self.root + 7]).all():
            return 'non-finite pelvis pose'
        if data.xpos[self.pelvis, 2] < 0.45:
            return 'pelvis below 0.45 m'
        if data.xmat[self.torso, 8] < math.cos(math.pi / 4):
            return 'trunk tilt exceeds 45 degrees'
        return None

    def state(self, model, data):
        _, _, yaw = attitude(data.xmat[self.pelvis])
        xyz = data.xpos[self.pelvis]
        return {'x': float(xyz[0]), 'y': float(xyz[1]), 'z': float(xyz[2]), 'yaw_deg': math.degrees(yaw),
                'tilt_deg': math.degrees(math.acos(max(-1.0, min(1.0, data.xmat[self.torso, 8])))),
                'hands': self.hand_states.copy(), 'holding_side': self.holding_side, 'holding': self.holding,
                'squat_depth_m': max(0.0, float(self.height - xyz[2]))}
