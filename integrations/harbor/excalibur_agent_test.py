"""Unit tests for the Harbor installed-route adapter.

Stdlib unittest only; no Harbor installation and no paid APIs.
A fake excalibur binary (shell script) stands in for the static build.
"""

import ast
import asyncio
import json
import os
import stat
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace as Obj
from unittest.mock import patch

os.environ["EXCALIBUR_TEST_NO_HARBOR"] = "1"
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from excalibur_agent import (
    ExcaliburAgent,
    resolve_agent_timeout,
    agent_timeout_argument,
    configure_env,
    ensure_harbor_trajectory,
    fill_agent_context,
    install_binary,
    metrics_path,
    verify_sha256,
    sha256_of,
)


def write_fake_binary(path, logs_dir):
    script = (
        "#!/bin/sh\n"
        "mkdir -p '%s/excalibur'\n" % logs_dir
        + "cat > '%s/excalibur/metrics.json' <<'EOF'\n" % logs_dir
        + '{"usage": {"input": 1000, "cache_read": 300, "cache_write": 100,'
        + ' "output": 200, "reasoning": 50}, "cost_usd": 0.05,'
        + ' "model": "test-model"}\n'
        + "EOF\n"
        + "echo '{\"status\": \"completed\"}'\n"
    )
    with open(path, "w", encoding="utf-8") as f:
        f.write(script)
    os.chmod(path, os.stat(path).st_mode | stat.S_IEXEC)


class TestInstall(unittest.TestCase):
    def test_install_and_verify(self):
        with tempfile.TemporaryDirectory() as tmp:
            src = os.path.join(tmp, "excalibur")
            with open(src, "wb") as f:
                f.write(b"fake-binary")
            digest = sha256_of(src)
            dest = install_binary(src, os.path.join(tmp, "prefix"), digest)
            self.assertTrue(os.path.exists(dest))
            self.assertEqual(verify_sha256(dest, digest), digest)
            with self.assertRaises(ValueError):
                verify_sha256(dest, "0" * 64)

    def test_agent_install_needs_source(self):
        agent = ExcaliburAgent()
        with self.assertRaises(ValueError):
            agent.install(env={})


class TestConfigure(unittest.TestCase):
    def test_env_carries_provider_model(self):
        env = configure_env("openai", "gpt-x", api_key="k", extra_env={"A": "b"})
        self.assertEqual(env["EXCALIBUR_PROFILE"], "eval")
        self.assertEqual(env["EXCALIBUR_PROVIDER"], "openai")
        self.assertEqual(env["EXCALIBUR_MODEL"], "gpt-x")
        self.assertEqual(env["EXCALIBUR_API_KEY"], "k")
        self.assertEqual(env["A"], "b")


class TestRun(unittest.TestCase):
    def test_run_fills_agent_context(self):
        with tempfile.TemporaryDirectory() as tmp:
            logs = os.path.join(tmp, "logs")
            os.makedirs(logs)
            fake = os.path.join(tmp, "excalibur")
            write_fake_binary(fake, logs)
            agent = ExcaliburAgent(binary=fake)
            env = {
                "LOGS_DIR": logs,
                "EXCALIBUR_PROVIDER": "openai",
                "EXCALIBUR_MODEL": "test-model",
            }
            result = agent.run("do the thing", env=env)
            self.assertEqual(result["returncode"], 0)
            ctx = result["agent_context"]
            self.assertIsNotNone(ctx)
            self.assertEqual(ctx.n_input_tokens, 1000)
            self.assertEqual(ctx.n_cache_tokens, 400)
            # Output includes reasoning.
            self.assertEqual(ctx.n_output_tokens, 250)
            self.assertAlmostEqual(ctx.cost_usd, 0.05)
            self.assertTrue(result["trajectory"].endswith("trajectory.json"))

    def test_missing_metrics_is_loud(self):
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(FileNotFoundError):
                fill_agent_context(tmp)

    def test_metrics_path(self):
        self.assertEqual(
            metrics_path("/logs/agent"), "/logs/agent/excalibur/metrics.json"
        )

    def test_metrics_json_shape(self):
        with tempfile.TemporaryDirectory() as tmp:
            os.makedirs(os.path.join(tmp, "excalibur"))
            path = os.path.join(tmp, "excalibur", "metrics.json")
            snap = {
                "usage": {"input": 10, "cache_read": 2, "cache_write": 1,
                          "output": 3, "reasoning": 4},
                "cost_usd": 0.01,
                "model": "m",
            }
            with open(path, "w", encoding="utf-8") as f:
                json.dump(snap, f)
            ctx = fill_agent_context(tmp)
            self.assertEqual(ctx.n_output_tokens, 7)
            self.assertEqual(ctx.n_cache_tokens, 3)

    def test_ensure_trajectory_fallback(self):
        with tempfile.TemporaryDirectory() as tmp:
            os.makedirs(os.path.join(tmp, "excalibur"))
            path = os.path.join(tmp, "excalibur", "metrics.json")
            with open(path, "w", encoding="utf-8") as f:
                json.dump({"usage": {"input": 1, "output": 1}, "cost_usd": 0.0}, f)
            traj = ensure_harbor_trajectory(tmp, model="m")
            self.assertTrue(os.path.exists(traj))


