"""No hardware, paid APIs, Docker or ur_rtde installation needed."""
import json
import math
import tempfile
import threading
import time
import unittest
import urllib.error
import urllib.request
from pathlib import Path
from unittest.mock import patch

import dashboard
from controller import Controller
from safety import Rejected, Safety
from server import Bridge, Server
from transforms import relative


class Fake:
    def __init__(self):
        self.pose = [.2, .2, .4, 0., 0., 0.]
        self.joints = [0.] * 6
        self.safety_mode = 1
        self.robot_mode = 7
        self.connected = True
        self.calls = []
        self.inputs = 2
        self.outputs = 0
        self.progress = -1
        self.fk_pose = self.pose.copy()
        self.tcp_speed = [0.] * 6
        self.qd = [0.] * 6
        self.accept = True

    def disconnect(self): self.connected = False
    def isConnected(self): return self.connected
    def getRobotMode(self): return self.robot_mode
    def getSafetyMode(self): return self.safety_mode
    def getRobotStatus(self): return 3
    def getActualTCPPose(self): return self.pose
    def getActualQ(self): return self.joints
    def getActualDigitalInputBits(self): return self.inputs
    def getActualDigitalOutputBits(self): return self.outputs
    def getSpeedScaling(self): return .5
    def getForwardKinematics(self, q): return self.fk_pose
    def isJointsWithinSafetyLimits(self, q): return self.accept
    def isPoseWithinSafetyLimits(self, pose): return self.accept
    def getAsyncOperationProgressEx(self): return self
    def isAsyncOperationRunning(self): return self.progress >= 0
    def getActualTCPSpeed(self): return self.tcp_speed
    def getActualQd(self): return self.qd
    def freedriveMode(self): self.calls.append("freedrive"); return self.accept
    def endFreedriveMode(self): self.calls.append("end_freedrive"); return self.accept

    def moveL(self, pose, speed, accel, asynchronous):
        self.calls.append(("moveL", pose, speed, accel, asynchronous))
        self.pose = pose
        return self.accept

    def moveJ(self, joints, speed, accel, asynchronous):
        self.calls.append(("moveJ", joints, speed, accel, asynchronous))
        self.joints = joints
        return self.accept

    def setStandardDigitalOut(self, index, value):
        self.calls.append(("gripper", index, value))
        self.outputs = int(value) << index
        return self.accept


def idle_worker(pipe, directory):
    while True:
        path, body = pipe.recv()
        if path == "/move":
            time.sleep(5)
        else:
            pipe.send((200, {"connected": path != "/disconnect", "host": "127.0.0.1"}))


