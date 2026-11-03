#!/usr/bin/env python3
"""Opt-in URSim checks; direct RTDE evidence is distinct from bridge evidence."""

import argparse
from concurrent.futures import ThreadPoolExecutor
import json
import os
import math
from pathlib import Path
import subprocess
import sys
import time

from contract_client import BridgeClient, BridgeError

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "ursim"))
from dashboard import Dashboard  # noqa: E402

HOME = [-math.pi / 2, -math.pi / 2, -math.pi / 2, -math.pi / 2, math.pi / 2, 0.0]
PORTS = (29999, 30001, 30002, 30003, 30004, 5900, 6080)


def require_simulator():
    """Refuse physical hosts and unrelated localhost services before any motion."""
    if not __debug__:
        raise RuntimeError("Run simulator checks without Python optimization; assertions are required")
    result = subprocess.run(["docker", "inspect", "excalibur-ursim"],
                            check=True, capture_output=True, text=True, timeout=10)
    container = json.loads(result.stdout)[0]
    config = container["Config"]
    assert container["State"]["Running"], "URSim is stopped"
    assert config["Image"].startswith("universalrobots/ursim_e-series:"), config["Image"]
    assert "ROBOT_MODEL=UR10" in config["Env"], "Expected the e-Series UR10e"
    assert config["Labels"].get("excalibur.robot.ursim") == "true"
    for port in PORTS:
        bindings = container["NetworkSettings"]["Ports"][f"{port}/tcp"]
        assert bindings == [{"HostIp": "127.0.0.1", "HostPort": str(port)}], bindings
    with Dashboard() as dashboard:
        version = dashboard.command("PolyscopeVersion")
        assert "5." in version, version
        remote = dashboard.command("is in remote control")
        # URSim accepts RTDE control in Local mode; the real arm needs Remote.
        allow_local = os.environ.get("EXCALIBUR_URSIM_ALLOW_LOCAL") == "1"
        assert allow_local or remote.lower() == "true", "Enable and select Remote Control in the URSim pendant"
    emit("simulator", image=config["Image"], image_id=container["Image"],
         version=version, remote_control=remote.lower() == "true")


def emit(step, **evidence):
    print(json.dumps({"step": step, **evidence}), flush=True)


def wait_for(read, predicate, timeout=10, description="state"):
    deadline = time.monotonic() + timeout
    value = None
    while time.monotonic() < deadline:
        value = read()
        if predicate(value):
            return value
        time.sleep(0.05)
    raise AssertionError(f"Timed out waiting for {description}: {value}")


def near(actual, expected, tolerance):
    assert len(actual) == len(expected), (actual, expected)
    assert all(abs(a - b) <= tolerance for a, b in zip(actual, expected)), (actual, expected)


def check_state(state):
    for field in ("connected", "host", "robot_mode", "safety_mode", "program_running",
                  "tcp_pose", "joints", "digital_out", "digital_in", "speed_scaling",
                  "gripper", "updated_at"):
        assert field in state, f"Contract 1 /state missing {field}: {state}"
    assert state["connected"] is True
    assert state["host"] == "127.0.0.1"
    for field in ("tcp_pose", "joints"):
        assert len(state[field]) == 6 and all(math.isfinite(v) for v in state[field])
    for field in ("digital_out", "digital_in"):
        assert len(state[field]) >= 3 and all(isinstance(v, bool) for v in state[field])
    assert state["gripper"] in ("open", "closed", "unknown")


