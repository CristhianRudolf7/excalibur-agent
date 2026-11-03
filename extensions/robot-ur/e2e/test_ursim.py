"""Motion tests are deliberately opt-in and never target a physical arm."""

import os
import subprocess
from urllib.error import URLError

import pytest

from check import bridge_check, direct_check, require_simulator
from contract_client import BridgeClient

pytestmark = pytest.mark.ursim


@pytest.fixture
def simulator():
    if os.environ.get("EXCALIBUR_URSIM_TESTS") != "1":
        pytest.skip("Set EXCALIBUR_URSIM_TESTS=1 to opt in to local simulator motion")
    try:
        result = subprocess.run(["docker", "inspect", "--format", "{{.State.Running}}",
                                 "excalibur-ursim"], capture_output=True, text=True, timeout=10)
    except FileNotFoundError:
        pytest.skip("Docker is not installed; URSim is not running")
    if result.returncode or result.stdout.strip() != "true":
        pytest.skip("URSim is not running; run ../ursim/ursim.sh up")
    # A running but incorrectly configured simulator is a failure, not a skip.
    require_simulator()


def test_direct_dashboard_rtde(simulator):
    direct_check()


def test_bridge_contract(simulator):
    port = int(os.environ.get("EXCALIBUR_ROBOT_BRIDGE_PORT", "7811"))
    url = f"http://127.0.0.1:{port}"
    try:
        BridgeClient(url).get("/health", timeout=2)
    except URLError as error:
        # TODO(RB1 merge): rerun with the bridge started; this is not bridge evidence.
        if isinstance(error.reason, ConnectionRefusedError):
            pytest.skip("RB1 Contract 1 bridge is not listening; direct test remains available")
        raise
    bridge_check(url)