class SafetyTest(unittest.TestCase):
    def test_defaults_and_all_hard_caps(self):
        safety = Safety()
        self.assertEqual((safety.speed, safety.joint_speed, safety.step_m, safety.step_rad), (.25, .6, .15, .5))
        for key, ceiling in (("speed_max", .5), ("joint_speed_max", .6), ("accel_max", .5), ("max_step_m", .15), ("max_step_rad", .5)):
            for value in (0, -1, ceiling + .01, math.nan, math.inf, True, "0.1"):
                with self.subTest(key=key, value=value), self.assertRaises(Rejected):
                    Safety({key: value})
            self.assertGreater(getattr(Safety({key: .01}), {"speed_max": "speed", "joint_speed_max": "joint_speed", "accel_max": "accel", "max_step_m": "step_m", "max_step_rad": "step_rad"}[key]), 0)

    def test_box_and_invalid_vectors(self):
        safety = Safety()
        for axis in range(3):
            for value in (-2, 2):
                pose = [0, 0, .5, 0, 0, 0]
                pose[axis] = value
                with self.assertRaises(Rejected): safety.pose(pose)
        for pose in (None, [], [0]*5, [0, 0, .3, 0, 0, math.nan], [0, 0, .3, 0, 0, True]):
            with self.assertRaises(Rejected): safety.pose(pose)
        with self.assertRaises(Rejected): Safety({"workspace": {"min": [0]*3, "max": [0]*3}})
        safety.pose([-.6, .6, .1, 0, 0, 0])

    def test_relative_norm_and_rotation(self):
        safety = Safety()
        for delta in ({"dx": .151}, {"dx": .11, "dy": .11}, {"drz": .501}, {"drx": .4, "dry": .4}, {"x": 1}):
            with self.assertRaises(Rejected): safety.delta(delta)
        safety.delta({"dx": .15, "drz": .5})
        pose = [.2, .2, .4, 0, 0, math.pi/2]
        target = relative(pose, [.1, 0, 0, 0, 0, 0], "tool")
        self.assertAlmostEqual(target[0], .2)
        self.assertAlmostEqual(target[1], .3)
        self.assertAlmostEqual(target[5], math.pi/2)

    def test_modes_rates_approval_config(self):
        safety = Safety()
        for mode in ("REDUCED", "PROTECTIVE_STOP", "UNKNOWN"):
            with self.assertRaises(Rejected): safety.modes({"safety_mode": mode, "robot_mode": "RUNNING"})
        with self.assertRaises(Rejected): safety.modes({"safety_mode": "NORMAL", "robot_mode": "IDLE"})
        for body in ({}, {"approved": 1}, {"approved": "true"}, {"approved": False}):
            with self.assertRaises(Rejected): safety.approved(body)
        Safety({"require_approval": False}).approved({})
        for body in ({"speed": .26}, {"accel": .51}, {"speed": -1}, {"speed": math.inf}):
            with self.assertRaises(Rejected): safety.rates(body)
        with self.assertRaises(Rejected): safety.rates({"speed": .61}, joint=True)
        for config in ({"gripper_do": 8}, {"gripper_do": True}, {"gripper_close_value": 1}, {"require_approval": "false"}):
            with self.assertRaises(Rejected): Safety(config)


class ControllerTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.fake = Fake()
        self.controller = Controller(self.temp.name, lambda host: (self.fake, self.fake, self.fake))
        self.dashboard = patch("controller.dashboard.command", return_value="true")
        self.dashboard.start()
        self.addCleanup(self.dashboard.stop)
        self.stop = patch("controller.dashboard.stop")
        self.stopped = self.stop.start()
        self.addCleanup(self.stop.stop)
        self.controller.connect("127.0.0.1")

    def test_every_motion_kind_and_rates(self):
        c = self.controller
        c.call("/poses", {"name": "home"})
        for body in ({"kind": "named", "name": "home"}, {"kind": "pose", "pose": [.3, .2, .4, 0, 0, 0]}, {"kind": "relative", "delta": {"dx": .01}, "frame": "tool"}, {"kind": "joints", "joints": [.1]*6}):
            self.assertTrue(c.move(dict(body, approved=True))["ok"])
        self.assertTrue(all(call[-1] is True for call in self.fake.calls))
        self.assertEqual(self.fake.calls[-1][2], .6)
        self.assertEqual(self.fake.calls[0][2], .25)
        for body in ({"kind": "no"}, {"kind": "named", "name": "missing"}, {"kind": "relative", "delta": {"dx": .01}, "frame": "world"}, {"kind": "joints", "joints": [7.]*6}):
            with self.assertRaises(Rejected): c.move(dict(body, approved=True))

    def test_denials_have_no_effect(self):
        for body in ({"kind": "pose", "pose": [2]*6, "approved": True}, {"kind": "relative", "delta": {"dx": .2}, "approved": True}, {"kind": "joints", "joints": [0]*6}, {"kind": "pose", "pose": self.fake.pose, "speed": .3, "approved": True}):
            with self.assertRaises(Rejected): self.controller.move(body)
        self.fake.fk_pose = [2]*6
        with self.assertRaises(Rejected): self.controller.move({"kind": "joints", "joints": [0]*6, "approved": True})
        self.fake.safety_mode = 3
        with self.assertRaisesRegex(Rejected, "PROTECTIVE_STOP"):
            self.controller.move({"kind": "named", "name": "home", "approved": True})
        self.assertEqual(self.fake.calls, [])

    def test_gripper_sensor_polarity_and_approval(self):
        with self.assertRaises(Rejected): self.controller.call("/gripper", {"action": "close"})
        result = self.controller.call("/gripper", {"action": "close", "approved": True})
        self.assertEqual(result["gripper"], "closed")
        self.assertEqual(self.fake.calls[-1], ("gripper", 1, True))
        self.controller.safety.close_value = False
        self.fake.inputs = 4
        result = self.controller.call("/gripper", {"action": "open", "approved": True})
        self.assertEqual(result["gripper"], "open")
        self.assertEqual(self.fake.calls[-1], ("gripper", 1, True))
        self.fake.inputs = 6
        self.assertEqual(self.controller.state()["gripper"], "unknown")

    def test_poses_durable_delete_and_bad_names(self):
        self.controller.call("/poses", {"name": "home"})
        reloaded = Controller(self.temp.name)
        self.assertIn("home", reloaded.call("/poses", None)["poses"])
        self.assertEqual(reloaded.call("/poses/home", {})["poses"], {})
        for name in ("../x", "", "x"*65):
            with self.assertRaises(Rejected): self.controller.call("/poses", {"name": name})
        with self.assertRaises(Rejected): self.controller.call("/poses/missing", {})

    def test_remote_disconnect_reconnect_and_power(self):
        with patch("controller.dashboard.command", return_value="false"):
            with self.assertRaisesRegex(Rejected, "Remote Control"):
                self.controller.move({"kind": "relative", "delta": {"dx": .01}, "approved": True})
        self.controller.disconnect()
        self.assertFalse(self.controller.state()["connected"])
        self.fake = Fake()
        self.controller.connect("127.0.0.1")
        for action, reply in (("on", "Powering on"), ("off", "Powering off"), ("brake_release", "Brake releasing")):
            with patch("controller.dashboard.command", side_effect=lambda host, cmd: "true" if cmd == "is in remote control" else reply):
                self.assertTrue(self.controller.call("/power", {"action": action, "approved": True})["ok"])
        self.fake.connected = False
        with self.assertRaisesRegex(Rejected, "disconnected"): self.controller.state()

    def test_protective_stop_and_timeout_during_move(self):
        self.fake.progress = 1
        with patch("controller.time.sleep", side_effect=lambda _: setattr(self.fake, "safety_mode", 3)):
            with self.assertRaisesRegex(Rejected, "PROTECTIVE_STOP"):
                self.controller.move({"kind": "relative", "delta": {"dx": .01}, "approved": True})
        self.stopped.assert_called_once()
        self.fake.safety_mode = 1
        with patch("controller.time.monotonic", side_effect=[0, 30]):
            with self.assertRaises(TimeoutError):
                self.controller.move({"kind": "relative", "delta": {"dx": .01}, "approved": True})

    def test_freedrive_approval_watchdog_and_exit(self):
        with self.assertRaises(Rejected): self.controller.call("/freedrive", {"on": True})
        self.controller.call("/freedrive", {"on": True, "approved": True})
        self.assertTrue(self.controller.freedrive)
        self.controller.watch_freedrive()
        self.fake.tcp_speed[0] = .3
        self.controller.watch_freedrive()
        self.assertFalse(self.controller.freedrive)
        self.assertIn("speed", self.controller.fault)
        self.stopped.assert_called_once()
        self.assertTrue(self.controller.call("/freedrive", {"on": False})["ok"])


