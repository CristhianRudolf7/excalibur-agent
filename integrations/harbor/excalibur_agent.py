"""Harbor installed-agent adapter for Excalibur (Section 23.2).

Pinned Harbor: 0.25.0 (git fdba28fc411e47e8765420a1a063b4918fce3def, tag v0.25.0).

Installed route (the main Harbor path): ``install`` uploads the static
excalibur binary into the trial container and verifies its sha256;
``run`` executes ``excalibur exec --profile eval`` with the task
instruction and fills the Harbor AgentContext from
``/logs/agent/excalibur/metrics.json``. Credentials travel only through
environment variables and are never written to logs, metrics or ATIF.

Registration (verified against Harbor 0.25.0): custom agents are NOT
built-in; they are resolved by import path ``module.path:ClassName``
(AgentFactory.get_agent_class_from_config). Set PYTHONPATH to this
directory and pass ``-a excalibur_agent:ExcaliburAgent``.
See docs/decisions/ for the four resolved Section 23.2 items
(registration, timeout, ATIF version, AgentContext).
"""

import hashlib
import json
import os
import shlex
import shutil
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path, PurePosixPath

AGENT_NAME = "excalibur"
PROFILE = "eval"
METRICS_REL = os.path.join("excalibur", "metrics.json")
TRAJECTORY_NAME = "trajectory.json"

if os.environ.get("EXCALIBUR_TEST_NO_HARBOR") == "1":
    _HARBOR = False
else:
    try:  # Harbor's real base; absent in unit-test environments.
        from harbor.agents.capabilities import AgentCapabilities  # type: ignore
        from harbor.agents.installed.base import BaseInstalledAgent  # type: ignore
        from harbor.agents.agent_auth_resolution import (  # type: ignore
            ModelConnectionSpec,
            ProviderAccess,
        )
        from harbor.models.agent.context import AgentContext  # type: ignore
        from harbor.models.agent.name import AgentName  # type: ignore
        from harbor.models.trajectories import (  # type: ignore
            Agent as AtifAgent,
            FinalMetrics as AtifFinalMetrics,
            Step as AtifStep,
            Trajectory as AtifTrajectory,
        )
        _HARBOR = True
    except Exception:  # pragma: no cover - fallback stub for tests
        _HARBOR = False
if not _HARBOR:  # fallback definitions below

    @dataclass
    class AgentContext:  # type: ignore
        """Minimal Harbor AgentContext stand-in (new field names)."""

        n_input_tokens: int | None = None
        n_cache_tokens: int | None = None
        n_output_tokens: int | None = None
        cost_usd: float | None = None
        model: str = ""
        extra: dict = field(default_factory=dict)

        def is_empty(self):  # pragma: no cover
            return all(
                v is None
                for v in (
                    self.n_input_tokens,
                    self.n_cache_tokens,
                    self.n_output_tokens,
                    self.cost_usd,
                )
            )

    class BaseInstalledAgent:  # type: ignore
        """Fallback base so the adapter imports without Harbor."""

        def __init__(self, *args, **kwargs):
            self._extra_env = dict(kwargs.get("extra_env") or {})
            self.model_name = kwargs.get("model_name")
            self.logs_dir = Path(kwargs.get("logs_dir", "/tmp"))

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

    source may be a local path or an https URL. The digest is always
    verified when expected_sha256 is given; callers should always give
    it for benchmark trials.
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


def _read_metrics(logs_dir):
    path = metrics_path(logs_dir)
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def fill_agent_context(logs_dir, model=""):
    """Read metrics.json and build the Harbor AgentContext.

    Uses the Harbor 0.25.0 field names (n_input_tokens incl. cached,
    n_cache_tokens, n_output_tokens, cost_usd). Raises FileNotFoundError
    when the harness wrote no metrics: the trial must fail loudly
    instead of reporting zero usage.
    """
    snap = _read_metrics(logs_dir)
    usage = snap.get("usage", {})
    cache_read = int(usage.get("cache_read", 0))
    cache_write = int(usage.get("cache_write", 0))
    ctx = AgentContext(
        n_input_tokens=int(usage.get("input", 0)),
        n_cache_tokens=cache_read + cache_write,
        n_output_tokens=int(usage.get("output", 0))
        + int(usage.get("reasoning", 0)),
        cost_usd=float(snap.get("cost_usd", 0.0)),
    )
    # model_usage stays None: Harbor backfills it from the ATIF
    # trajectory, and manual plain-dict values trip Pydantic warnings.
    return ctx