class TestRunnerTimeout(unittest.TestCase):
    def setUp(self):
        self.env_patch = patch.dict(os.environ, {}, clear=True)
        self.env_patch.start()
        self.addCleanup(self.env_patch.stop)

    def test_operator_precedence_and_invalid_values(self):
        agent = Obj(_ex_timeout_hints={"agent_timeout_sec": 4000})
        os.environ["EXCALIBUR_TIMEOUT_SEC"] = "2000"
        self.assertEqual(resolve_agent_timeout(agent), (2000, "operator:EXCALIBUR_TIMEOUT_SEC"))
        self.assertEqual(resolve_agent_timeout(agent, extra={"HARBOR_AGENT_TIMEOUT_SEC": "3000"}),
                         (3000, "operator:HARBOR_AGENT_TIMEOUT_SEC"))
        del os.environ["EXCALIBUR_TIMEOUT_SEC"]
        for raw in ("bad", "nan", "inf", "-1", "0", True):
            with self.subTest(raw=raw):
                self.assertEqual(resolve_agent_timeout(agent, extra={"HARBOR_AGENT_TIMEOUT_SEC": raw}),
                                 (4000, "agent kwargs:agent_timeout_sec"))

    def test_resolved_objects(self):
        for environment, agent, source in (
            (Obj(), Obj(_ex_timeout_hints={"agent_timeout_sec": 3600}), "agent kwargs:agent_timeout_sec"),
            (Obj(trial=Obj(agent_timeout_sec=3600)), Obj(), "trial:agent_timeout_sec"),
            (Obj(_agent_timeout_sec=3600), Obj(), "environment:_agent_timeout_sec"),
        ):
            with self.subTest(source=source):
                self.assertEqual(resolve_agent_timeout(agent, environment), (3600, source))

    def test_task_directory_and_saved_trial_semantics(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            task = root / "task"
            (task / "environment").mkdir(parents=True)
            (task / "task.toml").write_text("[agent]\ntimeout_sec = 2400\n")
            trial = root / "trial"
            trial.mkdir()
            environment = Obj(environment_dir=task / "environment", trial_paths=Obj(trial_dir=trial))
            self.assertEqual(resolve_agent_timeout(Obj(), environment), (2400, "task.toml"))
            cases = (
                ({}, 2400),
                ({"timeout_multiplier": 2}, 4800),
                ({"timeout_multiplier": 3, "agent_timeout_multiplier": 0.5}, 1200),
                ({"agent": {"override_timeout_sec": 4000, "max_timeout_sec": 3000},
                  "agent_timeout_multiplier": 2}, 6000),
                ({"agent": {"override_timeout_sec": 1000}}, 1000),
            )
            for config, expected in cases:
                with self.subTest(config=config):
                    (trial / "config.json").write_text(json.dumps(config))
                    self.assertEqual(resolve_agent_timeout(Obj(), environment),
                                     (expected, "trial config.json + task.toml"))
            # Logs alone can locate the trial config and its materialized task.
            (trial / "config.json").write_text(json.dumps({"task": {"path": str(task)}}))
            self.assertEqual(resolve_agent_timeout(Obj(logs_dir=trial / "agent"))[0], 2400)
            (trial / "config.json").write_text("invalid")
            self.assertIsNone(resolve_agent_timeout(Obj(), environment)[0])

    def test_config_objects_and_missing_invalid_values(self):
        task = Obj(config=Obj(agent=Obj(timeout_sec=5000)))
        config = Obj(agent=Obj(max_timeout_sec=3000), timeout_multiplier=2)
        environment = Obj(trial=Obj(config=config, task=task))
        self.assertEqual(resolve_agent_timeout(Obj(), environment), (6000, "trial config"))
        self.assertEqual(resolve_agent_timeout(Obj(), Obj()), (None, "none"))
        for raw in ("garbage", "nan", "inf", 0, -5, True):
            task.config.agent.timeout_sec = raw
            self.assertIsNone(resolve_agent_timeout(Obj(), environment)[0])
        with tempfile.TemporaryDirectory() as tmp:
            for text in ("broken", "[agent]\ntimeout_sec = -1", "[agent]\ntimeout_sec = 'bad'", ""):
                Path(tmp, "task.toml").write_text(text)
                self.assertIsNone(resolve_agent_timeout(Obj(_ex_timeout_hints={"task_dir": tmp}))[0])

    def test_installed_run_passes_resolved_flag(self):
        # Exercise the real installed run method without importing Harbor.
        import excalibur_agent as adapter
        tree = ast.parse(Path(adapter.__file__).read_text())
        cls = next(node for node in ast.walk(tree)
                   if isinstance(node, ast.ClassDef) and node.name == "ExcaliburAgent"
                   and any(isinstance(n, ast.AsyncFunctionDef) for n in node.body))
        method = next(n for n in cls.body if isinstance(n, ast.AsyncFunctionDef) and n.name == "run")
        namespace = dict(vars(adapter))
        exec(compile(ast.Module(body=[method], type_ignores=[]), adapter.__file__, "exec"), namespace)
        commands = []

        async def execute(environment, command, **kwargs):
            commands.append(command)
            return Obj(return_code=0, stdout='{"usage": {}}')

        with tempfile.TemporaryDirectory() as tmp:
            agent = Obj(_ex_prefix="/opt/excalibur", model_name="local/model", _extra_env={},
                        logs_dir=Path(tmp), _ex_timeout_hints={}, exec_as_agent=execute)
            environment = Obj(agent_timeout_sec=2400)
            asyncio.run(namespace["run"](agent, "write the requested output", environment, Obj()))
            self.assertIn(" --timeout 2160 ", commands[0])
            self.assertIn("source=environment:agent_timeout_sec", Path(tmp, "setup.log").read_text())

    def test_closing_argument_setup_log_and_switch(self):
        with tempfile.TemporaryDirectory() as tmp:
            agent = Obj(logs_dir=Path(tmp), _ex_timeout_hints={"agent_timeout_sec": 3600})
            self.assertEqual(agent_timeout_argument(agent), " --timeout 3240")
            log = Path(tmp, "setup.log").read_text()
            self.assertIn("outer_seconds=3600.0 source=agent kwargs:agent_timeout_sec", log)
            self.assertIn("--timeout 3240", log)
            self.assertEqual(agent_timeout_argument(agent, extra={"EXCALIBUR_RUNNER_TIMEOUT": "0"}), "")
            self.assertEqual(agent_timeout_argument(agent, extra={"EXCALIBUR_RUNNER_TIMEOUT": "0",
                                                                 "EXCALIBUR_TIMEOUT_SEC": "1000"}), " --timeout 900")


if __name__ == "__main__":
    unittest.main()
