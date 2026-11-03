"""Loopback HTTP boundary with a killable RTDE worker and independent STOP."""
import ipaddress
import json
import multiprocessing
import os
import signal
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import dashboard
from safety import Rejected

VERSION = "0.1.0"


def worker(pipe, directory):
    from controller import Controller
    try:
        try:
            controller = Controller(directory)
        except Exception as exc:
            # Keep the initialization cause available through HTTP; a native
            # child crash must not replace a malformed-config error with EOF.
            error = {"ok": False, "error": str(exc)}
            while True:
                pipe.recv()
                pipe.send((400, error))
        last_progress = 0
        def progress(state):
            nonlocal last_progress
            if time.monotonic() - last_progress >= .5:
                pipe.send(("state", state))
                last_progress = time.monotonic()
        controller.progress = progress
        while True:
            if not pipe.poll(.02):
                controller.watch_freedrive()
                continue
            path, body = pipe.recv()
            try:
                result = controller.call(path, body)
                pipe.send((200, result))
            except (Rejected, KeyError, TypeError, ValueError) as exc:
                pipe.send((400, {"ok": False, "error": str(exc)}))
            except Exception as exc:
                pipe.send((502, {"ok": False, "error": str(exc)}))
    except (EOFError, BrokenPipeError):
        pass
    finally:
        pipe.close()


class Bridge:
    def __init__(self, directory=None, timeout=24, process_target=worker):
        self.directory = str(directory or Path.home() / ".config/excalibur")
        self.timeout = timeout
        self.target = process_target
        self.lock = threading.Lock()
        self.guard = threading.Lock()
        self.process = self.pipe = None
        self.host = None
        self.cached = {"connected": False}

    def terminate(self):
        with self.guard:
            process, pipe = self.process, self.pipe
            self.process = self.pipe = None
            self.cached = {"connected": False}
        if process:
            process.terminate()
            process.join(timeout=1)
            if process.is_alive():
                process.kill()
                process.join(timeout=1)
        if pipe:
            pipe.close()

    def stop(self):
        # Terminate before STOP so no queued native call can issue a late move.
        self.terminate()
        if self.host:
            dashboard.stop(self.host)
        return {"ok": True}

    def call(self, method, path, body):
        if method == "GET" and path == "/health":
            return 200, {"ok": True, "version": VERSION}
        if method == "POST" and path == "/stop":
            return 200, self.stop()
        allowed = {("GET", "/state"), ("GET", "/poses"), ("POST", "/connect"),
                   ("POST", "/disconnect"), ("POST", "/move"), ("POST", "/gripper"),
                   ("POST", "/power"), ("POST", "/poses"), ("POST", "/freedrive")}
        if (method, path) not in allowed and not (method == "DELETE" and path.startswith("/poses/")):
            return 404, {"ok": False, "error": "unknown endpoint"}
        if path == "/connect":
            host = body.get("host")
            try:
                ipaddress.ip_address(host)
            except (ValueError, TypeError):
                raise Rejected("host must be a robot IP address")
        if not self.lock.acquire(blocking=False):
            if method == "GET" and path == "/state":
                with self.guard:
                    return 200, self.cached.copy()
            return 409, {"ok": False, "error": "robot operation in progress; STOP remains available"}
        try:
            if path in ("/connect", "/disconnect"):
                if self.host:
                    try:
                        self.stop()
                    finally:
                        self.host = None
                else:
                    self.terminate()
                self.host = body["host"] if path == "/connect" else None
                if path == "/disconnect":
                    return 200, {"connected": False}
            fresh = False
            with self.guard:
                if self.process is None:
                    fresh = True
                    context = multiprocessing.get_context("spawn")
                    pipe, child = context.Pipe()
                    process = context.Process(target=self.target, args=(child, self.directory), daemon=True)
                    process.start()
                    child.close()
                    self.process, self.pipe = process, pipe
                pipe = self.pipe
            if fresh and self.host and path != "/connect":
                # After STOP (or a worker restart) reconnect to the same arm so
                # state stays readable; STOP itself never waits for this.
                try:
                    pipe.send(("/connect", {"host": self.host}))
                    deadline = time.monotonic() + self.timeout
                    while True:
                        if not pipe.poll(max(0, deadline - time.monotonic())):
                            try:
                                self.stop()
                            except OSError as exc:
                                return 504, {"ok": False, "error": "robot operation timed out; " + str(exc)}
                            return 504, {"ok": False, "error": "robot operation timed out; reconnect required"}
                        status, result = pipe.recv()
                        if status != "state":
                            break
                    if status != 200:
                        self.terminate()
                        return status, result
                except (OSError, EOFError, ValueError):
                    try:
                        self.stop()
                    except OSError as exc:
                        return 502, {"ok": False, "error": "RTDE worker disconnected; " + str(exc)}
                    return 502, {"ok": False, "error": "RTDE worker disconnected; reconnect required"}
            try:
                pipe.send((path, None if method == "GET" else body))
                deadline = time.monotonic() + self.timeout
                while True:
                    if not pipe.poll(max(0, deadline - time.monotonic())):
                        try:
                            self.stop()
                        except OSError as exc:
                            return 504, {"ok": False, "error": "robot operation timed out; " + str(exc)}
                        return 504, {"ok": False, "error": "robot operation timed out; reconnect required"}
                    status, result = pipe.recv()
                    if status != "state":
                        break
                    with self.guard:
                        if pipe is self.pipe:
                            self.cached = result

            except (OSError, EOFError, ValueError):
                try:
                    self.stop()
                except OSError as exc:
                    return 502, {"ok": False, "error": "RTDE worker disconnected; " + str(exc)}
                return 502, {"ok": False, "error": "RTDE worker disconnected; reconnect required"}
            if status == 200 and path in ("/connect", "/state"):
                with self.guard:
                    if pipe is self.pipe:
                        self.cached = result.copy()
            return status, result
        finally:
            self.lock.release()


class Server(ThreadingHTTPServer):
    daemon_threads = True
    allow_reuse_address = True

    def __init__(self, address, bridge):
        self.bridge = bridge
        super().__init__(address, Handler)


class Handler(BaseHTTPRequestHandler):
    def setup(self):
        super().setup()
        self.connection.settimeout(5)

    def dispatch(self):
        try:
            size = int(self.headers.get("Content-Length", "0"))
            if size < 0 or size > 16384:
                raise Rejected("body exceeds 16 KiB")
            body = json.loads(self.rfile.read(size)) if size else {}
            if not isinstance(body, dict):
                raise Rejected("body must be an object")
            status, result = self.server.bridge.call(self.command, self.path, body)
        except (Rejected, ValueError, TypeError) as exc:
            status, result = 400, {"ok": False, "error": str(exc)}
        except Exception as exc:
            status, result = 502, {"ok": False, "error": str(exc)}
        data = json.dumps(result, allow_nan=False).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    do_GET = do_POST = do_DELETE = dispatch

    def log_message(self, *args):
        pass


def main():
    # Remote links (VPN relays) need ~10 s just to start the control script.
    bridge = Bridge(timeout=float(os.environ.get("EXCALIBUR_ROBOT_BRIDGE_TIMEOUT", "60")))
    server = Server(("127.0.0.1", int(os.environ.get("EXCALIBUR_ROBOT_BRIDGE_PORT", "7811"))), bridge)
    def shutdown(signum, frame):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, shutdown)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        try:
            bridge.stop()
        finally:
            server.server_close()


if __name__ == "__main__":
    main()