def bridge_check(url):
    require_simulator()
    client = BridgeClient(url)
    health = client.get("/health", timeout=3)
    assert health["ok"] is True and health["version"]
    existing = client.get("/state")
    assert not existing.get("connected") or existing.get("host") == "127.0.0.1", \
        "Refusing to take over a bridge connected to another host"
    try:
        connected = client.post("/connect", {"host": "127.0.0.1"})
        assert connected["connected"] is True and connected["host"] == "127.0.0.1"
        state = client.get("/state")
        check_state(state)
        emit("bridge_connected", state=state)
        already_running = client.get("/state")["robot_mode"].upper() == "RUNNING"
        for action, mode in (() if already_running else (("on", ("IDLE", "RUNNING")), ("brake_release", ("RUNNING",)))):
            assert client.post("/power", {"action": action, "approved": True})["ok"] is True
            state = wait_for(lambda: client.get("/state"),
                             lambda s: s["robot_mode"].upper() in mode,
                             timeout=45, description=f"robot mode {mode}")
        state = client.get("/state")
        assert state["robot_mode"].upper() == "RUNNING", state
        assert state["safety_mode"].upper() == "NORMAL", state
        emit("bridge_running", state=state)

        result = client.post("/move", {"kind": "joints", "joints": HOME,
                                       "speed": 0.3, "accel": 0.5, "approved": True})
        assert result["ok"] is True
        near(result["final_joints"], HOME, 0.015)
        assert len(result["final_pose"]) == 6
        near(client.get("/state")["joints"], HOME, 0.015)
        emit("bridge_home", result=result)

        before = client.get("/state")["tcp_pose"]
        result = client.post("/move", {"kind": "relative", "delta": {
                                       "dx": 0.02, "dy": 0.0, "dz": 0.0,
                                       "drx": 0.0, "dry": 0.0, "drz": 0.0},
                                       "frame": "base", "speed": 0.05, "accel": 0.1,
                                       "approved": True})
        assert result["ok"] is True
        after = client.get("/state")["tcp_pose"]
        expected = before[:]
        expected[0] += 0.02
        near(after, expected, 0.002)
        emit("bridge_relative_2cm", delta=[a - b for a, b in zip(after, before)])

        for action, value in (("close", True), ("open", False)):
            result = client.post("/gripper", {"action": action, "approved": True})
            assert result["ok"] is True
            state = wait_for(lambda: client.get("/state"),
                             lambda s: s["digital_out"][1] is value)
            emit("bridge_gripper", action=action, digital_out=state["digital_out"])

        # Check rejection while RUNNING so a stopped program cannot produce a false pass.
        before = client.get("/state")["tcp_pose"]
        try:
            client.post("/move", {"kind": "pose", "pose": [100, 100, 100, *before[3:]],
                                   "speed": 0.05, "approved": True})
        except BridgeError as error:
            assert 400 <= error.status < 600
            assert error.body["ok"] is False and error.body["error"]
            reason = error.body["error"].lower()
            assert any(word in reason for word in ("workspace", "box", "outside", "bounds")), reason
            emit("bridge_workspace_rejected", status=error.status, error=error.body)
        else:
            raise AssertionError("Out-of-box target was accepted")
        near(client.get("/state")["tcp_pose"], before, 0.002)

        with ThreadPoolExecutor(max_workers=1) as executor:
            motion = executor.submit(client.post, "/move", {
                "kind": "relative", "delta": {"dx": 0.10, "dy": 0.0, "dz": 0.0,
                                                "drx": 0.0, "dry": 0.0, "drz": 0.0}, "frame": "base",
                "speed": 0.02, "accel": 0.1, "approved": True})
            try:
                wait_for(lambda: client.get("/state")["tcp_pose"],
                         lambda p: abs(p[0] - before[0]) > 0.003,
                         timeout=5, description="motion started before STOP")
                assert not motion.done(), "Move finished before STOP"
                start = time.monotonic()
                assert client.post("/stop", timeout=3)["ok"] is True
                assert time.monotonic() - start < 3, "STOP blocked behind /move"
                try:
                    completed = motion.result(timeout=5)
                    assert completed["ok"] is True
                    assert abs(completed["final_pose"][0] - before[0]) < 0.08, completed
                except BridgeError as error:
                    assert error.body["ok"] is False and error.body["error"]
            finally:
                assert client.post("/stop", timeout=3)["ok"] is True
        stopped = client.get("/state")["tcp_pose"]
        assert abs(stopped[0] - before[0]) < 0.08, (before, stopped)
        time.sleep(0.4)
        near(client.get("/state")["tcp_pose"], stopped, 0.001)
        emit("bridge_stop_during_motion", before=before, stopped=stopped)
    finally:
        # STOP never carries approval, including failure cleanup.
        try:
            assert client.post("/stop", timeout=3)["ok"] is True
        finally:
            disconnected = client.post("/disconnect")
            assert disconnected["connected"] is False


