"""Independent, deadline-bounded sockets keep STOP available during RTDE calls."""
import os
import socket
import time


def remaining(deadline):
    budget = deadline - time.monotonic()
    if budget <= 0:
        raise TimeoutError("Dashboard socket deadline exceeded")
    return budget


def line(sock, deadline):
    data = bytearray()
    while b"\n" not in data:
        sock.settimeout(remaining(deadline))
        chunk = sock.recv(4096 - len(data))
        if not chunk:
            raise OSError("Dashboard disconnected")
        data.extend(chunk)
        if len(data) >= 4096 and b"\n" not in data:
            raise OSError("Dashboard reply exceeds 4096 bytes")
    return bytes(data).split(b"\n", 1)[0].decode("utf-8", "replace").strip()


# Remote links can add ~0.5 s per round trip; STOP keeps a shorter budget.
COMMAND_SECONDS = float(os.environ.get("EXCALIBUR_ROBOT_DASHBOARD_TIMEOUT", "3"))
STOP_SECONDS = 2.0


def command(host, text):
    deadline = time.monotonic() + COMMAND_SECONDS
    with socket.create_connection((host, 29999), timeout=remaining(deadline)) as sock:
        line(sock, deadline)
        sock.settimeout(remaining(deadline))
        sock.sendall((text + "\n").encode("ascii"))
        return line(sock, deadline)


def stop(host):
    errors = []
    try:
        deadline = time.monotonic() + STOP_SECONDS
        with socket.create_connection((host, 30002), timeout=remaining(deadline)) as sock:
            sock.settimeout(remaining(deadline))
            sock.sendall(b"def excalibur_stop():\n  stopl(0.5)\n  stopj(0.6)\nend\n")
    except OSError as exc:
        errors.append(str(exc))
    try:
        reply = command(host, "stop")
        if "stopped" not in reply.lower():
            errors.append(reply)
    except OSError as exc:
        errors.append(str(exc))
    if errors:
        raise OSError("STOP: " + "; ".join(errors))
