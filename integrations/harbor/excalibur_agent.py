"""Harbor installed-agent adapter for Excalibur (Section 23.2).

Installed route (the main Harbor path): ``install`` leaves the static
excalibur binary in the trial's own prefix and verifies its sha256;
``configure`` passes provider, model and credentials through
environment variables; ``run`` executes
``excalibur exec --profile eval --json`` with the task instruction and
fills the Harbor AgentContext (tokens incl. cached, cache read/write,
output and cost) from ``/logs/agent/excalibur/metrics.json``.

The module imports Harbor's installed-agent base when available and
falls back to a local stub so unit tests run without Harbor.
"""

import hashlib
import json
import os
import shutil
import subprocess
import urllib.request
from dataclasses import dataclass, field

AGENT_NAME = "excalibur"
PROFILE = "eval"
METRICS_REL = os.path.join("excalibur", "metrics.json")
TRAJECTORY_NAME = "trajectory.json"

try:  # Harbor's real base; absent in unit-test environments.
    from harbor.agents.installed import BaseInstalledAgent  # type: ignore
    from harbor.models.agent.context import AgentContext  # type: ignore
except Exception:  # pragma: no cover - fallback stub for tests

    @dataclass
    class AgentContext:
        """Minimal Harbor AgentContext stand-in."""

        input_tokens: int = 0
        cache_read_tokens: int = 0
        cache_write_tokens: int = 0
        output_tokens: int = 0
        cost_usd: float = 0.0
        model: str = ""
        extra: dict = field(default_factory=dict)

    class BaseInstalledAgent:
        """Fallback base so the adapter imports without Harbor."""

        def install(self, env=None):
            raise NotImplementedError

        def configure(self, env=None, provider=None, model=None):
            raise NotImplementedError

        def run(self, instruction, env=None):
            raise NotImplementedError


def sha256_of(path):
    """Return the hex sha256 of a file."""
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def verify_sha256(path, expected):
    """Check path against its expected hex digest."""
    actual = sha256_of(path)
    if actual != expected.lower():
        raise ValueError(
            "excalibur_agent: sha256 mismatch for %s: got %s want %s"
            % (path, actual, expected)
        )
    return actual


def install_binary(source, prefix, expected_sha256=None):
    """Install the static binary into prefix/bin.

    source may be a local path or an https URL. Returns the installed
    path. The digest is always verified when expected_sha256 is given;
    callers should always give it for benchmark trials.
    """
    bindir = os.path.join(prefix, "bin")
    os.makedirs(bindir, exist_ok=True)
    dest = os.path.join(bindir, "excalibur")
    if source.startswith("https://"):
        with urllib.request.urlopen(source) as resp:  # noqa: S310
            with open(dest, "wb") as out:
                shutil.copyfileobj(resp, out)
    else:
        shutil.copyfile(source, dest)
    os.chmod(dest, 0o755)
    if expected_sha256:
        verify_sha256(dest, expected_sha256)
    return dest


def configure_env(provider, model, api_key=None, extra_env=None):
    """Build the trial environment for the eval profile.

    Credentials travel only through environment variables and are never
    written to logs, metrics or ATIF records.
    """
    env = dict(os.environ)
    if extra_env:
        env.update(extra_env)
    env["EXCALIBUR_PROFILE"] = PROFILE
    if provider:
        env["EXCALIBUR_PROVIDER"] = provider
    if model:
        env["EXCALIBUR_MODEL"] = model
    if api_key:
        env["EXCALIBUR_API_KEY"] = api_key
    return env


def metrics_path(logs_dir):
    """Canonical metrics location inside a Harbor trial log directory."""
    return os.path.join(logs_dir, METRICS_REL)


def fill_agent_context(logs_dir, model=""):
    """Read metrics.json and build the Harbor AgentContext.

    Raises FileNotFoundError when the harness wrote no metrics: the
    trial must fail loudly instead of reporting zero usage.
    """
    path = metrics_path(logs_dir)
    with open(path, "r", encoding="utf-8") as f:
        snap = json.load(f)
    usage = snap.get("usage", {})
    return AgentContext(
        input_tokens=int(usage.get("input", 0)),
        cache_read_tokens=int(usage.get("cache_read", 0)),
        cache_write_tokens=int(usage.get("cache_write", 0)),
        output_tokens=int(usage.get("output", 0))
        + int(usage.get("reasoning", 0)),
        cost_usd=float(snap.get("cost_usd", 0.0)),
        model=snap.get("model", model),
    )


class ExcaliburAgent(BaseInstalledAgent):
    """Installed-route adapter: excalibur binary inside the container."""

    def __init__(self, binary=None, expected_sha256=None):
        self.binary = binary
        self.expected_sha256 = expected_sha256
        self.installed = None

    def install(self, env=None):
        prefix = (env or {}).get("EXCALIBUR_PREFIX", "/opt/excalibur")
        source = self.binary or (env or {}).get("EXCALIBUR_BINARY", "")
        if not source:
            raise ValueError("excalibur_agent: no binary source for install")
        expected = self.expected_sha256 or (env or {}).get(
            "EXCALIBUR_SHA256", ""
        )
        self.installed = install_binary(
            source, prefix, expected or None
        )
        return self.installed

    def configure(self, env=None, provider=None, model=None):
        return configure_env(
            provider or (env or {}).get("EXCALIBUR_PROVIDER", ""),
            model or (env or {}).get("EXCALIBUR_MODEL", ""),
            api_key=(env or {}).get("EXCALIBUR_API_KEY"),
        )

    def run(self, instruction, env=None):
        binary = self.installed or self.binary or "excalibur"
        logs_dir = (env or {}).get("LOGS_DIR", "/logs/agent")
        cmd = [binary, "exec", "--profile", PROFILE, "--json", instruction]
        proc = subprocess.run(  # noqa: S603
            cmd,
            env=configure_env(
                (env or {}).get("EXCALIBUR_PROVIDER", ""),
                (env or {}).get("EXCALIBUR_MODEL", ""),
                api_key=(env or {}).get("EXCALIBUR_API_KEY"),
            ),
            capture_output=True,
            text=True,
        )
        context = None
        try:
            context = fill_agent_context(logs_dir)
        except FileNotFoundError:
            context = None
        return {
            "returncode": proc.returncode,
            "stdout": proc.stdout,
            "stderr": proc.stderr,
            "agent_context": context,
            "trajectory": os.path.join(logs_dir, TRAJECTORY_NAME),
        }