def direct_check():
    require_simulator()
    import rtde_control
    import rtde_io
    import rtde_receive

    with Dashboard() as dashboard:
        mode = dashboard.command("robotmode").upper()
        if mode not in ("ROBOTMODE: IDLE", "ROBOTMODE: RUNNING"):
            emit("dashboard_power_on", reply=dashboard.command("power on"))
            dashboard.wait_mode("IDLE")
        if mode != "ROBOTMODE: RUNNING":
            emit("dashboard_brake_release", reply=dashboard.command("brake release"))
            dashboard.wait_mode("RUNNING")
        emit("dashboard_running", mode=dashboard.command("robotmode"),
             safety=dashboard.command("safetystatus"),
             remote=dashboard.command("is in remote control"))

    receive = control = io = None
    original_do = None
    try:
        # PolyScope 5.25.0 has a documented simulator RTDE issue below 500 Hz.
        receive = rtde_receive.RTDEReceiveInterface("127.0.0.1", 500.0)
        assert receive.getSafetyMode() == 1, "Expected NORMAL safety"
        assert receive.getRobotMode() == 7, "Expected RUNNING robot mode"
        emit("rtde_state", tcp_pose=receive.getActualTCPPose(), joints=receive.getActualQ(),
             digital_out=receive.getActualDigitalOutputBits())
        control = rtde_control.RTDEControlInterface("127.0.0.1", 500.0)
        io = rtde_io.RTDEIOInterface("127.0.0.1")
        original_do = receive.getDigitalOutState(1)
        assert control.moveJ(HOME, 0.3, 0.5)
        wait_for(receive.getActualQ, lambda q: max(abs(a - b) for a, b in zip(q, HOME)) < 0.015)
        emit("rtde_home", joints=receive.getActualQ(), tcp_pose=receive.getActualTCPPose())
        before = receive.getActualTCPPose()
        target = before[:]
        target[0] += 0.02
        assert control.moveL(target, 0.05, 0.1)
        after = wait_for(receive.getActualTCPPose, lambda p: abs(p[0] - target[0]) < 0.002)
        near(after, target, 0.002)
        emit("rtde_relative_2cm", delta=[a - b for a, b in zip(after, before)])
        for value in (True, False):
            assert io.setStandardDigitalOut(1, value)
            wait_for(lambda: receive.getDigitalOutState(1), lambda state: state == value)
            emit("rtde_gripper_do1", value=value, digital_out=receive.getActualDigitalOutputBits())
        before = receive.getActualTCPPose()
        target = before[:]
        target[0] += 0.10
        assert control.moveL(target, 0.02, 0.1, True)
        wait_for(receive.getActualTCPSpeed, lambda speed: abs(speed[0]) > 0.003,
                 timeout=5, description="asynchronous move started")
        control.stopL(0.5)
        wait_for(receive.getActualTCPSpeed, lambda speed: max(abs(v) for v in speed) < 0.001)
        with Dashboard() as dashboard:
            reply = dashboard.command("stop")
            assert "stopped" in reply.lower(), reply
        stopped = receive.getActualTCPPose()
        assert abs(stopped[0] - before[0]) < 0.08, (before, stopped)
        time.sleep(0.4)
        near(receive.getActualTCPPose(), stopped, 0.001)
        emit("rtde_stop_during_motion", before=before, stopped=stopped, dashboard=reply)
        emit("not_tested", checks=["bridge HTTP contract", "bridge workspace rejection",
                                   "bridge approval and speed enforcement", "physical gripper sensors"])
    finally:
        try:
            if io is not None and original_do is not None:
                io.setStandardDigitalOut(1, original_do)
            if control is not None:
                control.stopJ(0.5)
                control.stopScript()
        finally:
            try:
                with Dashboard() as dashboard:
                    dashboard.command("stop")
            finally:
                for interface in (io, control, receive):
                    if interface is not None:
                        interface.disconnect()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("direct", "bridge"))
    parser.add_argument("--bridge-url", default="http://127.0.0.1:7811")
    args = parser.parse_args()
    if args.mode == "direct":
        direct_check()
    else:
        bridge_check(args.bridge_url)


if __name__ == "__main__":
    main()