class ServerTest(unittest.TestCase):
    def test_invalid_config_error_is_preserved(self):
        with tempfile.TemporaryDirectory() as directory:
            (Path(directory) / "robot.json").write_text('{"speed_max": 100}')
            bridge = Bridge(directory=directory)
            self.addCleanup(bridge.terminate)
            status, result = bridge.call("GET", "/state", {})
            self.assertEqual(status, 400)
            self.assertIn("speed_max", result["error"])

    def test_timeout_kills_worker_and_stop_bypasses_busy_lock(self):
        bridge = Bridge(timeout=.1, process_target=idle_worker)
        self.addCleanup(bridge.terminate)
        with patch("server.dashboard.stop") as stop:
            bridge.host = "127.0.0.1"
            start = time.monotonic()
            status, result = bridge.call("POST", "/move", {"approved": True})
            self.assertEqual(status, 504)
            self.assertLess(time.monotonic()-start, 3)
            self.assertIsNone(bridge.process)
            stop.assert_called_once()
            bridge.lock.acquire()
            try:
                self.assertTrue(bridge.call("POST", "/stop", {})[1]["ok"])
                self.assertEqual(bridge.call("POST", "/gripper", {})[0], 409)
                self.assertFalse(bridge.call("GET", "/state", {})[1]["connected"])
            finally:
                bridge.lock.release()

    def test_http_health_validation_and_disconnect(self):
        bridge = Bridge(process_target=idle_worker)
        self.addCleanup(bridge.terminate)
        server = Server(("127.0.0.1", 0), bridge)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        root = "http://127.0.0.1:" + str(server.server_port)
        with urllib.request.urlopen(root+"/health") as response:
            self.assertTrue(json.load(response)["ok"])
        for path, body in (("/connect", {"host": "not-an-ip"}), ("/move", [])):
            req = urllib.request.Request(root+path, data=json.dumps(body).encode(), method="POST")
            with self.assertRaises(urllib.error.HTTPError) as caught: urllib.request.urlopen(req)
            self.assertEqual(caught.exception.code, 400)
            self.assertFalse(json.load(caught.exception)["ok"])
        self.assertEqual(bridge.call("POST", "/disconnect", {})[1], {"connected": False})


class RTDEBindingsTest(unittest.TestCase):
    def test_pinned_native_binding_surface_without_connecting(self):
        try:
            import rtde_control
            import rtde_receive
            import rtde_io
        except ImportError:
            self.skipTest("ur_rtde is optional for ordinary fake tests")
        for cls, methods in (
            (rtde_receive.RTDEReceiveInterface, ("getRobotStatus", "getRobotMode", "getSafetyMode", "getActualTCPPose", "getActualQ", "getActualDigitalInputBits", "getActualDigitalOutputBits", "getActualTCPSpeed", "getActualQd", "getSpeedScaling", "isConnected", "disconnect")),
            (rtde_control.RTDEControlInterface, ("moveL", "moveJ", "getAsyncOperationProgressEx", "getForwardKinematics", "isJointsWithinSafetyLimits", "isPoseWithinSafetyLimits", "freedriveMode", "endFreedriveMode", "disconnect")),
            (rtde_io.RTDEIOInterface, ("setStandardDigitalOut", "disconnect")),
        ):
            for method in methods:
                self.assertTrue(hasattr(cls, method), f"{cls.__name__}.{method}")
        self.assertTrue(hasattr(rtde_control.AsyncOperationStatus, "isAsyncOperationRunning"))


class DashboardTest(unittest.TestCase):
    def test_stop_sends_both_script_stops_and_dashboard(self):
        class Socket:
            def __init__(self, chunks=()):
                self.chunks = iter(chunks)
                self.sent = b""
            def __enter__(self): return self
            def __exit__(self, *args): pass
            def settimeout(self, value): self.timeout = value
            def recv(self, size): return next(self.chunks)
            def sendall(self, data): self.sent += data
        script = Socket()
        board = Socket([b"Connected\n", b"Stopped\n"])
        with patch("dashboard.socket.create_connection", side_effect=[script, board]) as connect:
            dashboard.stop("127.0.0.1")
        self.assertIn(b"stopl", script.sent)
        self.assertIn(b"stopj", script.sent)
        self.assertEqual(board.sent, b"stop\n")
        self.assertEqual([call.args[0][1] for call in connect.call_args_list], [30002, 29999])
        with patch("dashboard.socket.create_connection", side_effect=OSError("offline")) as connect:
            with self.assertRaisesRegex(OSError, "STOP: offline; offline"):
                dashboard.stop("127.0.0.1")
            self.assertEqual(connect.call_count, 2)

    def test_slow_replies_cannot_extend_overall_deadline(self):
        with patch("dashboard.time.monotonic", return_value=2):
            with self.assertRaises(TimeoutError): dashboard.remaining(1)


if __name__ == "__main__":
    unittest.main()
