"""Exercise the arm through the real JSON-lines runtime process."""
from __future__ import annotations

import base64
import json
from pathlib import Path
import queue
import subprocess
import sys
import threading
import time

import numpy as np

from robotlab.scenarios.arm import ArmScenario


def test_arm_real_sidecar_sort_snapshot_skill_and_pose(tmp_path):
    """Keep protocol, rendering, grasping and editable teleop working together."""
    runtime = Path(__file__).resolve().parents[1]
    with (tmp_path / "sidecar.stderr").open("w") as stderr:
        process = subprocess.Popen([sys.executable, "-m", "robotlab.server", "--data", str(tmp_path)],
                                   cwd=runtime, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                   stderr=stderr, text=True)
        replies = queue.Queue()

        def read():
            for line in process.stdout:
                replies.put(json.loads(line))

        reader = threading.Thread(target=read, daemon=True)
        reader.start()
        events = []
        identifier = 0

        def receive(predicate, timeout=8):
            deadline = time.monotonic() + timeout
            while time.monotonic() < deadline:
                reply = replies.get(timeout=max(.001, deadline - time.monotonic()))
                if predicate(reply):
                    return reply
                if "event" in reply:
                    events.append(reply)
            raise AssertionError("sidecar reply deadline exceeded")

        def call(method, params=None, timeout=8):
            nonlocal identifier
            identifier += 1
            process.stdin.write(json.dumps({"id": identifier, "method": method, "params": params or {}}) + "\n")
            process.stdin.flush()
            reply = receive(lambda value: value.get("id") == identifier, timeout)
            assert "error" not in reply, reply
            return reply["result"]

        try:
            receive(lambda value: value.get("event") == "ready")
            assert any(s["id"] == "arm" for s in call("scenarios.list")["scenarios"])
            opened = call("session.open", {"scenario": "arm", "params": {"task": "sort", "objects": 8}, "seed": 0})
            session = opened["session"]
            assert call("session.control", {"session": session, "mode": "manual", "inputs": {"gripper": 1}}) == {"mode": "manual"}
            image = call("session.snapshot", {"session": session, "camera": "top", "marks": True})
            (tmp_path / "arm-top.png").write_bytes(base64.b64decode(image.pop("image_png_b64")))
            assert len(image["marks"]) == 8
            assert [m["id"] for m in image["marks"]] == list(range(1, 9))
            assert all(m["bbox"][2] > m["bbox"][0] and m["bbox"][3] > m["bbox"][1] for m in image["marks"])
            result = call("session.skill", {"session": session, "skill": "pick_place", "args": {"object": image["marks"][0]["name"], "target": "A"}}, timeout=25)
            assert result["in_target"] is True and result["bin"] == "A"
            assert result["ok"] and result["frames"] == 700, result
            after = call("session.snapshot", {"session": session, "camera": "top", "marks": True})
            placed = next(m for m in after["marks"] if m["name"] == "part_1")
            assert abs(placed["world"][1] + .32) < .06, placed
            for _ in range(3):
                receive(lambda value: value.get("event") == "frame" and value["data"].get("mode") == "manual")
            settled = call("session.snapshot", {"session": session, "camera": "top"})
            assert abs(next(m for m in settled["marks"] if m["name"] == "part_1")["world"][1] + .32) < .06
            mapping = call("teleop.get_mapping", {"session": session})
            assert mapping == {"source": "default", "code": ArmScenario().pose_mapping_source()}
            landmarks = [[.5, .5, 0., 1.] for _ in range(33)]
            for idx, point in zip((12, 14, 16), ([.4, .3, 0, 1], [.6, .5, -.1, 1], [.65, .3, -.2, 1])):
                landmarks[idx] = point
            for i in range(6):
                assert call("session.control", {"session": session, "mode": "pose", "pose": {"landmarks": landmarks, "t": i * 40}}) == {"mode": "pose"}
                frame = receive(lambda value: value.get("event") == "frame" and value["data"].get("mode") == "pose")
                assert np.all(np.isfinite(frame["data"]["bodies"]))
            assert not [e for e in events if e.get("event") in ("teleop", "log")], events[-3:]
            call("session.close", {"session": session})
        finally:
            process.stdin.close()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
            reader.join(timeout=2)
            process.stdout.close()
