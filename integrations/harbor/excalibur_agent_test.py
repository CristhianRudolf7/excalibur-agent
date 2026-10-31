"""Unit tests for the Harbor installed-route adapter.

Stdlib unittest only; no Harbor installation and no paid APIs.
A fake excalibur binary (shell script) stands in for the static build.
"""

import json
import os
import stat
import sys
import tempfile
import unittest

os.environ["EXCALIBUR_TEST_NO_HARBOR"] = "1"
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from excalibur_agent import (
    ExcaliburAgent,
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


if __name__ == "__main__":
    unittest.main()
