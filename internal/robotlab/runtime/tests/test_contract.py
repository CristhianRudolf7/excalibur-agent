from robotlab import registry


def test_every_scenario_builds_and_steps():
    for sid, cls in registry.load_all().items():
        s = cls()
        info = cls.info()
        assert info["id"] == sid
        env = s.make_env()
        obs, _ = env.reset(seed=0)
        assert env.observation_space.contains(obs) or obs.shape == env.observation_space.shape
        names = {a["name"] for a in info["actuators"]}
        assert len(names) == env.model.nu
        for _ in range(5):
            obs, r, term, trunc, extra = env.step(env.action_space.sample())
            assert "terms" in extra
