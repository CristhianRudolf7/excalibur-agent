"""Small primitive-only world used by runtime subprocess integration tests."""
import numpy as np
from robotlab.registry import register
from robotlab.scenario import Actuator, RewardTerm, RobotEnv, Scenario


class TinyEnv(RobotEnv):
    def __init__(self, scenario, **kwargs):
        super().__init__(scenario, scenario.params, frame_skip=1, max_episode_steps=32, **kwargs)

    def _observation(self):
        return np.concatenate([self.data.qpos, self.data.qvel]).astype(np.float32)

    def _reward_terms(self, action):
        return {'balance': 1.0 - float(self.data.qpos[0] ** 2)}


@register
class Tiny(Scenario):
    id = '_core_test'
    title = {'en': 'Test pendulum', 'es': 'Test pendulum'}
    actuators = [Actuator('motor', title, 'torque', [-1, 1])]
    reward_terms = [RewardTerm('balance', 1.0, title)]
    default_params = {'timestep': 0.01}

    def build_xml(self, params):
        return f'''<mujoco><option timestep="{params['timestep']}"/><visual><global offwidth="640" offheight="480"/></visual>
        <worldbody><light pos="0 -2 4"/><geom type="plane" size="0 0 .1" rgba=".2 .2 .2 1"/>
        <camera name="front" pos="0 -3 2" xyaxes="1 0 0 0 .55 .83"/>
        <body name="red" pos="-.4 0 .3"><joint name="hinge" type="hinge" axis="0 1 0" damping="1"/>
        <geom name="red_geom" type="box" size=".18 .18 .18" rgba="1 0 0 1"/></body>
        <body name="blue" pos=".4 0 .3"><geom name="blue_geom" type="box" size=".18 .18 .18" rgba="0 0 1 1"/></body>
        </worldbody><actuator><motor name="motor" joint="hinge" ctrlrange="-1 1"/></actuator></mujoco>'''

    def make_env(self, reward_weights=None, randomize=False, render_mode=None):
        return TinyEnv(self, reward_weights=reward_weights, randomize=randomize)

    def marks(self, env):
        return [{'name': 'red', 'label': 'red cube', 'body': 'red'},
                {'name': 'blue', 'label': 'blue cube', 'geom': 'blue_geom'}]

    def skill(self, env, name, args):
        for _ in range(int(args.get('steps', 3))):
            env.step(np.zeros(1))
            yield
        return 'skill complete'

    def pose_mapping(self):
        return lambda pose, state: {'motor': pose.get('value', 0.0)}