def ensure_harbor_trajectory(logs_dir, model=""):
    """Ensure <logs>/trajectory.json is Harbor-ATIF-valid.

    The Go harness already writes a minimal ATIF-v1.8 file there on the
    installed route. If it is missing or invalid (killed trial before
    the first flush, old binary), write a minimal valid document from
    metrics so the trial still validates.
    """
    path = os.path.join(logs_dir, TRAJECTORY_NAME)
    if _HARBOR:
        try:
            raw = Path(path).read_text(encoding="utf-8")
            AtifTrajectory.model_validate_json(raw)
            return path
        except Exception:
            pass
    else:
        if os.path.exists(path):
            return path
    try:
        snap = _read_metrics(logs_dir)
        usage = snap.get("usage", {})
        prompt = int(usage.get("input", 0))
        completion = int(usage.get("output", 0)) + int(usage.get("reasoning", 0))
        cached = int(usage.get("cache_read", 0)) + int(usage.get("cache_write", 0))
        cost = float(snap.get("cost_usd", 0.0))
        model_name = snap.get("model") or model or None
    except Exception:
        prompt, completion, cached, cost, model_name = 0, 0, 0, 0.0, model or None
    if _HARBOR:
        doc = AtifTrajectory(
            agent=AtifAgent(name="excalibur", version="dev", model_name=model_name),
            steps=[AtifStep(step_id=1, source="agent", message="excalibur trial")],
            final_metrics=AtifFinalMetrics(
                total_prompt_tokens=prompt,
                total_completion_tokens=completion,
                total_cached_tokens=cached,
                total_cost_usd=cost if cost else None,
            ),
        )
        Path(logs_dir).mkdir(parents=True, exist_ok=True)
        Path(path).write_text(json.dumps(doc.to_json_dict(), indent=2) + "\n", encoding="utf-8")
    else:
        Path(logs_dir).mkdir(parents=True, exist_ok=True)
        Path(path).write_text(
            json.dumps({"schema_version": "ATIF-v1.8", "steps": []}) + "\n", encoding="utf-8"
        )
    return path


