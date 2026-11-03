"""RTDE session. Imported in an isolated, killable child process only."""
import json
import math
import os
import re
import time
from datetime import datetime, timezone
from pathlib import Path

import dashboard
from safety import Rejected, Safety, vector
from transforms import relative

MODES = {0: "DISCONNECTED", 1: "CONFIRM_SAFETY", 2: "BOOTING", 3: "POWER_OFF",
         4: "POWER_ON", 5: "IDLE", 6: "BACKDRIVE", 7: "RUNNING", 8: "UPDATING_FIRMWARE"}
SAFETY_MODES = {1: "NORMAL", 2: "REDUCED", 3: "PROTECTIVE_STOP", 4: "RECOVERY",
                5: "SAFEGUARD_STOP", 6: "SYSTEM_EMERGENCY_STOP", 7: "ROBOT_EMERGENCY_STOP",
                8: "VIOLATION", 9: "FAULT", 10: "VALIDATE_JOINT_ID", 11: "UNDEFINED_SAFETY_MODE"}


class Controller:
    def _remote_ok(self):
        """Real arms must be in Remote Control. Only a loopback simulator
        (URSim on this machine) may run in Local mode, and only on request."""
        if dashboard.command(self.host, "is in remote control").lower() == "true":
            return True
        return (os.environ.get("EXCALIBUR_ROBOT_SIMULATOR_LOCAL") == "1"
                and self.host in ("127.0.0.1", "localhost", "::1"))

    def __init__(self, config_dir, factory=None):
        self.directory = Path(config_dir)
        self.safety = Safety.load(self.directory / "robot.json")
        self.poses_path = self.directory / "robot-poses.json"
        self.poses = json.loads(self.poses_path.read_text()) if self.poses_path.exists() else {}
        for pose in self.poses.values():
            vector(pose, 6, "saved pose")
        self.factory = factory
        self.host = None
        self.control = self.receive = self.io = None
        self.freedrive = False
        self.fault = None
        self.progress = lambda state: None

    def connect(self, host):
        self.disconnect()
        self.host = host
        self.fault = None
        try:
            if self.factory:
                self.control, self.receive, self.io = self.factory(host)
            else:
                from rtde_control import RTDEControlInterface
                from rtde_receive import RTDEReceiveInterface
                from rtde_io import RTDEIOInterface
                # Receive/IO remain usable while powered off. Control uploads its
                # script lazily after Dashboard power-on and brake release.
                self.receive = RTDEReceiveInterface(host)
                self.io = RTDEIOInterface(host)
            result = self.state()
            result["polyscope_version"] = dashboard.command(host, "PolyscopeVersion")
            result["remote_control"] = dashboard.command(host, "is in remote control").lower() == "true"
            return result
        except Exception:
            self.disconnect()
            raise

    def disconnect(self):
        for interface in (self.control, self.receive, self.io):
            if interface:
                interface.disconnect()
        self.control = self.receive = self.io = None
        self.host = None
        self.freedrive = False
        return {"connected": False}

    def state(self):
        if self.fault:
            raise Rejected(self.fault + "; reconnect required")
        if not self.receive:
            return {"connected": False}
        if not self.receive.isConnected():
            raise Rejected("RTDE disconnected; reconnect required")
        inputs = [bool(self.receive.getActualDigitalInputBits() & (1 << i)) for i in range(18)]
        outputs = [bool(self.receive.getActualDigitalOutputBits() & (1 << i)) for i in range(18)]
        return {"connected": True, "host": self.host,
                "robot_mode": MODES.get(self.receive.getRobotMode(), "UNKNOWN"),
                "safety_mode": SAFETY_MODES.get(self.receive.getSafetyMode(), "UNKNOWN"),
                "program_running": bool(self.receive.getRobotStatus() & 2),
                "tcp_pose": list(self.receive.getActualTCPPose()), "joints": list(self.receive.getActualQ()),
                "digital_out": outputs, "digital_in": inputs,
                # The pendant speed slider. RTDE's trajectory speed scaling reads 0 whenever
                # no program runs, which looks like "cannot move" while the arm is idle.
                "speed_scaling": self.receive.getTargetSpeedFraction(),
                **self.gripper_state(inputs, outputs),
                "updated_at": datetime.now(timezone.utc).isoformat()}

    def gripper_state(self, inputs, outputs):
        """Sensor feedback when DI1/DI2 report it; otherwise the commanded output, labelled as such."""
        if inputs[1] != inputs[2]:
            return {"gripper": "closed" if inputs[1] else "open", "gripper_source": "sensor"}
        if inputs[1] and inputs[2]:
            return {"gripper": "unknown", "gripper_source": "sensor"}  # contradictory sensors
        commanded = outputs[self.safety.gripper_do] == bool(self.safety.close_value)
        return {"gripper": "closed" if commanded else "open", "gripper_source": "command"}

    def ready(self, body):
        self.safety.approved(body)
        state = self.state()
        if self.fault:
            raise Rejected(self.fault)
        if not state["connected"]:
            raise Rejected("robot disconnected")
        self.safety.modes(state)
        if not self._remote_ok():
            raise Rejected("Remote Control mode required")
        return state

    def get_control(self):
        if self.control is None:
            from rtde_control import RTDEControlInterface
            self.control = RTDEControlInterface(self.host)
        return self.control

    def move(self, body):
        state = self.ready(body)
        if self.freedrive:
            raise Rejected("exit freedrive before motion")
        kind = body.get("kind")
        if kind not in ("named", "pose", "relative", "joints"):
            raise Rejected("unknown move kind")
        speed, accel = self.safety.rates(body, kind == "joints")
        control = self.get_control()
        self.safety.pose(state["tcp_pose"])
        if kind == "joints":
            target = vector(body.get("joints"), 6, "joints")
            if any(abs(v) > 2 * math.pi for v in target):
                raise Rejected("joint target outside +/-2pi")
            # Reject unsafe joint paths before starting. Runtime monitoring is
            # also mandatory; this is not a collision planner.
            steps = max(1, math.ceil(max(abs(a-b) for a, b in zip(target, state["joints"])) / .025))
            for step in range(steps + 1):
                q = [a + (b-a)*step/steps for a, b in zip(state["joints"], target)]
                self.safety.pose(list(control.getForwardKinematics(q)))
            if not control.isJointsWithinSafetyLimits(target):
                raise Rejected("joint target violates controller safety limits")
            method = control.moveJ
        else:
            if kind == "named":
                if body.get("name") not in self.poses:
                    raise Rejected("unknown named pose")
                target = self.poses[body["name"]]
            elif kind == "relative":
                delta = self.safety.delta(body.get("delta"))
                frame = body.get("frame", "base")
                if frame not in ("base", "tool"):
                    raise Rejected("frame must be base or tool")
                target = relative(state["tcp_pose"], delta, frame)
            else:
                target = body.get("pose")
            target = self.safety.pose(target)
            if not control.isPoseWithinSafetyLimits(target):
                raise Rejected("pose violates controller safety limits")
            method = control.moveL
        # Recheck after potentially expensive IK/FK work.
        self.ready(body)
        if not method(target, speed, accel, True):
            raise Rejected("controller rejected motion")
        deadline = time.monotonic() + 22
        try:
            while True:
                state = self.state()
                self.safety.modes(state)
                self.safety.pose(state["tcp_pose"])
                self.progress(state)
                if not control.getAsyncOperationProgressEx().isAsyncOperationRunning():
                    break
                if time.monotonic() >= deadline:
                    raise TimeoutError("motion exceeded 30 second deadline")
                time.sleep(.02)
            actual = state["joints"] if kind == "joints" else state["tcp_pose"]
            if any(abs(a-b) > .01 for a, b in zip(actual, target)):
                raise Rejected("motion interrupted before target")
            return {"ok": True, "final_pose": state["tcp_pose"], "final_joints": state["joints"]}
        except Exception:
            dashboard.stop(self.host)
            raise

    def watch_freedrive(self):
        if not self.freedrive:
            return
        try:
            state = self.state()
            self.safety.modes(state)
            self.safety.pose(state["tcp_pose"])
            if math.hypot(*self.receive.getActualTCPSpeed()[:3]) > self.safety.speed:
                raise Rejected("freedrive linear speed exceeds cap")
            if max(abs(v) for v in self.receive.getActualQd()) > self.safety.joint_speed:
                raise Rejected("freedrive joint speed exceeds cap")
            self.progress(state)
        except Exception as exc:
            self.freedrive = False
            self.fault = str(exc)
            dashboard.stop(self.host)

    def call(self, path, body):
        if path == "/connect":
            return self.connect(body["host"])
        if path == "/disconnect":
            return self.disconnect()
        if path == "/state":
            return self.state()
        if path == "/move":
            return self.move(body)
        if path == "/poses" and body is None:
            return {"poses": self.poses.copy()}
        if path.startswith("/poses/") or path == "/poses":
            name = path[len("/poses/"):] if path.startswith("/poses/") else body.get("name", "")
            if not re.fullmatch(r"[A-Za-z0-9_-]{1,64}", name):
                raise Rejected("invalid pose name")
            updated = self.poses.copy()
            if path.startswith("/poses/"):
                if name not in updated:
                    raise Rejected("unknown named pose")
                del updated[name]
            else:
                state = self.state()
                if not state["connected"]:
                    raise Rejected("robot disconnected")
                updated[name] = self.safety.pose(state["tcp_pose"])
            self.directory.mkdir(parents=True, exist_ok=True)
            temporary = self.poses_path.with_suffix(".tmp")
            with temporary.open("w") as output:
                json.dump(updated, output, allow_nan=False)
                output.flush()
                os.fsync(output.fileno())
            temporary.replace(self.poses_path)
            self.poses = updated
            return {"poses": self.poses.copy()}
        if path == "/gripper":
            self.ready(body)
            action = body.get("action")
            if action not in ("open", "close"):
                raise Rejected("gripper action must be open or close")
            value = self.safety.close_value if action == "close" else not self.safety.close_value
            if not self.io.setStandardDigitalOut(self.safety.gripper_do, value):
                raise Rejected("controller rejected gripper output")
            state = self.state()
            if state.get("gripper_source") == "command":
                # Output bits lag the write by a cycle; without sensors the command is the truth.
                return {"ok": True, "gripper": "closed" if action == "close" else "open", "gripper_source": "command"}
            return {"ok": True, "gripper": state["gripper"], "gripper_source": "sensor"}
        if path == "/freedrive":
            if type(body.get("on")) is not bool:
                raise Rejected("freedrive on must be boolean")
            if body["on"]:
                state = self.ready(body)
                self.safety.pose(state["tcp_pose"])
                if not self.get_control().freedriveMode():
                    raise Rejected("controller rejected freedrive entry")
                self.freedrive = True
            elif self.control and not self.control.endFreedriveMode():
                raise Rejected("controller rejected freedrive exit")
            if not body["on"]:
                self.freedrive = False
            return {"ok": True}
        if path == "/power":
            actions = {"on": "power on", "off": "power off", "brake_release": "brake release"}
            action = body.get("action")
            if action not in actions:
                raise Rejected("unknown power action")
            if not self.host:
                raise Rejected("robot disconnected")
            if action != "off":
                self.safety.approved(body)
                if not self._remote_ok():
                    raise Rejected("Remote Control mode required")
            reply = dashboard.command(self.host, actions[action])
            if not any(word in reply.lower() for word in ("powering on", "powering off", "brake releasing")):
                raise Rejected("Dashboard rejected power command: " + reply)
            return {"ok": True, "robot_mode": self.state()["robot_mode"]}
        raise Rejected("unknown endpoint")