if _HARBOR:

    class ExcaliburOptions(__import__("pydantic").BaseModel):  # type: ignore
        model_config = {"extra": "allow"}
        binary: str | None = None
        expected_sha256: str | None = None
        install_prefix: str | None = None

    class ExcaliburAgent(BaseInstalledAgent):  # type: ignore
        """Installed-route adapter: excalibur binary inside the container.

        Harbor 0.25.0 resolves this class by import path
        ``excalibur_agent:ExcaliburAgent`` with PYTHONPATH pointing at
        this directory (AgentFactory import-path route, not built-in).
        """

        capabilities = AgentCapabilities(atif=True, base_url=True, reasoning_effort=True)
        MODEL_CONNECTION = ModelConnectionSpec(
            default_provider="openai",
            agent_provider_access=ProviderAccess(
                api_key_candidate_names=["EXCALIBUR_API_KEY", "OPENAI_API_KEY"],
                base_url_candidate_names=["EXCALIBUR_BASE_URL", "OPENAI_BASE_URL"],
            ),
        )

        def __init__(self, *args, **kwargs):
            self._ex_binary = kwargs.pop("binary", None) or os.environ.get("EXCALIBUR_BINARY")
            self._ex_sha256 = kwargs.pop("expected_sha256", None) or os.environ.get("EXCALIBUR_SHA256")
            self._ex_prefix = kwargs.pop("install_prefix", None) or "/opt/excalibur"
            super().__init__(*args, **kwargs)

        @staticmethod
        def name() -> str:
            try:
                return AgentName("excalibur").value  # type: ignore
            except Exception:
                return "excalibur"

        def version(self) -> str | None:
            return None

        def get_version_command(self) -> str | None:
            return "/opt/excalibur/bin/excalibur --help"

        async def install(self, environment) -> None:
            prefix = self._ex_prefix
            source = self._ex_binary or os.environ.get("EXCALIBUR_BINARY", "")
            expected = self._ex_sha256 or os.environ.get("EXCALIBUR_SHA256", "")
            if not source:
                raise ValueError("excalibur_agent: no binary source for install (set binary kwarg or EXCALIBUR_BINARY)")
            await self.exec_as_root(environment, command=f"mkdir -p {shlex.quote(prefix + '/bin')}")
            # Upload the host static binary into the container, then
            # verify its sha256 inside the container.
            await environment.upload_file(source, prefix + "/bin/excalibur")
            await self.exec_as_root(
                environment, command=f"chmod 755 {shlex.quote(prefix + '/bin/excalibur')}"
            )
            if expected:
                result = await self.exec_as_agent(
                    environment,
                    command=f"sha256sum {shlex.quote(prefix + '/bin/excalibur')} | cut -d' ' -f1",
                )
                actual = (result.stdout or "").strip().lower()
                if actual != expected.lower():
                    raise ValueError(
                        f"excalibur_agent: sha256 mismatch: got {actual} want {expected}"
                    )

        async def run(self, instruction: str, environment, context) -> None:
            binary = self._ex_prefix + "/bin/excalibur"
            # Model identity: Harbor's --model flag lands in
            # self.model_name as provider/model; credentials and wiring
            # arrive via the scoped exec env (--ae EXCALIBUR_*) plus the
            # Harbor connection flags (--base-url, --effort). Nothing is
            # written to files or logs (Harbor redacts sensitive env).
            raw_model = self.model_name or ""
            provider, _, bare_model = raw_model.partition("/")
            if not bare_model:
                bare_model = raw_model
            extra = {}
            try:
                extra = dict(self._extra_env or {})
            except Exception:
                extra = {}
            if not provider:
                provider = extra.get("EXCALIBUR_PROVIDER") or os.environ.get("EXCALIBUR_PROVIDER", "openai")
            if not bare_model:
                bare_model = extra.get("EXCALIBUR_MODEL") or os.environ.get("EXCALIBUR_MODEL", "")
            model = bare_model
            base_url = None
            try:
                base_url = self._base_url
            except Exception:
                base_url = None
            base_url = base_url or extra.get("EXCALIBUR_BASE_URL") or os.environ.get("EXCALIBUR_BASE_URL", "")
            effort = ""
            try:
                effort = (self._model_connection_kwargs or {}).get("reasoning_effort") or ""
            except Exception:
                effort = ""
            effort = effort or extra.get("EXCALIBUR_REASONING_EFFORT") or os.environ.get("EXCALIBUR_REASONING_EFFORT", "")
            # Timeout is NOT communicated by Harbor 0.25.0 (the trial
            # kills the agent via asyncio.wait_for with no env/var
            # forwarding). Honor an explicit operator override when set
            # and apply the Section 23.2 90% closing budget.
            timeout_sec = None
            for key in ("HARBOR_AGENT_TIMEOUT_SEC", "EXCALIBUR_TIMEOUT_SEC"):
                raw = extra.get(key) or os.environ.get(key)
                if raw:
                    try:
                        timeout_sec = float(raw) * 0.9
                        break
                    except ValueError:
                        continue
            try:
                logs_posix = self.environment_logs_dir.as_posix()
            except Exception:
                logs_posix = "/logs/agent"
            # /logs/agent inside the container.
            cmd = f"{shlex.quote(binary)} exec --profile eval --json --logs-dir {shlex.quote(logs_posix)}"
            if timeout_sec:
                cmd += f" --timeout {int(timeout_sec)}"
            # Quote the instruction as a single argv element.
            cmd += f" {shlex.quote(instruction)}"
            run_env = {
                "EXCALIBUR_PROVIDER": provider,
                "EXCALIBUR_MODEL": model,
            }
            if base_url:
                run_env["EXCALIBUR_BASE_URL"] = base_url
            if effort:
                run_env["EXCALIBUR_REASONING_EFFORT"] = effort
            for k in ("EXCALIBUR_API_KEY", "EXCALIBUR_FALLBACK_API_KEY", "OPENAI_API_KEY"):
                v = extra.get(k) or os.environ.get(k)
                if v:
                    run_env[k] = v
            result = await self.exec_as_agent(environment, command=cmd, env=run_env)
            # A non-zero exec exit is the agent's honest final state
            # (failed/blocked/budget), not a harness error: the verifier
            # still decides the reward. Only raise when the harness
            # produced no records at all (missing metrics in container).
            if result.return_code != 0:
                try:
                    check = await self.exec_as_agent(
                        environment, command="test -f /logs/agent/excalibur/metrics.json && test -f /logs/agent/trajectory.json"
                    )
                    if check.return_code != 0:
                        raise RuntimeError(
                            f"excalibur exec failed (exit {result.return_code}) with no records: {(result.stderr or '')[-2000:]}"
                        )
                except RuntimeError:
                    raise
                except Exception:
                    pass
            # Fill AgentContext from the harness metrics inside the
            # container (host files sync only after the trial; see
            # populate_context_post_run for the host-side backfill).
            try:
                cat = await self.exec_as_agent(
                    environment, command="cat /logs/agent/excalibur/metrics.json"
                )
                snap = json.loads(cat.stdout or "{}")
                usage = snap.get("usage", {})
                context.n_input_tokens = int(usage.get("input", 0))
                context.n_cache_tokens = int(usage.get("cache_read", 0)) + int(usage.get("cache_write", 0))
                context.n_output_tokens = int(usage.get("output", 0)) + int(usage.get("reasoning", 0))
                context.cost_usd = float(snap.get("cost_usd", 0.0))
                # model_usage is backfilled by Harbor from the ATIF
                # trajectory; setting it manually as plain dicts trips
                # Pydantic serialization warnings, so it stays None here.
            except Exception:
                # Host-side populate_context_post_run backfills after
                # Harbor downloads the agent logs.
                pass

        def populate_context_post_run(self, context) -> None:
            # Host-side backfill after Harbor downloads the agent logs
            # (container files are invisible on the host during run()).
            # Also ensures the Harbor trajectory validates.
            try:
                ensure_harbor_trajectory(str(self.logs_dir))
            except Exception:
                pass
            try:
                if context.n_input_tokens is not None:
                    return
                filled = fill_agent_context(str(self.logs_dir))
                context.n_input_tokens = filled.n_input_tokens
                context.n_cache_tokens = filled.n_cache_tokens
                context.n_output_tokens = filled.n_output_tokens
                context.cost_usd = filled.cost_usd
            except Exception:
                pass

else:

    class ExcaliburAgent(BaseInstalledAgent):  # type: ignore
        """Fallback adapter for unit tests without Harbor."""

        def __init__(self, binary=None, expected_sha256=None):
            super().__init__()
            self.binary = binary
            self.expected_sha256 = expected_sha256
            self.installed = None

        def install(self, env=None):
            import subprocess  # noqa: F401

            prefix = (env or {}).get("EXCALIBUR_PREFIX", "/opt/excalibur")
            source = self.binary or (env or {}).get("EXCALIBUR_BINARY", "")
            if not source:
                raise ValueError("excalibur_agent: no binary source for install")
            expected = self.expected_sha256 or (env or {}).get("EXCALIBUR_SHA256", "")
            self.installed = install_binary(source, prefix, expected or None)
            return self.installed

        def configure(self, env=None, provider=None, model=None):
            return configure_env(
                provider or (env or {}).get("EXCALIBUR_PROVIDER", ""),
                model or (env or {}).get("EXCALIBUR_MODEL", ""),
                api_key=(env or {}).get("EXCALIBUR_API_KEY"),
            )

        def run(self, instruction, env=None):
            import subprocess

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
